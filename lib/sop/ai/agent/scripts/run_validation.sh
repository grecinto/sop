#!/bin/bash

# Automated Validation Runner for Multi-Turn Orchestration
# Usage: ./scripts/run_validation.sh [options]

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Default options
TIMEOUT="15m"
VERBOSE=false
REPORT_FILE=""
SCENARIOS=""
PERFORMANCE=false

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -t|--timeout)
            TIMEOUT="$2"
            shift 2
            ;;
        -v|--verbose)
            VERBOSE=true
            shift
            ;;
        -o|--output)
            REPORT_FILE="$2"
            shift 2
            ;;
        -s|--scenario)
            SCENARIOS="$2"
            shift 2
            ;;
        -p|--performance)
            PERFORMANCE=true
            shift
            ;;
        -h|--help)
            cat << EOF
Automated Validation Runner

Usage: $0 [options]

Options:
    -t, --timeout TIME       Set timeout (default: 15m)
    -v, --verbose           Enable verbose output
    -o, --output FILE       Save report to file
    -s, --scenario NAME     Run specific scenario
    -p, --performance       Run performance benchmarks only
    -h, --help             Show this help message

Examples:
    # Run all validation scenarios
    $0

    # Run with verbose output and 30 minute timeout
    $0 -v -t 30m

    # Run specific scenario
    $0 -s BugFixWorkflow

    # Run performance benchmarks
    $0 -p

    # Save report to file
    $0 -o validation_report.txt
EOF
            exit 0
            ;;
        *)
            echo -e "${RED}Unknown option: $1${NC}"
            exit 1
            ;;
    esac
done

# Change to agent directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
AGENT_DIR="$(dirname "$SCRIPT_DIR")"
cd "$AGENT_DIR"

echo -e "${BLUE}╔════════════════════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║     Multi-Turn Orchestration - Automated Validation Suite      ║${NC}"
echo -e "${BLUE}╚════════════════════════════════════════════════════════════════╝${NC}"
echo ""

# Show configuration
echo -e "${YELLOW}Configuration:${NC}"
echo "  Directory: $AGENT_DIR"
echo "  Timeout: $TIMEOUT"
echo "  Verbose: $VERBOSE"
if [ -n "$SCENARIOS" ]; then
    echo "  Scenario: $SCENARIOS"
fi
if [ -n "$REPORT_FILE" ]; then
    echo "  Report: $REPORT_FILE"
fi
echo ""

# Build test command
TEST_CMD="go test -tags=validation -timeout=$TIMEOUT"

if [ "$VERBOSE" = true ]; then
    TEST_CMD="$TEST_CMD -v"
fi

if [ "$PERFORMANCE" = true ]; then
    TEST_CMD="$TEST_CMD -run=TestValidation_Performance"
elif [ -n "$SCENARIOS" ]; then
    TEST_CMD="$TEST_CMD -run=TestValidation_RealWorldScenarios/$SCENARIOS"
fi

# Run tests
echo -e "${YELLOW}Running validation tests...${NC}"
echo -e "${BLUE}Command: $TEST_CMD${NC}"
echo ""

START_TIME=$(date +%s)

if [ -n "$REPORT_FILE" ]; then
    # Run and save to file
    if $TEST_CMD 2>&1 | tee "$REPORT_FILE"; then
        TEST_RESULT=0
    else
        TEST_RESULT=$?
    fi
else
    # Run normally
    if $TEST_CMD; then
        TEST_RESULT=0
    else
        TEST_RESULT=$?
    fi
fi

END_TIME=$(date +%s)
DURATION=$((END_TIME - START_TIME))

echo ""
echo -e "${BLUE}════════════════════════════════════════════════════════════════${NC}"

# Show results
if [ $TEST_RESULT -eq 0 ]; then
    echo -e "${GREEN}✓ VALIDATION PASSED${NC}"
    echo -e "${GREEN}All scenarios completed successfully in ${DURATION}s${NC}"
    
    if [ -n "$REPORT_FILE" ]; then
        echo -e "${YELLOW}Report saved to: $REPORT_FILE${NC}"
    fi
    
    exit 0
else
    echo -e "${RED}✗ VALIDATION FAILED${NC}"
    echo -e "${RED}Some scenarios failed (exit code: $TEST_RESULT)${NC}"
    
    if [ -n "$REPORT_FILE" ]; then
        echo -e "${YELLOW}Report saved to: $REPORT_FILE${NC}"
        echo -e "${YELLOW}Review the report for details${NC}"
    fi
    
    exit $TEST_RESULT
fi
