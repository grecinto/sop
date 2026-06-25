#!/bin/bash
#
# Test script for daemon bootstrap authentication flow
# 
# This script verifies the complete authentication flow:
# 1. Daemon generates bootstrap secret on startup
# 2. UI retrieves bootstrap secret via httpserver
# 3. UI authenticates with daemon using bootstrap secret
# 4. UI makes authenticated requests to daemon
#

set -e

echo "🧪 Testing Daemon Bootstrap Authentication Flow"
echo "================================================"
echo ""

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

DAEMON_PORT=9090
HTTPSERVER_PORT=8080
BOOTSTRAP_FILE="$HOME/.config/sop/daemon.bootstrap"

# Cleanup function
cleanup() {
    echo ""
    echo "🧹 Cleaning up..."
    if [ ! -z "$DAEMON_PID" ]; then
        kill $DAEMON_PID 2>/dev/null || true
    fi
    if [ ! -z "$HTTPSERVER_PID" ]; then
        kill $HTTPSERVER_PID 2>/dev/null || true
    fi
    rm -f "$BOOTSTRAP_FILE"
}

trap cleanup EXIT

echo "📋 Step 1: Starting daemon..."
cd /Volumes/T7/sop-cloud/lib/sop
./cmd/sop-daemon/sop-daemon --port $DAEMON_PORT > /tmp/daemon-test.log 2>&1 &
DAEMON_PID=$!
echo "   Daemon PID: $DAEMON_PID"

# Wait for daemon to start and generate bootstrap
sleep 2

if [ ! -f "$BOOTSTRAP_FILE" ]; then
    echo -e "${RED}❌ FAIL: Bootstrap file not created${NC}"
    exit 1
fi

echo -e "${GREEN}✅ Bootstrap file created${NC}"
BOOTSTRAP_SECRET=$(cat "$BOOTSTRAP_FILE")
echo "   Bootstrap secret: ${BOOTSTRAP_SECRET:0:16}... (truncated)"
echo ""

echo "📋 Step 2: Starting httpserver..."
./tools/httpserver/httpserver --port $HTTPSERVER_PORT --config /tmp/test-config.json > /tmp/httpserver-test.log 2>&1 &
HTTPSERVER_PID=$!
echo "   Httpserver PID: $HTTPSERVER_PID"
sleep 2
echo ""

echo "📋 Step 3: Retrieving bootstrap secret via httpserver..."
SECRET_RESPONSE=$(curl -s http://localhost:$HTTPSERVER_PORT/api/daemon/bootstrap-secret)
echo "   Response: $SECRET_RESPONSE"

RETRIEVED_SECRET=$(echo $SECRET_RESPONSE | grep -o '"bootstrap_secret":"[^"]*"' | cut -d'"' -f4)
if [ "$RETRIEVED_SECRET" != "$BOOTSTRAP_SECRET" ]; then
    echo -e "${RED}❌ FAIL: Retrieved secret doesn't match${NC}"
    exit 1
fi

echo -e "${GREEN}✅ Bootstrap secret retrieved successfully${NC}"
echo ""

echo "📋 Step 4: Authenticating with daemon using bootstrap secret..."
AUTH_RESPONSE=$(curl -s -X POST http://localhost:$DAEMON_PORT/api/daemon/auth \
    -H "Content-Type: application/json" \
    -d "{\"bootstrap_secret\":\"$BOOTSTRAP_SECRET\"}")

echo "   Auth response: ${AUTH_RESPONSE:0:100}... (truncated)"

ACCESS_TOKEN=$(echo $AUTH_RESPONSE | grep -o '"access_token":"[^"]*"' | cut -d'"' -f4)
if [ -z "$ACCESS_TOKEN" ]; then
    echo -e "${RED}❌ FAIL: No access token received${NC}"
    echo "   Full response: $AUTH_RESPONSE"
    exit 1
fi

echo -e "${GREEN}✅ Access token received${NC}"
echo "   Token: ${ACCESS_TOKEN:0:30}... (truncated)"
echo ""

echo "📋 Step 5: Verifying bootstrap file deletion..."
if [ -f "$BOOTSTRAP_FILE" ]; then
    echo -e "${RED}❌ FAIL: Bootstrap file still exists (should be deleted after use)${NC}"
    exit 1
fi

echo -e "${GREEN}✅ Bootstrap file deleted (one-time use)${NC}"
echo ""

echo "📋 Step 6: Making authenticated request to daemon..."
ASK_RESPONSE=$(curl -s -X POST http://localhost:$DAEMON_PORT/api/ask \
    -H "Authorization: Bearer $ACCESS_TOKEN" \
    -H "Content-Type: application/json" \
    -d '{"query":"Hello"}')

if echo "$ASK_RESPONSE" | grep -q "error"; then
    echo -e "${RED}❌ FAIL: Authenticated request failed${NC}"
    echo "   Response: $ASK_RESPONSE"
    exit 1
fi

echo -e "${GREEN}✅ Authenticated request succeeded${NC}"
echo ""

echo "📋 Step 7: Testing token refresh..."
REFRESH_TOKEN=$(echo $AUTH_RESPONSE | grep -o '"refresh_token":"[^"]*"' | cut -d'"' -f4)
REFRESH_RESPONSE=$(curl -s -X POST http://localhost:$DAEMON_PORT/api/daemon/refresh \
    -H "Content-Type: application/json" \
    -d "{\"refresh_token\":\"$REFRESH_TOKEN\"}")

NEW_ACCESS_TOKEN=$(echo $REFRESH_RESPONSE | grep -o '"access_token":"[^"]*"' | cut -d'"' -f4)
if [ -z "$NEW_ACCESS_TOKEN" ]; then
    echo -e "${RED}❌ FAIL: Token refresh failed${NC}"
    echo "   Response: $REFRESH_RESPONSE"
    exit 1
fi

echo -e "${GREEN}✅ Token refresh succeeded${NC}"
echo "   New token: ${NEW_ACCESS_TOKEN:0:30}... (truncated)"
echo ""

echo "================================================"
echo -e "${GREEN}🎉 All tests passed!${NC}"
echo ""
echo "Summary:"
echo "  ✅ Daemon generates bootstrap secret on startup"
echo "  ✅ Httpserver proxies bootstrap secret to browser"
echo "  ✅ Bootstrap authentication issues JWT tokens"
echo "  ✅ Bootstrap file deleted after one-time use"
echo "  ✅ Authenticated requests work with JWT"
echo "  ✅ Token refresh works correctly"
echo ""
