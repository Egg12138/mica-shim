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
#include <sys/mman.h>
#include <semaphore.h>
#include <sys/time.h>
#include <time.h>

#define SOCKET_PATH "/tmp/mica/mica-create.socket"
#define SOCKET_DIR "/tmp/mica"
#define BUFFER_SIZE 1024
#define MAX_EVENTS 64
#define MAX_CLIENTS 10
#define MAX_NAME_LEN 32
#define RESPONSE_SUCCESS "MICA-SUCCESS\n"
#define RESPONSE_FAILED "MICA-FAILED\n"

/* RTOS IO Simulation Constants */
#define OPENAMP_SHM_SIZE  0x1000000    /* 16M */
#define OPENAMP_SHM_COPY_SIZE 0x100000 /* 1M */
#define SHM_NAME "/my_shared_memory_%d"
#define SEM_USER_TO_MICAD "/sem_user_to_mciad_%d"
#define SEM_MICAD_TO_USER "/sem_mciad_to_user_%d"
#define RING_BUFFER_SIZE 4096
#define MAX_RTOS_INSTANCES 4

/* RPMSG Message Types */
#define RPMSG_TYPE_RPC 1
#define RPMSG_TYPE_UMT 2  
#define RPMSG_TYPE_PTY 3
#define RPMSG_TYPE_DEBUG 4

/* Message format matching mica.py's CreateMsg */
struct create_msg {
	uint32_t cpu;
	char name[MAX_NAME_LEN];
	char path[128];
	char ped[MAX_NAME_LEN];
	char ped_cfg[128];
};

/* RTOS Communication Structures */
typedef struct {
    unsigned long phy_addr;
    int data_len;
    int instance_id;
    int rcv_data_len;
    int lock;
    char rcv_buffer[256];
} process_shared_data_t;

typedef struct {
    unsigned long phy_addr;
    int data_len;
} umt_send_msg_t;

typedef struct {
    uint32_t msg_type;
    uint32_t src_addr;
    uint32_t dst_addr;
    uint32_t data_len;
    char data[BUFFER_SIZE];
} rpmsg_message_t;

/* Ring Buffer Structure */
typedef struct ring_buffer {
    unsigned int in;
    unsigned int out;
    unsigned int len;
    unsigned int esize;
    char data[0];
} ring_buffer_t;

/* RTOS Instance Simulation */
struct rtos_instance {
    int instance_id;
    char name[MAX_NAME_LEN];
    uint32_t cpu_id;
    bool active;
    
    /* Shared Memory */
    process_shared_data_t *shared_memory;
    int shm_fd;
    
    /* Semaphores */
    sem_t *sem_user_to_micad;
    sem_t *sem_micad_to_user;
    
    /* Ring Buffers */
    ring_buffer_t *tx_ring;
    ring_buffer_t *rx_ring;
    
    /* Thread for RTOS simulation */
    pthread_t rtos_thread;
    pthread_mutex_t data_mutex;
    
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

/* RTOS IO Function prototypes */
static int create_rtos_instance(const char *name, uint32_t cpu_id);
static void destroy_rtos_instance(const char *name);
static struct rtos_instance *find_rtos_instance(const char *name);
static int init_shared_memory(struct rtos_instance *rtos);
static int init_semaphores(struct rtos_instance *rtos);
static int init_ring_buffers(struct rtos_instance *rtos);
static void *rtos_simulation_thread(void *arg);
static void simulate_rpmsg_processing(struct rtos_instance *rtos, rpmsg_message_t *msg);
static int ring_buffer_write(ring_buffer_t *rb, const char *data, int len);
static int ring_buffer_read(ring_buffer_t *rb, char *data, int len);
static void simulate_rtos_response(struct rtos_instance *rtos, const char *input_data, int input_len);

/* Created clients tracking */
struct client_entry {
	char name[MAX_NAME_LEN];
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

/* RTOS IO Implementation Functions */

static int ring_buffer_write(ring_buffer_t *rb, const char *data, int len)
{
	int available, to_write, first_chunk;
	
	if (!rb || !data || len <= 0)
		return -1;
		
	available = rb->len - (rb->in - rb->out);
	if (available == 0)
		return 0; /* Buffer full */
		
	to_write = (len < available) ? len : available;
	first_chunk = rb->len - (rb->in % rb->len);
	
	if (first_chunk >= to_write) {
		memcpy(rb->data + (rb->in % rb->len), data, to_write);
	} else {
		memcpy(rb->data + (rb->in % rb->len), data, first_chunk);
		memcpy(rb->data, data + first_chunk, to_write - first_chunk);
	}
	
	rb->in += to_write;
	return to_write;
}

static int ring_buffer_read(ring_buffer_t *rb, char *data, int len)
{
	int available, to_read, first_chunk;
	
	if (!rb || !data || len <= 0)
		return -1;
		
	available = rb->in - rb->out;
	if (available == 0)
		return 0; /* Buffer empty */
		
	to_read = (len < available) ? len : available;
	first_chunk = rb->len - (rb->out % rb->len);
	
	if (first_chunk >= to_read) {
		memcpy(data, rb->data + (rb->out % rb->len), to_read);
	} else {
		memcpy(data, rb->data + (rb->out % rb->len), first_chunk);
		memcpy(data + first_chunk, rb->data, to_read - first_chunk);
	}
	
	rb->out += to_read;
	return to_read;
}

static int init_shared_memory(struct rtos_instance *rtos)
{
	char shm_name[64];
	
	snprintf(shm_name, sizeof(shm_name), SHM_NAME, rtos->instance_id);
	
	/* Create shared memory */
	rtos->shm_fd = shm_open(shm_name, O_CREAT | O_RDWR, 0666);
	if (rtos->shm_fd < 0) {
		perror("shm_open failed");
		return -1;
	}
	
	if (ftruncate(rtos->shm_fd, sizeof(process_shared_data_t)) < 0) {
		perror("ftruncate failed");
		close(rtos->shm_fd);
		return -1;
	}
	
	/* Map shared memory */
	rtos->shared_memory = mmap(NULL, sizeof(process_shared_data_t),
				   PROT_READ | PROT_WRITE, MAP_SHARED,
				   rtos->shm_fd, 0);
	if (rtos->shared_memory == MAP_FAILED) {
		perror("mmap failed");
		close(rtos->shm_fd);
		return -1;
	}
	
	/* Initialize shared memory */
	memset(rtos->shared_memory, 0, sizeof(process_shared_data_t));
	rtos->shared_memory->instance_id = rtos->instance_id;
	rtos->shared_memory->lock = 0;
	
	printf("RTOS[%s]: Shared memory initialized at %p\n", rtos->name, rtos->shared_memory);
	return 0;
}

static int init_semaphores(struct rtos_instance *rtos)
{
	char sem_name[64];
	
	/* Create user->micad semaphore */
	snprintf(sem_name, sizeof(sem_name), SEM_USER_TO_MICAD, rtos->instance_id);
	rtos->sem_user_to_micad = sem_open(sem_name, O_CREAT, 0666, 0);
	if (rtos->sem_user_to_micad == SEM_FAILED) {
		perror("sem_open user_to_micad failed");
		return -1;
	}
	
	/* Create micad->user semaphore */
	snprintf(sem_name, sizeof(sem_name), SEM_MICAD_TO_USER, rtos->instance_id);
	rtos->sem_micad_to_user = sem_open(sem_name, O_CREAT, 0666, 0);
	if (rtos->sem_micad_to_user == SEM_FAILED) {
		perror("sem_open micad_to_user failed");
		sem_close(rtos->sem_user_to_micad);
		return -1;
	}
	
	printf("RTOS[%s]: Semaphores initialized\n", rtos->name);
	return 0;
}

static int init_ring_buffers(struct rtos_instance *rtos)
{
	/* Allocate ring buffers */
	rtos->tx_ring = malloc(sizeof(ring_buffer_t) + RING_BUFFER_SIZE);
	rtos->rx_ring = malloc(sizeof(ring_buffer_t) + RING_BUFFER_SIZE);
	
	if (!rtos->tx_ring || !rtos->rx_ring) {
		free(rtos->tx_ring);
		free(rtos->rx_ring);
		return -1;
	}
	
	/* Initialize ring buffers */
	rtos->tx_ring->in = rtos->tx_ring->out = 0;
	rtos->tx_ring->len = RING_BUFFER_SIZE;
	rtos->tx_ring->esize = 1;
	
	rtos->rx_ring->in = rtos->rx_ring->out = 0;
	rtos->rx_ring->len = RING_BUFFER_SIZE;
	rtos->rx_ring->esize = 1;
	
	printf("RTOS[%s]: Ring buffers initialized (size=%d each)\n", rtos->name, RING_BUFFER_SIZE);
	return 0;
}

static void simulate_rtos_response(struct rtos_instance *rtos, const char *input_data, int input_len)
{
	char response[BUFFER_SIZE];
	int response_len;
	
	/* Simulate RTOS processing - echo with timestamp and processing info */
	struct timeval tv;
	gettimeofday(&tv, NULL);
	
	response_len = snprintf(response, sizeof(response),
				"RTOS[%s@CPU%u] processed %d bytes at %ld.%06ld: %.100s",
				rtos->name, rtos->cpu_id, input_len,
				tv.tv_sec, tv.tv_usec, input_data);
	
	/* Write response to shared memory */
	if (response_len < 256) {
		memcpy(rtos->shared_memory->rcv_buffer, response, response_len);
		rtos->shared_memory->rcv_data_len = response_len;
	}
	
	/* Also write to ring buffer for streaming data */
	ring_buffer_write(rtos->tx_ring, response, response_len);
	
	printf("RTOS[%s]: Generated response (%d bytes)\n", rtos->name, response_len);
}

static void simulate_rpmsg_processing(struct rtos_instance *rtos, rpmsg_message_t *msg)
{
	printf("RTOS[%s]: Processing RPMSG message type=%u, len=%u\n",
	       rtos->name, msg->msg_type, msg->data_len);
	
	switch (msg->msg_type) {
	case RPMSG_TYPE_RPC:
		printf("RTOS[%s]: RPC call: %.*s\n", rtos->name, msg->data_len, msg->data);
		simulate_rtos_response(rtos, msg->data, msg->data_len);
		break;
		
	case RPMSG_TYPE_UMT:
		printf("RTOS[%s]: UMT message: %.*s\n", rtos->name, msg->data_len, msg->data);
		simulate_rtos_response(rtos, msg->data, msg->data_len);
		break;
		
	case RPMSG_TYPE_PTY:
		printf("RTOS[%s]: PTY data: %.*s\n", rtos->name, msg->data_len, msg->data);
		ring_buffer_write(rtos->tx_ring, msg->data, msg->data_len);
		break;
		
	case RPMSG_TYPE_DEBUG:
		printf("RTOS[%s]: Debug data: %.*s\n", rtos->name, msg->data_len, msg->data);
		ring_buffer_write(rtos->tx_ring, msg->data, msg->data_len);
		break;
		
	default:
		printf("RTOS[%s]: Unknown message type: %u\n", rtos->name, msg->msg_type);
		break;
	}
}

static void *rtos_simulation_thread(void *arg)
{
	struct rtos_instance *rtos = (struct rtos_instance *)arg;
	struct timespec timeout;
	int ret;
	
	printf("RTOS[%s]: Simulation thread started on CPU %u\n", rtos->name, rtos->cpu_id);
	
	while (rtos->active && is_running) {
		/* Wait for user data with timeout */
		clock_gettime(CLOCK_REALTIME, &timeout);
		timeout.tv_sec += 1; /* 1 second timeout */
		
		ret = sem_timedwait(rtos->sem_user_to_micad, &timeout);
		if (ret == -1) {
			if (errno == ETIMEDOUT) {
				/* Generate periodic debug data */
				char debug_msg[128];
				int len = snprintf(debug_msg, sizeof(debug_msg),
						   "RTOS[%s] heartbeat at %ld\n", 
						   rtos->name, time(NULL));
				ring_buffer_write(rtos->tx_ring, debug_msg, len);
				continue;
			} else {
				perror("sem_timedwait failed");
				break;
			}
		}
		
		/* Process incoming data */
		pthread_mutex_lock(&rtos->data_mutex);
		
		if (rtos->shared_memory->data_len > 0) {
			/* Simulate processing delay */
			usleep(10000); /* 10ms processing delay */
			
			/* Create RPMSG message from shared memory data */
			rpmsg_message_t msg;
			msg.msg_type = RPMSG_TYPE_UMT;
			msg.src_addr = 0;
			msg.dst_addr = rtos->instance_id;
			msg.data_len = rtos->shared_memory->data_len;
			
			/* Copy data from physical address simulation */
			snprintf(msg.data, sizeof(msg.data), "Data from phy_addr=0x%lx: simulated payload",
				 rtos->shared_memory->phy_addr);
			
			/* Process the message */
			simulate_rpmsg_processing(rtos, &msg);
			
			/* Clear processed data */
			rtos->shared_memory->data_len = 0;
			rtos->shared_memory->phy_addr = 0;
		}
		
		pthread_mutex_unlock(&rtos->data_mutex);
		
		/* Signal completion to user */
		sem_post(rtos->sem_micad_to_user);
	}
	
	printf("RTOS[%s]: Simulation thread terminated\n", rtos->name);
	pthread_exit(NULL);
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
	strncpy(rtos->name, name, MAX_NAME_LEN - 1);
	rtos->cpu_id = cpu_id;
	rtos->active = true;
	pthread_mutex_init(&rtos->data_mutex, NULL);
	
	/* Initialize communication mechanisms */
	if (init_shared_memory(rtos) < 0 ||
	    init_semaphores(rtos) < 0 ||
	    init_ring_buffers(rtos) < 0) {
		destroy_rtos_instance(name);
		return -1;
	}
	
	/* Start simulation thread */
	if (pthread_create(&rtos->rtos_thread, NULL, rtos_simulation_thread, rtos) != 0) {
		perror("Failed to create RTOS simulation thread");
		destroy_rtos_instance(name);
		return -1;
	}
	
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
	char shm_name[64], sem_name[64];
	
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
	
	pthread_mutex_unlock(&rtos_mutex);
	
	if (!rtos) {
		printf("RTOS instance '%s' not found\n", name);
		return;
	}
	
	/* Stop simulation thread */
	rtos->active = false;
	sem_post(rtos->sem_user_to_micad); /* Wake up thread */
	pthread_join(rtos->rtos_thread, NULL);
	
	/* Cleanup shared memory */
	if (rtos->shared_memory) {
		munmap(rtos->shared_memory, sizeof(process_shared_data_t));
		close(rtos->shm_fd);
		snprintf(shm_name, sizeof(shm_name), SHM_NAME, rtos->instance_id);
		shm_unlink(shm_name);
	}
	
	/* Cleanup semaphores */
	if (rtos->sem_user_to_micad) {
		sem_close(rtos->sem_user_to_micad);
		snprintf(sem_name, sizeof(sem_name), SEM_USER_TO_MICAD, rtos->instance_id);
		sem_unlink(sem_name);
	}
	if (rtos->sem_micad_to_user) {
		sem_close(rtos->sem_micad_to_user);
		snprintf(sem_name, sizeof(sem_name), SEM_MICAD_TO_USER, rtos->instance_id);
		sem_unlink(sem_name);
	}
	
	/* Cleanup ring buffers */
	free(rtos->tx_ring);
	free(rtos->rx_ring);
	
	/* Cleanup mutex */
	pthread_mutex_destroy(&rtos->data_mutex);
	
	free(rtos);
	printf("RTOS instance '%s' destroyed\n", name);
}

static void cleanup_listeners(void)
{
	struct listen_unit *current, *next;
	struct client_entry *client, *client_next;
	struct rtos_instance *rtos, *rtos_next;
	
	/* Cleanup RTOS instances */
	pthread_mutex_lock(&rtos_mutex);
	rtos = rtos_instances;
	while (rtos) {
		rtos_next = rtos->next;
		rtos->active = false;
		sem_post(rtos->sem_user_to_micad); /* Wake up threads */
		rtos = rtos_next;
	}
	pthread_mutex_unlock(&rtos_mutex);
	
	/* Wait for threads and cleanup */
	pthread_mutex_lock(&rtos_mutex);
	rtos = rtos_instances;
	while (rtos) {
		rtos_next = rtos->next;
		pthread_join(rtos->rtos_thread, NULL);
		/* Note: Individual cleanup will be done by destroy_rtos_instance */
		rtos = rtos_next;
	}
	rtos_instances = NULL;
	pthread_mutex_unlock(&rtos_mutex);
	
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
		/* Remove RTOS instance */
		destroy_rtos_instance(client_name);
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
		int create_ret = create_client_socket(client_name);
		
		/* Also create RTOS instance for IO handling */
		if (create_ret == 0) {
			create_ret = create_rtos_instance(client_name, msg->cpu);
		}
		
		if (create_ret == 0) {
			register_client(client_name);
			printf("Successfully added client<%s> with RTOS simulation and client socket\n", client_name);
			if (send_response) {
				safe_send(client_fd, RESPONSE_SUCCESS, strlen(RESPONSE_SUCCESS));
			}
		} else {
			printf("Err: %d. Failed to create client socket/RTOS for: %s\n", create_ret, client_name);
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