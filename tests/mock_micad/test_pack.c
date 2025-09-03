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

#pragma pack(push, 1)  // No padding
struct create_msg_packed {
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
#pragma pack(pop)

int main() {
    printf("Normal struct size: %zu\n", sizeof(struct create_msg));
    printf("Packed struct size: %zu\n", sizeof(struct create_msg_packed));
    printf("Difference: %zu bytes\n", sizeof(struct create_msg) - sizeof(struct create_msg_packed));
    
    return 0;
}