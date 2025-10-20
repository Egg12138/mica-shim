# Micran IO Workflow Validation (tests/ioworkflow)

Purpose:
- Validate micran’s IO can receive RTOS console output and forward it to “containerd-like” stdio (FIFOs).
- Validate terminal-mode input/controls (keystrokes, simple commands, synthetic resize) without running containerd or actual containers.
- Provide both a real-RTOS path (/dev/ttyRPMSG0) and a simulated RTOS bound to /tmp/ttyRPMSG2.

Contents:
- cmd/host-driver: A host-side driver that:
  - Creates stdio FIFOs (stdin/stdout[/stderr]).
  - Bridges FIFOs <-> libmica.MicaIO <-> PTY device (/dev/ttyRPMSG* or /dev/pts/N).
  - Provides a simple REPL to send input and control commands.
- sim_rtos: A userspace pseudo-RTOS that exposes a PTY and echoes/handles simple commands. It creates a symlink /tmp/ttyRPMSG2 -> /dev/pts/N for deterministic testing.
- sim_linux_tty: A Linux bash shell simulator that spawns a real bash process in a PTY. It creates a symlink /tmp/ttyRPMSG3 -> /dev/pts/N for testing shell interaction capabilities.

Prerequisites:
- Go toolchain, GOPROXY set for China network:
  - go env -w GOPROXY=https://goproxy.cn,direct
  - go env -w GOSUMDB=off
- Built binaries:
  - Using Makefile (recommended):
    ```bash
    cd tests/ioworkflow
    make setup    # Setup Go environment
    make native   # Build for x64 host testing
    make arm64    # Cross-compile for ARM64 MCU testing
    make both     # Build both architectures
    ```
  - Manual build:
    ```bash
    mkdir -p tests/ioworkflow/bin
    go build -o tests/ioworkflow/bin/host-driver ./tests/ioworkflow/cmd/host-driver
    go build -o tests/ioworkflow/bin/sim_rtos ./tests/ioworkflow/sim_rtos
    go build -o tests/ioworkflow/bin/sim_linux_tty ./tests/ioworkflow/sim_linux_tty
    ```

Quick start (no containerd involved):

1) Simulated RTOS (recommended for quick test)
- Launch sim RTOS:
  - ./tests/ioworkflow/bin/sim_rtos
  - It prints:
    - SLAVE_PATH=/dev/pts/N
    - LINK_PATH=/tmp/ttyRPMSG2
    - HINT: export MICRAN_PTY_DEVICE=/tmp/ttyRPMSG2 (or SLAVE_PATH)
- In another terminal, run host-driver bound to the simulated PTY:
  - ./tests/ioworkflow/bin/host-driver --pty=/tmp/ttyRPMSG2 --terminal=true
- You should see:
  - “RTOS SIM: terminal ready …”
- Type commands:
  - PING            -> expect “PONG <timestamp>”
  - BIG 4           -> emits ~4KB of data
  - :resize 30 100  -> host-driver sends “RESIZE 30 100”, sim RTOS replies “RESIZED 30 100”
  - :hex 00ff0a     -> send raw bytes followed by newline
  - :big 64         -> send 64KB from host to RTOS (input pressure test)
  - :quit           -> exit host-driver

Notes:
- host-driver creates stdin/stdout FIFOs under $TMPDIR/micran-iowf-<pid>. You can also pass explicit FIFO paths via flags.
- In terminal mode (default), stderr is combined with stdout (matches typical TTY semantics). Non-terminal mode supports a separate stderr FIFO, but micran’s current MicaIO maps everything on PTY to stdout.

2) Linux bash shell simulation (for testing shell interaction)
- Launch sim_linux_tty with bash shell:
  - ./tests/ioworkflow/bin/sim_linux_tty
  - It prints:
    - SLAVE_PATH=/dev/pts/N
    - LINK_PATH=/tmp/ttyRPMSG3
    - SHELL=/bin/bash --norc --noprofile
    - HINT: export MICRAN_PTY_DEVICE=/tmp/ttyRPMSG3 (or SLAVE_PATH)
- In another terminal, run host-driver bound to the PTY:
  - ./tests/ioworkflow/bin/host-driver --pty=/tmp/ttyRPMSG3 --terminal=true
- You should see a bash shell prompt (e.g., user@host:~$)
- Type bash commands:
  - ls -la            -> list directory contents
  - echo "Hello"       -> print Hello
  - pwd               -> print working directory
  - export VAR=value  -> set environment variable
  - cat /etc/os-release -> show OS info
  - :quit             -> exit host-driver
- For automated testing:
  - make test-linux   - builds and runs the Linux TTY test

3) Real RTOS on MCU
- Ensure your RTOS client is loaded and micad presents a console PTY (commonly /dev/ttyRPMSG0).
- Run host-driver against real PTY:
  - ./tests/ioworkflow/bin/host-driver --pty=/dev/ttyRPMSG0 --terminal=true
- Type commands or input; observe echoed output from RTOS.
- Optional firmware loading if needed:
  - Use mcs/tools/mica and your board-specific remoteproc config (e.g., scripts/qemu-zephyr-rproc.conf).
  - After client OS boots, confirm /dev/ttyRPMSG0 exists, then run host-driver as above.

Design choices and micran IO restoration:
- The host-driver uses libmica.MicaIO, which bridges:
  - PTY device <-> stdout FIFO
  - stdin FIFO <-> PTY device
  - stderr (non-terminal) currently not separated – PTY typically combines both.
- We added an environment override (MICRAN_PTY_DEVICE) to select the exact PTY (e.g., /dev/ttyRPMSG0 or /tmp/ttyRPMSG2) deterministically, avoiding “first device” scanning behavior.

Containerd IO simulation notes:
- This workflow intentionally avoids running containerd.
- The host-driver simulates containerd’s stdio FIFO behavior and exercises micran’s IO code paths directly.
- For future end-to-end containerd testing:
  - tests/containerd_client can be extended to create a Task with Terminal=true, set FIFO paths, and invoke ResizePty via the task API when the shim wiring is ready.

Troubleshooting:
- PTY not found:
  - Ensure MICRAN_PTY_DEVICE is set or pass --pty= explicitly to host-driver.
  - For sim RTOS, verify /tmp/ttyRPMSG2 symlink exists and points to /dev/pts/N.
- Large payload stalls:
  - This can indicate backpressure; current MicaIO uses short deadlines and retry loops. Tune deadlines or chunk sizes if needed.
- No stderr separation:
  - Expected in terminal mode. Non-terminal separation requires enhancements in mica/micad to expose distinct streams.
