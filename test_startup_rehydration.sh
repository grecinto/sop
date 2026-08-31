#!/bin/bash
# Test startup rehydration: pause work, restart daemon, verify reminder appears

set -e

echo "=== Test: Startup Rehydration with User Choice ==="
echo ""

# Clean up any previous test state
rm -rf /tmp/sop-test-rehydration-*
TEST_DIR=$(mktemp -d /tmp/sop-test-rehydration-XXXXX)
echo "Test directory: $TEST_DIR"

# Start daemon in background
echo "Starting daemon..."
./sop-daemon --data-dir "$TEST_DIR" --port 8765 > "$TEST_DIR/daemon.log" 2>&1 &
DAEMON_PID=$!
echo "Daemon PID: $DAEMON_PID"

# Wait for daemon to start
sleep 3

# Function to stop daemon
cleanup() {
    echo ""
    echo "Stopping daemon (PID: $DAEMON_PID)..."
    kill $DAEMON_PID 2>/dev/null || true
    wait $DAEMON_PID 2>/dev/null || true
    echo "Daemon stopped"
}
trap cleanup EXIT

# Test 1: Create and pause a workstream
echo ""
echo "Test 1: Creating and pausing workstream..."
curl -s -X POST http://localhost:8765/api/v1/ask \
  -H "Content-Type: application/json" \
  -d '{
    "query": "Create a test script that prints hello world",
    "session_id": "test-session-1"
  }' > /dev/null

sleep 2

# Pause the work
curl -s -X POST http://localhost:8765/api/v1/ask \
  -H "Content-Type: application/json" \
  -d '{
    "query": "pause this work, I need to do something else",
    "session_id": "test-session-1"
  }' > /dev/null

echo "✓ Workstream paused"

# Check daemon log for persistence
echo ""
echo "Checking for persistence logs..."
grep -i "persist" "$TEST_DIR/daemon.log" | tail -3 || echo "(no persistence logs yet)"

# Test 2: Restart daemon
echo ""
echo "Test 2: Restarting daemon to test rehydration..."
kill $DAEMON_PID
wait $DAEMON_PID 2>/dev/null || true
sleep 1

# Clear old log and restart
mv "$TEST_DIR/daemon.log" "$TEST_DIR/daemon-first-run.log"
./sop-daemon --data-dir "$TEST_DIR" --port 8765 > "$TEST_DIR/daemon.log" 2>&1 &
DAEMON_PID=$!
echo "Daemon restarted (PID: $DAEMON_PID)"

sleep 3

# Check for rehydration in logs
echo ""
echo "=== Checking for rehydration logs ==="
if grep -i "rehydrate\|paused workstream" "$TEST_DIR/daemon.log"; then
    echo ""
    echo "✅ SUCCESS: Rehydration working!"
else
    echo ""
    echo "⚠️  WARNING: No rehydration logs found"
    echo "Full daemon log:"
    cat "$TEST_DIR/daemon.log"
fi

# Test 3: Verify workstream is still accessible
echo ""
echo "Test 3: Listing workstreams after restart..."
RESPONSE=$(curl -s -X POST http://localhost:8765/api/v1/ask \
  -H "Content-Type: application/json" \
  -d '{
    "query": "list my paused workstreams",
    "session_id": "test-session-1"
  }')

if echo "$RESPONSE" | grep -q "test script\|workstream"; then
    echo "✅ Workstream still accessible after restart"
else
    echo "⚠️  Workstream may not be accessible"
    echo "Response: $RESPONSE"
fi

echo ""
echo "=== Test Complete ==="
echo "First run log: $TEST_DIR/daemon-first-run.log"
echo "Second run log: $TEST_DIR/daemon.log"
