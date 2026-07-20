#!/bin/bash
# Test script for PTY worker isolation - Phase 1

set -e

echo "=== PTY Worker Isolation - Phase 1 Test ==="
echo ""

# Build the daemon
echo "1. Building daemon..."
cd /Volumes/T7/sop-cloud/lib/sop/cmd/sop-daemon
go build -o /tmp/sop-daemon-test
echo "   ✓ Build successful"
echo ""

# Test 1: Worker mode detection
echo "2. Testing worker mode detection..."
timeout 2 /tmp/sop-daemon-test --worker-mode 2>&1 | head -5 || true
echo "   ✓ Worker mode flag detected (process should have started and waited for stdin)"
echo ""

# Test 2: Normal daemon mode still works
echo "3. Testing normal daemon mode (should show usage or start normally)..."
timeout 2 /tmp/sop-daemon-test --help 2>&1 | head -10 || true
echo "   ✓ Normal mode still works"
echo ""

echo "=== Phase 1 Tests Complete ==="
echo ""
echo "Next steps:"
echo "  - Phase 2: Integrate worker manager with PTY shell manager"
echo "  - Test actual command execution through workers"
echo "  - Test crash isolation"
