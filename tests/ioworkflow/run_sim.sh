#!/usr/bin/env bash
set -euo pipefail

# Quick-run script: Simulated RTOS + host-driver (no containerd)
# - Starts sim_rtos which exposes a PTY and symlink at /tmp/ttyRPMSG2
# - Starts host-driver bound to that PTY with explicit FIFO paths
# - Leaves both processes running interactively

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN="$DIR/bin"
OUT="$DIR/out"
SIM="$BIN/sim_rtos"
HOST="$BIN/host-driver"
PTY_LINK="/tmp/ttyRPMSG2"

mkdir -p "$OUT"

if [[ ! -x "$SIM" || ! -x "$HOST" ]]; then
  echo "Building binaries..."
  mkdir -p "$BIN"
  (cd "$DIR" && go build -o "$BIN/host-driver" ./cmd/host-driver)
  (cd "$DIR" && go build -o "$BIN/sim_rtos" ./sim_rtos)
fi

echo "Launching sim_rtos..."
nohup "$SIM" >"$OUT/sim.log" 2>&1 & echo $! >"$OUT/sim.pid"
sleep 0.5

# Wait for symlink to appear (best-effort)
ATTEMPTS=30
while [[ ! -e "$PTY_LINK" && $ATTEMPTS -gt 0 ]]; do
  sleep 0.2
  ATTEMPTS=$((ATTEMPTS - 1))
done

if [[ ! -e "$PTY_LINK" ]]; then
  echo "WARNING: $PTY_LINK not found yet; sim_rtos may still be initializing."
fi

STDIN_FIFO="$OUT/stdin.fifo"
STDOUT_FIFO="$OUT/stdout.fifo"
# In terminal mode, stderr is combined with stdout; no separate stderr FIFO needed.

echo "Starting host-driver (terminal mode) with explicit FIFOs:"
echo "  PTY:          $PTY_LINK"
echo "  STDIN FIFO:   $STDIN_FIFO"
echo "  STDOUT FIFO:  $STDOUT_FIFO"
nohup "$HOST" --pty="$PTY_LINK" --terminal=true --stdin="$STDIN_FIFO" --stdout="$STDOUT_FIFO" >"$OUT/host.log" 2>&1 & echo $! >"$OUT/host.pid"

echo "Processes started:"
echo "  sim_rtos PID:  $(cat "$OUT/sim.pid")"
echo "  host-driver PID: $(cat "$OUT/host.pid")"
echo ""
echo "Usage:"
echo "  - Write input to the RTOS by writing into $STDIN_FIFO, e.g.:"
echo "      printf 'PING\\n' > '$STDIN_FIFO'"
echo "  - Read output from RTOS by reading from $STDOUT_FIFO, e.g.:"
echo "      cat '$STDOUT_FIFO'"
echo "  - To send a synthetic resize (via FIFO write use uppercase without colon):"
echo "      printf 'RESIZE 30 100\\n' > '$STDIN_FIFO'"
echo ""
echo "Logs:"
echo "  $OUT/sim.log  (simulated RTOS console)"
echo "  $OUT/host.log (host-driver logs)"
echo ""
echo "To stop:"
echo "  kill \$(cat '$OUT/host.pid') || true"
echo "  kill \$(cat '$OUT/sim.pid') || true"
