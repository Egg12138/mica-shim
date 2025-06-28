#include <unistd.h>
#include <sys/syscall.h>

void _start(void)
{
    const char msg[] = "Hello Zephyr!\n";
    syscall(SYS_write, 1, msg, sizeof(msg) - 1);
    syscall(SYS_exit, 0);
}
