#include <unistd.h>
#include <sys/syscall.h>

void _start(void)
{
    // This is the Hello Zephyr message that should be captured by mica-shim IO system
    const char msg[] = "Hello Zephyr!\n";
    
    // Additional debug message to help track output
    const char debug_msg[] = "[DEBUG] Hello Zephyr program starting...\n";
    syscall(SYS_write, 1, debug_msg, sizeof(debug_msg) - 1);
    
    // The main Hello Zephyr message
    syscall(SYS_write, 1, msg, sizeof(msg) - 1);
    
    // Additional debug message to confirm completion
    const char complete_msg[] = "[DEBUG] Hello Zephyr program completed!\n";
    syscall(SYS_write, 1, complete_msg, sizeof(complete_msg) - 1);
    
    syscall(SYS_exit, 0);
}
