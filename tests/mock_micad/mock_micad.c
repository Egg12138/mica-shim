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
#define SOCKET_DIR "/tmp/mica"
#define BUFFER_SIZE 1024
#define MAX_EVENTS 64
#define MAX_CLIENTS 10
#define MAX_NAME_LEN 32
#define RESPONSE_SUCCESS "MICA-SUCCESS\n"
#define RESPONSE_FAILED "MICA-FAILED\n"

/* Message format matching mica.py's CreateMsg */
struct create_msg {
	uint32_t cpu;
	char name[MAX_NAME_LEN];
	char path[128];
	char ped[MAX_NAME_LEN];
	char ped_cfg[128];
};

/* Listener unit structure */
struct listen_unit {
	char name[MAX_NAME_LEN];
	int socket_fd;
	char socket_path[128];
	struct listen_unit *next;
	bool is_create_socket;  /* true for mica-create.socket, false for client sockets */
};

/* Function prototypes */
static void handle_client(int client_fd);
static void handle_client_ctrl(int client_fd, struct listen_unit *unit);

/* Created clients tracking */
struct client_entry {
	char name[MAX_NAME_LEN];
	struct client_entry *next;
};

static volatile bool is_running = true;
static struct listen_unit *listener_list = NULL;
static struct client_entry *client_list = NULL;
static pthread_mutex_t listener_mutex = PTHREAD_MUTEX_INITIALIZER;
static pthread_mutex_t client_mutex = PTHREAD_MUTEX_INITIALIZER;
static bool send_response = true;
static int global_epoll_fd = -1;

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
	// printf("Debug: %s\n", msg->debug ? "true" : "false");
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

static bool client_exists(const char *name)
{
	struct client_entry *entry;
	
	printf("verify client_existance: %s\n", name);
	pthread_mutex_lock(&client_mutex);
	entry = client_list;
	while (entry) {
		if (strncmp(entry->name, name, MAX_NAME_LEN) == 0) {
			printf("\tentry member fetched: %s\n", name);
			pthread_mutex_unlock(&client_mutex);
			return true;
		}
		entry = entry->next;
	}
	pthread_mutex_unlock(&client_mutex);
	return false;
}

static void register_client(const char *name)
{
	struct client_entry *entry;
	
	entry = calloc(1, sizeof(*entry));
	if (!entry)
		return;
	
	strncpy(entry->name, name, MAX_NAME_LEN - 1);
	
	pthread_mutex_lock(&client_mutex);
	entry->next = client_list;
	client_list = entry;
	pthread_mutex_unlock(&client_mutex);
}

static void remove_client(const char *name)
{
	struct client_entry *entry, *prev = NULL;
	
	pthread_mutex_lock(&client_mutex);
	entry = client_list;
	while (entry) {
		if (strncmp(entry->name, name, MAX_NAME_LEN) == 0) {
			if (prev)
				prev->next = entry->next;
			else
				client_list = entry->next;
			free(entry);
			break;
		}
		prev = entry;
		entry = entry->next;
	}
	pthread_mutex_unlock(&client_mutex);
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

static int create_client_socket(const char *client_name)
{
	char socket_path[128];
	struct listen_unit *unit;
	struct epoll_event ev;
	int server_fd;

	snprintf(socket_path, sizeof(socket_path), "%s/%s.socket", SOCKET_DIR, client_name);
	
	/* check socket_patch exists and is of a socket type */
	struct stat st;
	if (stat(socket_path, &st) == 0) {
		if (S_ISSOCK(st.st_mode)) {
			printf("Client socket already exists for %s\n", client_name);
			return 0;
		}
	}

	server_fd = setup_socket(socket_path);
	if (server_fd < 0) {
		printf("Failed to create client socket for %s\n", client_name);
		return -1;
	}

	unit = calloc(1, sizeof(*unit));
	if (!unit) {
		close(server_fd);
		return -1;
	}

	strncpy(unit->name, client_name, MAX_NAME_LEN - 1);
	strncpy(unit->socket_path, socket_path, sizeof(unit->socket_path) - 1);
	unit->socket_fd = server_fd;
	unit->is_create_socket = false;

	/* Add to epoll */
	ev.events = EPOLLIN;
	ev.data.ptr = unit;
	if (epoll_ctl(global_epoll_fd, EPOLL_CTL_ADD, server_fd, &ev) < 0) {
		printf("Failed to add client socket to epoll: %s\n", strerror(errno));
		close(server_fd);
		free(unit);
		return -1;
	}

	/* Add to listener list */
	pthread_mutex_lock(&listener_mutex);
	unit->next = listener_list;
	listener_list = unit;
	pthread_mutex_unlock(&listener_mutex);

	printf("Created client socket: %s\n", socket_path);
	return 0;
}

static void *epoll_thread(void *arg)
{
	int nfds, i;
	struct epoll_event events[MAX_EVENTS];
	struct listen_unit *unit;

	global_epoll_fd = epoll_create1(0);
	if (global_epoll_fd < 0) {
		perror("epoll_create1 failed");
		return NULL;
	}

	pthread_mutex_lock(&listener_mutex);
	unit = listener_list;
	while (unit) {
		struct epoll_event ev;
		ev.events = EPOLLIN;
		ev.data.ptr = unit;
		if (epoll_ctl(global_epoll_fd, EPOLL_CTL_ADD, unit->socket_fd, &ev) < 0) {
			printf("Failed to add fd to epoll: %s\n", strerror(errno));
		}
		unit = unit->next;
	}
	pthread_mutex_unlock(&listener_mutex);

	while (is_running) {
		nfds = epoll_wait(global_epoll_fd, events, MAX_EVENTS, 1000);
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
			
			if (unit->is_create_socket) {
				handle_client(client_fd);
			} else {
				handle_client_ctrl(client_fd, unit);
			}
			close(client_fd);
		}
	}

	close(global_epoll_fd);
	return NULL;
}

static int add_listener(const char *name, const char *socket_path, bool is_create_socket)
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
	unit->is_create_socket = is_create_socket;

	pthread_mutex_lock(&listener_mutex);
	unit->next = listener_list;
	listener_list = unit;
	pthread_mutex_unlock(&listener_mutex);

	return 0;
}

static void cleanup_listeners(void)
{
	struct listen_unit *current, *next;
	struct client_entry *client, *client_next;
	
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
	
	pthread_mutex_lock(&client_mutex);
	client = client_list;
	while (client) {
		client_next = client->next;
		free(client);
		client = client_next;
	}
	client_list = NULL;
	pthread_mutex_unlock(&client_mutex);
}

static void handle_client_ctrl(int client_fd, struct listen_unit *unit)
{
	char buffer[BUFFER_SIZE];
	ssize_t bytes_received;
	const char *client_name = unit->name;

	bytes_received = recv(client_fd, buffer, BUFFER_SIZE - 1, 0);
	if (bytes_received < 0) {
		perror("recv failed");
		if (send_response) {
			safe_send(client_fd, RESPONSE_FAILED, strlen(RESPONSE_FAILED));
		}
		return;
	}

	buffer[bytes_received] = '\0';
	printf("Received control command for client '%s': %s\n", client_name, buffer);

	/* Handle different control commands */
	if (strncmp(buffer, "start", 5) == 0) {
		printf("Starting client: %s\n", client_name);
	} else if (strncmp(buffer, "stop", 4) == 0) {
		printf("Stopping client: %s\n", client_name);
	} else if (strncmp(buffer, "status", 6) == 0) {
		printf("Getting status for client: %s\n", client_name);
	} else if (strncmp(buffer, "rm", 2) == 0) {
		printf("Removing client: %s\n", client_name);
		/* Remove from client list and cleanup */
		remove_client(client_name);
		/* The actual socket cleanup happens in the caller */
	} else {
		printf("Unknown command for client '%s': %s\n", client_name, buffer);
	}

	if (send_response) {
		safe_send(client_fd, RESPONSE_SUCCESS, strlen(RESPONSE_SUCCESS));
	}
}

#ifdef SIMPLE_MODE
static void create_client(int client_fd)
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

	/* Size matched */
	printf("bytes_received: %ld\n", bytes_received);
	printf("sizeof(struct create_msg): %ld\n", sizeof(struct create_msg));
	if (bytes_received == sizeof(struct create_msg)) {
		struct create_msg *msg = (struct create_msg *)buffer;
		print_create_msg(msg);

		/* Extract client name (remove null padding) */
		char client_name[MAX_NAME_LEN];
		size_t name_len = strnlen(msg->name, MAX_NAME_LEN);
		memcpy(client_name, msg->name, name_len);
		client_name[name_len] = '\0';

		/* Check if client already exists */
		if (client_exists(client_name)) {
			printf("Client '%s' already exists, do not re-create it\n", client_name);
			if (send_response) {
				safe_send(client_fd, RESPONSE_SUCCESS, strlen(RESPONSE_FAILED));
			}
			return;
		} else {
			printf("Client '%s' does not exist, register it\n", client_name);
		}

		/* Create client socket for this new client */
		printf("setting up client socket for: %s\n", client_name);
		int create_ret  = create_client_socket(client_name);
		if (create_ret == 0) {
			register_client(client_name);
			printf("Successfully added client<%s> and the client socket\n", client_name);
			if (send_response) {
				safe_send(client_fd, RESPONSE_SUCCESS, strlen(RESPONSE_SUCCESS));
			}
		} else {
			printf("Err: %d. Failed to create client socket for: %s\n", create_ret, client_name);
			if (send_response) {
				safe_send(client_fd, RESPONSE_FAILED, strlen(RESPONSE_FAILED));
			}
		}
	} else {
		buffer[bytes_received] = '\0';
		printf("Received control message: %s[%ld]\n", buffer, bytes_received);
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

	while ((opt = getopt(argc, argv, "q")) != -1) {
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

	if (add_listener("mica-create", SOCKET_PATH, true) < 0) {
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