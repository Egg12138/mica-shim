#define _GNU_SOURCE
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
#include <sys/time.h>
#include <time.h>

#define SOCKET_PATH "/tmp/mica/mica-create.socket"
#define SOCKET_DIR "/tmp/mica"
#define BUFFER_SIZE 1024
#define MAX_EVENTS 64
#define MAX_CLIENTS 10
#define MAX_NAME_LEN 32
#define MAX_FIRMWARE_PATH_LEN 128
#define MAX_CPU_STRING_LEN 128
#define MAX_NETWORK_LEN 512
#define RPMSG_TTY_DEV_PREFIX "/dev/ttyRPMSG_"
#define RESPONSE_SUCCESS "MICA-SUCCESS\n"
#define RESPONSE_FAILED "MICA-FAILED\n"

/* RTOS IO Simulation Constants */
#define MAX_RTOS_INSTANCES 4

#define INFO(fmt, ...) printf("[INFO] " fmt "\n", ##__VA_ARGS__)
#define ERROR(fmt, ...) printf("*ERROR* " fmt "\n", ##__VA_ARGS__)
#define FATAL(fmt, ...) do {\
	printf("!MICAD PANIC! " fmt "\n", ##__VA_ARGS__);\
	exit(EXIT_FAILURE);\
} while(0)


/* Message format matching mica.py's CreateMsg */
// Updated to match the new MicaClientConf structure
struct create_msg {
	/* required configs */
	char name[MAX_NAME_LEN];
	char path[MAX_FIRMWARE_PATH_LEN];
	/*optional configs for MICA*/
	char ped[MAX_NAME_LEN];
	char ped_cfg[MAX_FIRMWARE_PATH_LEN];
	bool debug;
	/*optional configs for pedestal */
	char cpu_str[MAX_CPU_STRING_LEN];
	int vcpu_num;
	int cpu_weight;
	int cpu_capacity;
    int memory;
    int maxmem;
    char network[MAX_NETWORK_LEN];
};

/* RTOS Communication Structures: removed (unused in mock). */

/* RTOS Instance Simulation */
struct rtos_instance {
    int instance_id;
    char name[MAX_NAME_LEN];
    uint32_t cpu_id;
    bool active;

    /* PTY (rpmsg-tty style) */
    int pty_master_fd;
    int pty_slave_fd;
    pthread_t pty_writer_thread;
    bool writer_started;
    char tty_symlink[64];
    char pts_slave_path[128];
    
    struct rtos_instance *next;
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
static int remove_socket(const char *client_name);
static void cleanup_listeners(void);
static void show_time(void);
static void print_all_client_statuses(void);
static void set_client_status(const char *name, const char *status);

/* RTOS IO Function prototypes */
static int create_rtos_instance(const char *name, uint32_t cpu_id);
static void destroy_rtos_instance(const char *name);
static struct rtos_instance *find_rtos_instance(const char *name);
/* Removed SHM/SEM/RPMSG and ring buffer prototypes in mock. */
static int create_tty_device(struct rtos_instance *rtos, const char *client_name);
static void remove_tty_device(struct rtos_instance *rtos);
static void *pty_writer_task(void *arg);

/* Created clients tracking */
struct client_entry {
	char name[MAX_NAME_LEN];
	char status[32];
	struct client_entry *next;
};

static volatile bool is_running = true;
static struct listen_unit *listener_list = NULL;
static struct client_entry *client_list = NULL;
static struct rtos_instance *rtos_instances = NULL;
static pthread_mutex_t listener_mutex = PTHREAD_MUTEX_INITIALIZER;
static pthread_mutex_t client_mutex = PTHREAD_MUTEX_INITIALIZER;
static pthread_mutex_t rtos_mutex = PTHREAD_MUTEX_INITIALIZER;
static bool send_response = true;
static int global_epoll_fd = -1;

static void signal_handler(int signum)
{
	if (signum == SIGINT || signum == SIGTERM) {
		INFO("Received signal %d, shutting down mock_micad...", signum);
		cleanup_listeners();
		is_running = false;
	}
}

static void print_create_msg(const struct create_msg *msg)
{
	printf("\nReceived Create Message:\n");
	printf("Name: %.*s\n", (int)strnlen(msg->name, sizeof(msg->name)), msg->name);
	printf("Path: %.*s\n", (int)strnlen(msg->path, sizeof(msg->path)), msg->path);
	printf("Ped: %.*s\n", (int)strnlen(msg->ped, sizeof(msg->ped)), msg->ped);
	printf("PedCfg: %.*s\n", (int)strnlen(msg->ped_cfg, sizeof(msg->ped_cfg)), msg->ped_cfg);
	printf("Debug: %s\n", msg->debug ? "true" : "false");
	printf("CPU String: %.*s\n", (int)strnlen(msg->cpu_str, sizeof(msg->cpu_str)), msg->cpu_str);
	printf("VCPU Num: %d\n", msg->vcpu_num);
	printf("CPU Weight: %d\n", msg->cpu_weight);
	printf("CPU Capacity: %d\n", msg->cpu_capacity);
    printf("Memory: %d\n", msg->memory);
    printf("MaxMem: %d\n", msg->maxmem);
    printf("Network: %.*s\n", (int)strnlen(msg->network, sizeof(msg->network)), msg->network);
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

static void respond_with_status(int fd, const char *msg)
{
	if (msg != NULL && fd >= 0) {
		safe_send(fd, msg, strlen(msg));
	}
	print_all_client_statuses();
}

static void set_client_status(const char *name, const char *status)
{
	struct client_entry *entry;
	if (!name || !status)
		return;

	pthread_mutex_lock(&client_mutex);
	entry = client_list;
	while (entry) {
		if (strncmp(entry->name, name, MAX_NAME_LEN) == 0) {
			snprintf(entry->status, sizeof(entry->status), "%s", status);
			break;
		}
		entry = entry->next;
	}
	pthread_mutex_unlock(&client_mutex);
}

static void print_all_client_statuses(void)
{
	struct client_entry *entry;
	pthread_mutex_lock(&client_mutex);
	entry = client_list;
	if (!entry) {
		printf("[CLIENT STATUS] (none)\n");
		pthread_mutex_unlock(&client_mutex);
		return;
	}

	printf("[CLIENT STATUS]");
	while (entry) {
		const char *status = entry->status[0] ? entry->status : "Unknown";
		printf(" %s:%s", entry->name, status);
		entry = entry->next;
		if (entry)
			printf(",");
	}
	printf("\n");
	pthread_mutex_unlock(&client_mutex);
}

/* Re-introduced utility helpers kept outside disabled SHM region. */
static void sanitize_client_name(char *dst, size_t dst_sz, const char *src)
{
    size_t i, j = 0;
    if (!dst || !src || dst_sz == 0)
        return;
    for (i = 0; src[i] != '\0' && j + 1 < dst_sz; i++) {
        char c = src[i];
        if ((c >= 'a' && c <= 'z') ||
            (c >= 'A' && c <= 'Z') ||
            (c >= '0' && c <= '9') ||
            c == '_' || c == '-') {
            dst[j++] = c;
        } else {
            dst[j++] = '_';
        }
    }
    dst[j] = '\0';
}

static struct rtos_instance *find_rtos_instance(const char *name)
{
    struct rtos_instance *rtos;
    pthread_mutex_lock(&rtos_mutex);
    rtos = rtos_instances;
    while (rtos) {
        if (strncmp(rtos->name, name, MAX_NAME_LEN) == 0) {
            pthread_mutex_unlock(&rtos_mutex);
            return rtos;
        }
        rtos = rtos->next;
    }
    pthread_mutex_unlock(&rtos_mutex);
    return NULL;
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

    snprintf(entry->name, sizeof(entry->name), "%s", name);
    snprintf(entry->status, sizeof(entry->status), "%s", "Created");

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
	if (remove_socket(name)) {
		ERROR("Failed to remove socket for client: %s\n", name);

	}
}

static void check_and_kill_existing_instances()
{
	FILE *fp;
	char line[256];
	int pid_found[16] = {0};
	int pid_count = 0;

	printf("Checking for existing mock_micad instances...\n");

	// Run lsof command to find processes using mica-create.socket
	fp = popen("lsof -t /tmp/mica/mica-create.socket 2>/dev/null", "r");
	if (fp == NULL) {
		printf("Failed to check for existing instances\n");
		return;
	}

	while (fgets(line, sizeof(line), fp) != NULL && pid_count < 16) {
		int pid = atoi(line);
		if (pid > 0 && pid != getpid()) {
			char cmd_path[256];
			snprintf(cmd_path, sizeof(cmd_path), "/proc/%d/comm", pid);
			FILE *cmd_fp = fopen(cmd_path, "r");
			if (cmd_fp) {
				char comm[64];
				if (fgets(comm, sizeof(comm), cmd_fp)) {
					char *newline = strchr(comm, '\n');
					if (newline) *newline = '\0';

					if (strstr(comm, "mock_mica") != NULL) {
						pid_found[pid_count++] = pid;
					}
				}
				fclose(cmd_fp);
			}
		}
	}
	pclose(fp);

	for (int i = 0; i < pid_count; i++) {
		printf("WARNING: Found existing mock_micad instance (PID %d), terminating it...\n", pid_found[i]);
		if (kill(pid_found[i], SIGTERM) == 0) {
			// graceful shutdown
			usleep(100000); // 100ms
			if (kill(pid_found[i], 0) == 0) {
				printf("Instance %d didn't terminate gracefully, sending SIGKILL\n", pid_found[i]);
				kill(pid_found[i], SIGKILL);
			}
			printf("Successfully terminated existing mock_micad instance (PID %d)\n", pid_found[i]);
		} else {
			printf("Failed to terminate instance %d: %s\n", pid_found[i], strerror(errno));
		}
	}

	if (pid_count > 0) {
		printf("Terminated %d existing mock_micad instance(s)\n", pid_count);
		usleep(200000);
	} else {
		printf("No existing mock_micad instances found\n");
	}
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

static int remove_socket(const char *client_name)
{
	char socket_path[128];
	snprintf(socket_path, sizeof(socket_path), "%s/%s.socket", SOCKET_DIR, client_name);
	int fd;

	fd = socket(AF_UNIX, SOCK_STREAM, 0);
	if (fd < 0) {
		perror("socket creation failed");
		return -1;
	}
	close(fd);
	if (unlink(socket_path) == 0) {
		INFO("Removed client socket: %s", socket_path);
	} else {
		INFO("Client socket already removed or not found: %s", socket_path);
	}
	return 0;

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

	snprintf(unit->name, sizeof(unit->name), "%s", client_name);
	snprintf(unit->socket_path, sizeof(unit->socket_path), "%s", socket_path);
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
/* RTOS IO Implementation Functions */

static int create_tty_device(struct rtos_instance *rtos, const char *client_name)
{
    static const char *fallback_prefix = "/tmp/ttyRPMSG_";
    const char *prefixes[] = {RPMSG_TTY_DEV_PREFIX, fallback_prefix};
    int ret;
    int master_fd = -1, slave_fd = -1;
    char pts_name[128] = {0};
    char suffix[MAX_NAME_LEN] = {0};
    bool linked = false;

    if (!rtos || !client_name)
        return -1;

    sanitize_client_name(suffix, sizeof(suffix), client_name);

    master_fd = posix_openpt(O_RDWR | O_NOCTTY);
    if (master_fd == -1)
        goto err;
    ret = grantpt(master_fd);
    if (ret != 0)
        goto err;
    ret = unlockpt(master_fd);
    if (ret != 0)
        goto err;
    ret = ptsname_r(master_fd, pts_name, sizeof(pts_name));
    if (ret != 0)
        goto err;

    snprintf(rtos->pts_slave_path, sizeof(rtos->pts_slave_path), "%s", pts_name);

    for (size_t i = 0; i < sizeof(prefixes)/sizeof(prefixes[0]); i++) {
        snprintf(rtos->tty_symlink, sizeof(rtos->tty_symlink), "%s%s", prefixes[i], suffix);
        unlink(rtos->tty_symlink);
        if (symlink(pts_name, rtos->tty_symlink) == 0) {
            linked = true;
            break;
        }
    }

    if (!linked)
        goto err;

    /* Keep slave open to avoid EIO on master */
    slave_fd = open(pts_name, O_RDWR | O_NOCTTY);
    if (slave_fd == -1)
        goto err_unlink;

    rtos->pty_master_fd = master_fd;
    rtos->pty_slave_fd = slave_fd;
    INFO("PTY created for %s: symlink=%s target=%s", client_name, rtos->tty_symlink, rtos->pts_slave_path);
    return 0;

err_unlink:
    unlink(rtos->tty_symlink);
err:
    if (master_fd != -1)
        close(master_fd);
    if (slave_fd != -1)
        close(slave_fd);
    rtos->pty_master_fd = -1;
    rtos->pty_slave_fd = -1;
    rtos->tty_symlink[0] = '\0';
    return -1;
}

static void remove_tty_device(struct rtos_instance *rtos)
{
    if (!rtos)
        return;
    if (rtos->pty_master_fd > -1) {
        close(rtos->pty_master_fd);
        rtos->pty_master_fd = -1;
    }
    if (rtos->pty_slave_fd > -1) {
        close(rtos->pty_slave_fd);
        rtos->pty_slave_fd = -1;
    }
    if (rtos->tty_symlink[0] != '\0') {
        if (unlink(rtos->tty_symlink) == 0) {
            INFO("Removed PTY symlink: %s", rtos->tty_symlink);
        } else {
            INFO("PTY symlink already removed or not found: %s", rtos->tty_symlink);
        }
        rtos->tty_symlink[0] = '\0';
    }
}

/* Periodically write output to the PTY master to simulate client console */
static void *pty_writer_task(void *arg)
{
    struct rtos_instance *rtos = (struct rtos_instance *)arg;
    if (!rtos)
        pthread_exit(NULL);
    const useconds_t interval_us = 200 * 1000; /* 200ms */
    int counter = 0;
    while (rtos->active && is_running) {
        if (rtos->pty_master_fd > -1) {
            char buf[256];
            int n = snprintf(buf, sizeof(buf), "[%s] tick=%d time=%ld\n", rtos->name, counter++, time(NULL));
            if (n > 0) {
                ssize_t written = write(rtos->pty_master_fd, buf, (size_t)n);
                (void)written;
            }
        }
        usleep(interval_us);
    }
    pthread_exit(NULL);
}

static void free_rtos_instance(struct rtos_instance *rtos)
{
    if (!rtos)
        return;
    rtos->active = false;
    if (rtos->writer_started) {
        pthread_join(rtos->pty_writer_thread, NULL);
        rtos->writer_started = false;
    }
    remove_tty_device(rtos);
    free(rtos);
}

static int create_rtos_instance(const char *name, uint32_t cpu_id)
{
    struct rtos_instance *rtos;
    static int next_instance_id = 0;

	/* Check if instance already exists */
	if (find_rtos_instance(name)) {
		printf("RTOS instance '%s' already exists\n", name);
		return 0;
	}
	
	/* Allocate new instance */
	rtos = calloc(1, sizeof(*rtos));
	if (!rtos) {
		perror("Failed to allocate RTOS instance");
		return -1;
	}

	/* Initialize instance */
	rtos->instance_id = next_instance_id++;
	snprintf(rtos->name, sizeof(rtos->name), "%s", name);
	rtos->cpu_id = cpu_id;
	rtos->active = true;

	/* Create PTY device for console */
	if (create_tty_device(rtos, name) != 0) {
		free(rtos);
		return -1;
	}

	/* Start PTY writer thread */
	if (pthread_create(&rtos->pty_writer_thread, NULL, pty_writer_task, rtos) != 0) {
		perror("Failed to create PTY writer thread");
		free_rtos_instance(rtos);
		return -1;
	}
	rtos->writer_started = true;

	/* Add to list */
	pthread_mutex_lock(&rtos_mutex);
	rtos->next = rtos_instances;
	rtos_instances = rtos;
	pthread_mutex_unlock(&rtos_mutex);

	printf("RTOS instance '%s' created successfully (ID=%d, CPU=%u)\n", 
	       name, rtos->instance_id, cpu_id);
	return 0;
}

static void destroy_rtos_instance(const char *name)
{
    struct rtos_instance *rtos, *prev = NULL;

	pthread_mutex_lock(&rtos_mutex);

	/* Find and remove from list */
	rtos = rtos_instances;
	while (rtos) {
		if (strncmp(rtos->name, name, MAX_NAME_LEN) == 0) {
			if (prev)
				prev->next = rtos->next;
			else
				rtos_instances = rtos->next;
			break;
		}
		prev = rtos;
		rtos = rtos->next;
	}

	if (!rtos) {
		pthread_mutex_unlock(&rtos_mutex);
		return;
	}

	pthread_mutex_unlock(&rtos_mutex);

	free_rtos_instance(rtos);
	printf("RTOS instance '%s' destroyed\n", name);
}

#define TSZ 64
static void show_time(void)
{
	time_t current_time;
	struct tm *local_time_info;
	char time_string[TSZ];
	current_time = time(NULL);
	if (current_time == (time_t) - 1)
		FATAL("failed to get current time");

	local_time_info  = localtime(&current_time);
	if (local_time_info == NULL)
		FATAL("failed to get local time");
	

	size_t bytes = strftime(time_string, TSZ, "%H:%M:%S", local_time_info);
	if (bytes == 0	)
		FATAL("failed to format time string");
	INFO("********%s********", time_string);
}

static void cleanup_listeners(void)
{
    struct listen_unit *current, *next;
    struct client_entry *client, *client_next;
    struct rtos_instance *rtos, *rtos_next;

    INFO("Starting cleanup of mock_micad resources...");

    /* Stop and destroy all RTOS instances safely */
    pthread_mutex_lock(&rtos_mutex);
    rtos = rtos_instances;
    rtos_instances = NULL;
    pthread_mutex_unlock(&rtos_mutex);

    while (rtos) {
        rtos_next = rtos->next;
        INFO("Cleaning up RTOS instance: %s", rtos->name);
        free_rtos_instance(rtos);
        rtos = rtos_next;
    }
    INFO("All RTOS instances cleaned up");

	pthread_mutex_lock(&listener_mutex);
	current = listener_list;
	while (current) {
		next = current->next;
		INFO("Removing listener socket: %s", current->socket_path);
		close(current->socket_fd);
		unlink(current->socket_path);
		free(current);
		current = next;
	}
	listener_list = NULL;
	pthread_mutex_unlock(&listener_mutex);
	INFO("All listener sockets cleaned up");

	pthread_mutex_lock(&client_mutex);
	client = client_list;
	while (client) {
		client_next = client->next;
		INFO("Cleaning up client: %s (status: %s)", client->name, client->status[0] ? client->status : "Unknown");
		/* Remove client socket for each client */
		remove_socket(client->name);
		free(client);
		client = client_next;
	}
	client_list = NULL;
	pthread_mutex_unlock(&client_mutex);
	INFO("All client entries cleaned up");

	INFO("Mock_micad resource cleanup completed");

	/* Clean up main socket */
	if (unlink(SOCKET_PATH) == 0) {
		INFO("Removed main socket: %s", SOCKET_PATH);
	} else {
		INFO("Main socket already removed or not found: %s", SOCKET_PATH);
	}
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
			respond_with_status(client_fd, RESPONSE_FAILED);
		} else {
			respond_with_status(-1, NULL);
		}
		return;
	}

	buffer[bytes_received] = '\0';
	printf("Received control command for client '%s': %s\n", client_name, buffer);

    /* Handle different control commands with state checks */
    int success = 0; /* 1=ok, 0=fail */
    if (strncmp(buffer, "start", 5) == 0) {
        INFO("Starting client: %s\n", client_name);
        struct client_entry *e;
        pthread_mutex_lock(&client_mutex);
        e = client_list;
        while (e && strncmp(e->name, client_name, MAX_NAME_LEN) != 0) e = e->next;
        if (e && strcasecmp(e->status, "Running") == 0) {
            ERROR("Cannot start client '%s' - already Running", client_name);
            success = 0;
        } else if (e) {
            snprintf(e->status, sizeof(e->status), "%s", "Running");
            success = 1;
        }
        pthread_mutex_unlock(&client_mutex);
    } else if (strncmp(buffer, "stop", 4) == 0) {
        INFO("Stopping client: %s\n", client_name);
        struct client_entry *e;
        pthread_mutex_lock(&client_mutex);
        e = client_list;
        while (e && strncmp(e->name, client_name, MAX_NAME_LEN) != 0) e = e->next;
        if (e) {
            if (strcasecmp(e->status, "Created") == 0) {
                INFO("Should not stop client '%s' - status is 'Created', must be 'Running' or 'Stopped'", client_name);
                success = 0;
            } else {
                snprintf(e->status, sizeof(e->status), "%s", "Stopped");
                success = 1;
            }
        }
        pthread_mutex_unlock(&client_mutex);
    } else if (strncmp(buffer, "status", 6) == 0) {
        INFO("Getting status for client: %s\n", client_name);
        success = 1;
    } else if (strncmp(buffer, "rm", 2) == 0) {
        INFO("Removing client: %s\n", client_name);
        /* Remove RTOS instance */
        destroy_rtos_instance(client_name);
        /* Remove from client list and cleanup */
        remove_client(client_name);
        success = 1;
    } else {
        INFO("Unknown command for client '%s': %s\n", client_name, buffer);
        success = 0;
    }

    if (send_response) {
        respond_with_status(client_fd, success ? RESPONSE_SUCCESS : RESPONSE_FAILED);
    } else {
        respond_with_status(-1, NULL);
    }
	printf(">>");
}

#ifdef SIMPLE_MODE
static void create_client(int client_fd)
{
	char buffer[BUFFER_SIZE];
	ssize_t bytes_received;

	bytes_received = recv(client_fd, buffer, BUFFER_SIZE - 1, 0);
	if (bytes_received < 0) {
		perror("recv failed");
		respond_with_status(client_fd, RESPONSE_FAILED);
		return;
	}

	buffer[bytes_received] = '\0';
	printf("Received string: %s\n", buffer);
	respond_with_status(client_fd, RESPONSE_SUCCESS);
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
			respond_with_status(client_fd, RESPONSE_FAILED);
		} else {
			respond_with_status(-1, NULL);
		}
		return;
	}

	show_time();
	print_hex_dump(buffer, bytes_received);
	
	/* Always display input as string */
	print_as_string(buffer, bytes_received);

	/* Check if received enough data for the struct */
	printf("bytes_received: %ld\n", bytes_received);
	printf("sizeof(struct create_msg): %ld\n", sizeof(struct create_msg));
    if (bytes_received >= offsetof(struct create_msg, debug) + sizeof(bool)) {
        struct create_msg *msg = (struct create_msg *)buffer;
        print_create_msg(msg);

		/* Extract client name (remove null padding) */
		char client_name[MAX_NAME_LEN];
		size_t name_len = strnlen(msg->name, MAX_NAME_LEN);
		memcpy(client_name, msg->name, name_len);
		client_name[name_len] = '\0';

		/* Check if client already exists */
		if (client_exists(client_name)) {
			ERROR("Client '%s' already exists - cannot create duplicate", client_name);
			if (send_response) {
				respond_with_status(client_fd, RESPONSE_FAILED);
			} else {
				respond_with_status(-1, NULL);
			}
			return;
		} else {
			printf("Client '%s' does not exist, register it\n", client_name);
		}

		/* Create client socket for this new client */
		printf("setting up client socket for: %s\n", client_name);
		int create_ret = create_client_socket(client_name);
		
		/* Also create RTOS instance for IO handling */
		if (create_ret == 0) {
			// Parse the CPU string to get the first CPU ID for the RTOS instance
			// For simplicity, we just use the first character of the cpu_str as a placeholder
			// A full implementation would parse the cpu_str properly
			uint32_t cpu_id = 0;
			if (msg->cpu_str[0] >= '0' && msg->cpu_str[0] <= '9') {
				cpu_id = msg->cpu_str[0] - '0';
			}
			create_ret = create_rtos_instance(client_name, cpu_id);
		}
		
		if (create_ret == 0) {
			register_client(client_name);
			printf("Successfully added client<%s> with RTOS simulation and client socket\n", client_name);
			if (send_response) {
				respond_with_status(client_fd, RESPONSE_SUCCESS);
			} else {
				respond_with_status(-1, NULL);
			}
		} else {
			ERROR("Failed to create client socket/RTOS for '%s' (error: %d)", client_name, create_ret);
			remove_socket(client_name);
			if (send_response) {
				respond_with_status(client_fd, RESPONSE_FAILED);
			} else {
				respond_with_status(-1, NULL);
			}
		}
    } else {
        buffer[bytes_received] = '\0';
        printf("Received control message: %s[%ld]\n", buffer, bytes_received);

        // Support simple textual create: "create <client_name>"
        const char *p = buffer;
        while (*p == ' ') p++;
        const char *cmd = p;
        // find first token
        while (*p && *p != ' ' && *p != '\n' && *p != '\t') p++;
        size_t cmdlen = p - cmd;
        while (*p == ' ') p++;
        const char *arg = p;
        // arg is rest of line
        char client_name[MAX_NAME_LEN] = {0};
        if (cmdlen == 6 && strncasecmp(cmd, "create", 6) == 0) {
            // parse name
            size_t i = 0;
            while (arg[i] && arg[i] != ' ' && arg[i] != '\n' && arg[i] != '\t' && i < MAX_NAME_LEN-1) {
                client_name[i] = arg[i];
                i++;
            }
            client_name[i] = '\0';
            if (client_name[0] == '\0') {
                ERROR("Create command missing client name");
                if (send_response) respond_with_status(client_fd, RESPONSE_FAILED); else respond_with_status(-1, NULL);
                return;
            }
            if (client_exists(client_name)) {
                ERROR("Client '%s' already exists - cannot create duplicate", client_name);
                if (send_response) respond_with_status(client_fd, RESPONSE_FAILED); else respond_with_status(-1, NULL);
                return;
            }
            printf("creating client via textual command: %s\n", client_name);
            int create_ret = create_client_socket(client_name);
            if (create_ret == 0) {
                // default CPU id 0
                if (create_rtos_instance(client_name, 0) == 0) {
                    register_client(client_name);
                    set_client_status(client_name, "Created");
                    if (send_response) respond_with_status(client_fd, RESPONSE_SUCCESS); else respond_with_status(-1, NULL);
                    return;
                }
            }
            ERROR("Failed to create client socket/RTOS for '%s'", client_name);
            remove_socket(client_name);
            if (send_response) respond_with_status(client_fd, RESPONSE_FAILED); else respond_with_status(-1, NULL);
            return;
        }

        // Unknown small control on create socket
        ERROR("Invalid command on create socket: '%s'. Use 'create <name>' for new clients or send 'start/stop/status' to client-specific socket (/tmp/mica/<client>.socket)", buffer);
        if (send_response) {
            respond_with_status(client_fd, RESPONSE_FAILED);
        } else {
            respond_with_status(-1, NULL);
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

	check_and_kill_existing_instances();

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
	INFO("Main loop exited, performing final cleanup...");
	cleanup_listeners();
	INFO("Mock micad stopped successfully.");

	return 0;
} 
