.PHONY: test test-critical test-setup-wizard

# Run all tests
test:
	go test -v ./...

# Run CRITICAL tests that must pass before any commit
# These protect first-run setup wizard and core authentication flows
test-critical:
	@echo "Running CRITICAL tests - these MUST pass before committing..."
	@go test -v -run TestSetupWizard ./...
	@echo ""
	@echo "✅ CRITICAL tests passed"

# Alias for critical tests focused on setup wizard
test-setup-wizard: test-critical
