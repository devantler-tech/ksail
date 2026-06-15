SHELL := /bin/bash

DESKTOP_DIR := desktop
VERSION ?= $(shell git describe --tags --always 2>/dev/null | sed 's/^v//' || echo dev)

.PHONY: help ui build test desktop desktop-app

help: ## Show available targets.
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

ui: ## Build the web UI and stage it for embedding into the Go binary (pkg/webui/dist).
	bash scripts/stage-webui.sh

build: ui ## Build the ksail binary with the web UI embedded.
	go build -o ksail .

desktop: ui ## Build the KSail desktop app (CGO + a system webview required); output: ./ksail-desktop.
	cd $(DESKTOP_DIR) && go build -o ../ksail-desktop .

desktop-app: ui ## Build the macOS KSail.app bundle (macOS only); output: ./KSail.app.
	cd $(DESKTOP_DIR) && go build -ldflags "-s -w" -o ksail-desktop .
	bash $(DESKTOP_DIR)/scripts/make-macos-app.sh "$(DESKTOP_DIR)/ksail-desktop" "KSail.app" "$(VERSION)"

test: ## Run the Go unit tests.
	go test ./...
