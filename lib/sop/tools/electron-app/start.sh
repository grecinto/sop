#!/bin/bash

# SOP AI Desktop - Setup and Launch Script
# This script checks prerequisites and launches the Electron app

set -e

echo "🚀 SOP AI Desktop - Setup & Launch"
echo "===================================="
echo ""

# Color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
HTTPSERVER_DIR="$SCRIPT_DIR/../httpserver"
HTTPSERVER_BIN="$HTTPSERVER_DIR/httpserver"

# Check Node.js
echo "Checking prerequisites..."
echo ""

if command -v node &> /dev/null; then
    NODE_VERSION=$(node --version)
    echo -e "${GREEN}✅ Node.js installed:${NC} $NODE_VERSION"
else
    echo -e "${RED}❌ Node.js not installed${NC}"
    echo ""
    echo "Please install Node.js first:"
    echo "  Option 1 (Homebrew): brew install node"
    echo "  Option 2 (Official):  https://nodejs.org/"
    echo ""
    exit 1
fi

if command -v npm &> /dev/null; then
    NPM_VERSION=$(npm --version)
    echo -e "${GREEN}✅ npm installed:${NC} $NPM_VERSION"
else
    echo -e "${RED}❌ npm not installed${NC}"
    exit 1
fi

# Check httpserver binary
if [ -f "$HTTPSERVER_BIN" ]; then
    BINARY_SIZE=$(du -h "$HTTPSERVER_BIN" | cut -f1)
    echo -e "${GREEN}✅ httpserver binary:${NC} $BINARY_SIZE"
else
    echo -e "${YELLOW}⚠️  httpserver binary not found${NC}"
    echo "Building httpserver..."
    cd "$HTTPSERVER_DIR"
    go build
    if [ -f "$HTTPSERVER_BIN" ]; then
        echo -e "${GREEN}✅ httpserver built successfully${NC}"
    else
        echo -e "${RED}❌ Failed to build httpserver${NC}"
        exit 1
    fi
    cd "$SCRIPT_DIR"
fi

# Check if node_modules exists
if [ ! -d "$SCRIPT_DIR/node_modules" ]; then
    echo -e "${YELLOW}⚠️  Dependencies not installed${NC}"
    echo ""
    echo "Installing Electron dependencies..."
    npm install
    echo -e "${GREEN}✅ Dependencies installed${NC}"
else
    echo -e "${GREEN}✅ Dependencies installed${NC}"
fi

echo ""
echo "===================================="
echo -e "${GREEN}All prerequisites met!${NC}"
echo "===================================="
echo ""
echo "Starting SOP AI Desktop..."
echo ""
echo "This will launch:"
echo "  • httpserver backend (port 8080)"
echo "  • Main window (Full SOP AI UI)"
echo "  • Floating chat window (Always on top)"
echo "  • System tray icon"
echo ""
echo "Press Ctrl+C to stop"
echo ""

# Launch Electron app
npm start
