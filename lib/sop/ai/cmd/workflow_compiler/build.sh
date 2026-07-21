#!/bin/bash
# Quick build and run script for workflow compiler

set -e

cd "$(dirname "$0")"

echo "Building sop-compile-workflow..."
go build -o sop-compile-workflow main.go

echo "✓ Build complete: ./sop-compile-workflow"
echo ""
echo "Usage examples:"
echo "  ./sop-compile-workflow -input workflow.json -output my-workflow"
echo "  ./sop-compile-workflow -script -input script.json -output-go script.go"
echo "  ./sop-compile-workflow -version"
