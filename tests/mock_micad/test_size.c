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
    bool debug;                       // 1 (but typically 4 bytes due to alignment)
    char cpu_str[MAX_CPU_STRING_LEN]; // 128
    int vcpu_num;                     // 4
    int cpu_weight;                   // 4
    int cpu_capacity;                 // 4
    int memory;                       // 4
    char network[MAX_NETWORK_LEN];    // 512
};

int main() {
    printf("Size of struct create_msg: %zu\n", sizeof(struct create_msg));
    printf("Calculated size: %d\n", 
           32 + 128 + 32 + 128 + 1 + 128 + 4 + 4 + 4 + 4 + 512);
    printf("Size of bool: %zu\n", sizeof(bool));
    return 0;
}