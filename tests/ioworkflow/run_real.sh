#!/usr/bin/env bash
set -euo pipefail

# Quick-run script: Real RTOS + host-driver (no containerd)
# - Binds host-driver to an existing RTOS PTY (default /dev/ttyRPMSG0)
# - Creates explicit FIFO paths under tests/ioworkflow/out
# - Leaves the host-driver running; interact via the FIFOs

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN="$DIR/bin"
OUT="$DIR/out"
HOST="$BIN/host-driver"
PTY_PATH="${1:-/dev/ttyRPMSG0}"

mkdir -p "$OUT"

if [[ ! -x "$HOST" ]]; then
  echo "Building host-driver..."
  (cd "$DIR" && GOOS=linux GOARCH=arm64 go build -o "$BIN/host-driver" ./cmd/host-driver)
fi

if [[ ! -e "$PTY_PATH" ]]; then
  echo "ERROR: PTY path not found: $PTY_PATH"
  echo "Provide the correct path, e.g.: $0 /dev/ttyRPMSG0"
  exit 1
fi

STDIN_FIFO="$OUT/stdin.fifo"
STDOUT_FIFO="$OUT/stdout.fifo"

echo "Starting host-driver (terminal mode) with explicit FIFOs:"
echo "  PTY:          $PTY_PATH"
echo "  STDIN FIFO:   $STDIN_FIFO"
echo "  STDOUT FIFO:  $STDOUT_FIFO"
nohup "$HOST" --pty="$PTY_PATH" --terminal=true --stdin="$STDIN_FIFO" --stdout="$STDOUT_FIFO" >"$OUT/host_real.log" 2>&1 & echo $! >"$OUT/host_real.pid"

echo "Host-driver started. PID: $(cat "$OUT/host_real.pid")"
echo ""
echo "Usage:"
echo "  - Write input to RTOS via stdin FIFO:"
echo "      printf 'PING\\n' > '$STDIN_FIFO'"
echo "  - Read RTOS output via stdout FIFO:"
echo "      cat '$STDOUT_FIFO'"
echo "  - Synthetic resize (terminal-mode test, via FIFO write use uppercase without colon):"
echo "      printf 'RESIZE 40 120\\n' > '$STDIN_FIFO'"
echo ""
echo "Logs:"
echo "  $OUT/host_real.log"
echo ""
echo "To stop:"
echo "  kill \$(cat '$OUT/host_real.pid') || true"
