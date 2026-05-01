#!/bin/bash
set -e

PROJECT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$PROJECT_DIR"

echo "=== Checking for residual processes ==="
for bin in master worker; do
    pids=$(pgrep -f "\./bin/$bin" || true)
    if [ -n "$pids" ]; then
        echo "Killing residual $bin: $pids"
        kill $pids 2>/dev/null || true
        sleep 1
    fi
done
echo "Cleaned up"

echo "=== Building ==="
go build -o bin/master ./cmd/master
go build -o bin/worker ./cmd/worker
echo "Build complete"

echo "=== Starting Master ==="
MASTER_OPTS="-http=:9090 -tcp=:9091"
if [ -n "$MODEL_API_KEY" ]; then
    MASTER_OPTS="$MASTER_OPTS -model-key=$MODEL_API_KEY"
fi
if [ -n "$MODEL_BASE_URL" ]; then
    MASTER_OPTS="$MASTER_OPTS -model-url=$MODEL_BASE_URL"
fi
if [ -n "$MODEL_NAME" ]; then
    MASTER_OPTS="$MASTER_OPTS -model=$MODEL_NAME"
fi
nohup ./bin/master $MASTER_OPTS > /tmp/eino-ptes-master.log 2>&1 &
MASTER_PID=$!
echo "Master PID: $MASTER_PID"

for i in {1..10}; do
    if curl -s http://localhost:9090/api/workers >/dev/null 2>&1; then
        echo "Master ready"
        break
    fi
    sleep 0.5
done

echo "=== Starting Worker ==="
# wait for TCP port to be ready before starting worker
for i in {1..20}; do
    if timeout 1 bash -c 'cat < /dev/null > /dev/tcp/localhost/9091' 2>/dev/null; then
        break
    fi
    sleep 0.3
done
nohup ./bin/worker -id=kali-worker-1 -name="Kali-VM-1" -master=localhost:9091 -caps=nmap,nikto,dirb > /tmp/eino-ptes-worker.log 2>&1 &
WORKER_PID=$!
echo "Worker PID: $WORKER_PID"

sleep 1
echo "=== Status ==="
echo "Workers: $(curl -s http://localhost:9090/api/workers | wc -c) bytes"
echo "Master log: tail -f /tmp/eino-ptes-master.log"
echo "Worker log: tail -f /tmp/eino-ptes-worker.log"
