#include <stdio.h>
#include <stddef.h>
#include <stdbool.h>

#define MAX_NAME_LEN 32
#define MAX_FIRMWARE_PATH_LEN 128
#define MAX_CPU_STRING_LEN 128
#define MAX_NETWORK_LEN 512

struct create_msg {
    char name[MAX_NAME_LEN];          // 32
    char path[MAX_FIRMWARE_PATH_LEN]; // 128
    char ped[MAX_NAME_LEN];           // 32
    char ped_cfg[MAX_FIRMWARE_PATH_LEN]; // 128
    bool debug;                       // 1
    char cpu_str[MAX_CPU_STRING_LEN]; // 128
    int vcpu_num;                     // 4
    int cpu_weight;                   // 4
    int cpu_capacity;                 // 4
    int memory;                       // 4
    char network[MAX_NETWORK_LEN];    // 512
};

int main() {
    printf("Field sizes and offsets:\n");
    printf("name: %zu bytes, offset: %zu\n", sizeof(((struct create_msg*)0)->name), offsetof(struct create_msg, name));
    printf("path: %zu bytes, offset: %zu\n", sizeof(((struct create_msg*)0)->path), offsetof(struct create_msg, path));
    printf("ped: %zu bytes, offset: %zu\n", sizeof(((struct create_msg*)0)->ped), offsetof(struct create_msg, ped));
    printf("ped_cfg: %zu bytes, offset: %zu\n", sizeof(((struct create_msg*)0)->ped_cfg), offsetof(struct create_msg, ped_cfg));
    printf("debug: %zu bytes, offset: %zu\n", sizeof(((struct create_msg*)0)->debug), offsetof(struct create_msg, debug));
    printf("cpu_str: %zu bytes, offset: %zu\n", sizeof(((struct create_msg*)0)->cpu_str), offsetof(struct create_msg, cpu_str));
    printf("vcpu_num: %zu bytes, offset: %zu\n", sizeof(((struct create_msg*)0)->vcpu_num), offsetof(struct create_msg, vcpu_num));
    printf("cpu_weight: %zu bytes, offset: %zu\n", sizeof(((struct create_msg*)0)->cpu_weight), offsetof(struct create_msg, cpu_weight));
    printf("cpu_capacity: %zu bytes, offset: %zu\n", sizeof(((struct create_msg*)0)->cpu_capacity), offsetof(struct create_msg, cpu_capacity));
    printf("memory: %zu bytes, offset: %zu\n", sizeof(((struct create_msg*)0)->memory), offsetof(struct create_msg, memory));
    printf("network: %zu bytes, offset: %zu\n", sizeof(((struct create_msg*)0)->network), offsetof(struct create_msg, network));
    
    printf("\nTotal size: %zu\n", sizeof(struct create_msg));
    printf("Calculated without padding: %d\n", 32+128+32+128+1+128+4+4+4+4+512);
    
    // Check padding after debug
    printf("Padding after debug: %zu bytes\n", offsetof(struct create_msg, cpu_str) - (offsetof(struct create_msg, debug) + sizeof(bool)));
    
    return 0;
}