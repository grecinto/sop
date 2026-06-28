.PHONY: install-sop-admin build-sop-admin

GOBIN ?= $(shell go env GOPATH)/bin

build-sop-admin:
	@mkdir -p "$(GOBIN)"
	@go build -o "$(GOBIN)/sop-admin" ./tools/sopadmin

install-sop-admin: build-sop-admin
	@echo "Installed $(GOBIN)/sop-admin"
