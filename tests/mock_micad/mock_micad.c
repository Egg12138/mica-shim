#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <sys/socket.h>
#include <sys/un.h>
#include <errno.h>
#include <signal.h>
#include <stdbool.h>
#include <sys/stat.h>
#include <stdint.h>
#include <fcntl.h>
#include <sys/types.h>
#include <sys/epoll.h>
#include <pthread.h>

#define SOCKET_PATH "/tmp/mica/mica-create.socket"
#define BUFFER_SIZE 1024
#define MAX_EVENTS 64
#define MAX_CLIENTS 10
#define MAX_NAME_LEN 32
#define RESPONSE_SUCCESS "MICA-SUCCESS\n"
#define RESPONSE_FAILED "MICA-FAILED\n"

/* Function prototypes */
static void handle_client(int client_fd);

/* Message format matching mica.py's CreateMsg */
struct create_msg {
	uint32_t cpu;
	char name[MAX_NAME_LEN];
	char path[128];
	char ped[MAX_NAME_LEN];
	char ped_cfg[128];
	bool debug;
};

/* Listener unit structure */
struct listen_unit {
	char name[MAX_NAME_LEN];
	int socket_fd;
	char socket_path[128];
	struct listen_unit *next;
};

static volatile bool is_running = true;
static struct listen_unit *listener_list = NULL;
static pthread_mutex_t listener_mutex = PTHREAD_MUTEX_INITIALIZER;
static bool send_response = true;

static void signal_handler(int signum)
{
	if (signum == SIGINT || signum == SIGTERM) {
		printf("\nReceived signal %d, shutting down...\n", signum);
		is_running = false;
	}
}

static void print_create_msg(const struct create_msg *msg)
{
	printf("\nReceived Create Message:\n");
	printf("CPU: %u\n", msg->cpu);
	printf("Name: %.*s\n", (int)strnlen(msg->name, sizeof(msg->name)), msg->name);
	printf("Path: %.*s\n", (int)strnlen(msg->path, sizeof(msg->path)), msg->path);
	printf("Ped: %.*s\n", (int)strnlen(msg->ped, sizeof(msg->ped)), msg->ped);
	printf("PedCfg: %.*s\n", (int)strnlen(msg->ped_cfg, sizeof(msg->ped_cfg)), msg->ped_cfg);
	printf("Debug: %s\n", msg->debug ? "true" : "false");
	printf("\n");
}

static int safe_send(int fd, const char *msg, ssize_t len)
{
	ssize_t sent = 0;
	ssize_t ret;

	while (sent < len) {
		ret = send(fd, msg + sent, len - sent, 0);
		if (ret < 0) {
			if (errno == EINTR)
				continue;
			return -1;
		}
		sent += ret;
	}
	return 0;
}

static void print_hex_dump(const char *data, size_t len)
{
	size_t i;
	printf("\nReceived data (%zu bytes):\n", len);
	for (i = 0; i < len; i++) {
		printf("%02x ", (unsigned char)data[i]);
		if ((i + 1) % 16 == 0)
			printf("\n");
	}
	if (i % 16 != 0)
		printf("\n");
	printf("\n");
}

static void print_as_string(const char *data, size_t len)
{
	size_t i;
	printf("Received input as string: ");
	for (i = 0; i < len; i++) {
		char c = data[i];
		if (c >= 32 && c <= 126) {  // printable ASCII
			printf("%c", c);
		} else if (c == 0) {
			// \0
			printf("*");  
		} else {
			printf("\\x%02x", (unsigned char)c);  // show other non-printable as hex
		}
	}
	printf("\n");
}

static int setup_socket(const char *socket_path)
{
	int server_fd;
	struct sockaddr_un server_addr;
	struct stat st;

	if (stat(socket_path, &st) == 0)
		unlink(socket_path);

	char *dir = strdup(socket_path);
	if (!dir) {
		perror("strdup failed");
		return -1;
	}

	char *last_slash = strrchr(dir, '/');
	if (last_slash) {
		*last_slash = '\0';
		if (mkdir(dir, 0755) < 0 && errno != EEXIST) {
			perror("mkdir failed");
			free(dir);
			return -1;
		}
	}
	free(dir);

	server_fd = socket(AF_UNIX, SOCK_STREAM, 0);
	if (server_fd < 0) {
		perror("socket creation failed");
		return -1;
	}

	memset(&server_addr, 0, sizeof(server_addr));
	server_addr.sun_family = AF_UNIX;
	strncpy(server_addr.sun_path, socket_path, sizeof(server_addr.sun_path) - 1);

	if (bind(server_fd, (struct sockaddr *)&server_addr, sizeof(server_addr)) < 0) {
		perror("bind failed");
		close(server_fd);
		return -1;
	}

	if (listen(server_fd, MAX_CLIENTS) < 0) {
		perror("listen failed");
		close(server_fd);
		return -1;
	}

	return server_fd;
}

static void *epoll_thread(void *arg)
{
	int epoll_fd, nfds, i;
	struct epoll_event events[MAX_EVENTS];
	struct listen_unit *unit;

	epoll_fd = epoll_create1(0);
	if (epoll_fd < 0) {
		perror("epoll_create1 failed");
		return NULL;
	}

	pthread_mutex_lock(&listener_mutex);
	unit = listener_list;
	while (unit) {
		struct epoll_event ev;
		ev.events = EPOLLIN;
		ev.data.ptr = unit;
		if (epoll_ctl(epoll_fd, EPOLL_CTL_ADD, unit->socket_fd, &ev) < 0) {
			printf("Failed to add fd to epoll: %s\n", strerror(errno));
		}
		unit = unit->next;
	}
	pthread_mutex_unlock(&listener_mutex);

	while (is_running) {
		nfds = epoll_wait(epoll_fd, events, MAX_EVENTS, 1000);
		if (nfds < 0) {
			if (errno == EINTR)
				continue;
			perror("epoll_wait failed");
			break;
		}

		for (i = 0; i < nfds; i++) {
			unit = (struct listen_unit *)events[i].data.ptr;
			int client_fd = accept(unit->socket_fd, NULL, NULL);
			if (client_fd < 0) {
				if (errno == EINTR)
					continue;
				perror("accept failed");
				continue;
			}
			handle_client(client_fd);
			close(client_fd);
		}
	}

	close(epoll_fd);
	return NULL;
}

static int add_listener(const char *name, const char *socket_path)
{
	struct listen_unit *unit;
	int server_fd;

	server_fd = setup_socket(socket_path);
	if (server_fd < 0)
		return -1;

	unit = calloc(1, sizeof(*unit));
	if (!unit) {
		close(server_fd);
		return -1;
	}

	strncpy(unit->name, name, MAX_NAME_LEN - 1);
	strncpy(unit->socket_path, socket_path, sizeof(unit->socket_path) - 1);
	unit->socket_fd = server_fd;

	pthread_mutex_lock(&listener_mutex);
	unit->next = listener_list;
	listener_list = unit;
	pthread_mutex_unlock(&listener_mutex);

	return 0;
}

static void cleanup_listeners(void)
{
	struct listen_unit *current, *next;
	
	pthread_mutex_lock(&listener_mutex);
	current = listener_list;
	while (current) {
		next = current->next;
		close(current->socket_fd);
		unlink(current->socket_path);
		free(current);
		current = next;
	}
	listener_list = NULL;
	pthread_mutex_unlock(&listener_mutex);
}
#ifdef SIMPLE_MODE
static void handle_client(int client_fd)
{
	char buffer[BUFFER_SIZE];
	ssize_t bytes_received;

	bytes_received = recv(client_fd, buffer, BUFFER_SIZE - 1, 0);
	if (bytes_received < 0) {
		perror("recv failed");
		safe_send(client_fd, RESPONSE_FAILED, strlen(RESPONSE_FAILED));
		return;
	}

	buffer[bytes_received] = '\0';
	printf("Received string: %s\n", buffer);
	safe_send(client_fd, RESPONSE_SUCCESS, strlen(RESPONSE_SUCCESS));
}
#else
static void handle_client(int client_fd)
{
	char buffer[BUFFER_SIZE];
	ssize_t bytes_received;

	bytes_received = recv(client_fd, buffer, sizeof(struct create_msg), 0);
	if (bytes_received < 0) {
		perror("recv failed");
		if (send_response) {
			safe_send(client_fd, RESPONSE_FAILED, strlen(RESPONSE_FAILED));
		}
		return;
	}

	print_hex_dump(buffer, bytes_received);
	
	/* Always display input as string */
	print_as_string(buffer, bytes_received);

	if (bytes_received == sizeof(struct create_msg)) {
		struct create_msg *msg = (struct create_msg *)buffer;
		print_create_msg(msg);

		if (send_response) {
			safe_send(client_fd, RESPONSE_SUCCESS, strlen(RESPONSE_SUCCESS));
		}
	} else {
		buffer[bytes_received] = '\0';
		printf("Received control message: %s\n", buffer);
		if (send_response) {
			safe_send(client_fd, RESPONSE_SUCCESS, strlen(RESPONSE_SUCCESS));
		}
	}
}
#endif



int main(int argc, char *argv[])
{
	pthread_t thread;
	int opt;

	while ((opt = getopt(argc, argv, "r")) != -1) {
		switch (opt) {
		case 'q':
			send_response = false;
			break;
		default:
			printf("Usage: %s [-q]\n", argv[0]);
			printf("  -q: Not send response to client\n");
			return EXIT_FAILURE;
		}
	}

	signal(SIGINT, signal_handler);
	signal(SIGTERM, signal_handler);

	if (add_listener("mica-create", SOCKET_PATH) < 0) {
		printf("Failed to add listener\n");
		return EXIT_FAILURE;
	}

	if (pthread_create(&thread, NULL, epoll_thread, NULL) != 0) {
		perror("pthread_create failed");
		cleanup_listeners();
		return EXIT_FAILURE;
	}

	printf("Mock micad started. Listening on %s\n", SOCKET_PATH);
	printf("Press Ctrl+C to stop\n");
	printf("Response mode: %s\n", send_response ? "enabled" : "disabled");

	while (is_running) {
		sleep(1);
	}

	pthread_join(thread, NULL);
	cleanup_listeners();
	printf("Mock micad stopped.\n");

	return 0;
} 