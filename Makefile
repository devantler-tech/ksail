SHELL := /bin/bash

DESKTOP_DIR := desktop
VERSION ?= $(shell git describe --tags --always 2>/dev/null | sed 's/^v//' || echo dev)

# CodeQL's Go autobuilder runs plain `make` here before it extracts the Go code. Inside that
# job the default goal prepares the runner instead of listing the targets (#7131).
ifneq ($(and $(GITHUB_ACTIONS),$(CODEQL_EXTRACTOR_GO_ROOT)),)
.DEFAULT_GOAL := codeql-autobuild-prepare
endif

.PHONY: help ui build test desktop desktop-app generate codeql-autobuild-prepare

help: ## Show available targets.
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

ui: ## Build the web UI and stage it for embedding into the Go binary (pkg/webui/dist).
	bash scripts/stage-webui.sh

build: ui ## Build the ksail binary with the web UI embedded.
	go build -o ksail .

desktop: ui ## Build the KSail desktop app (CGO + a system webview required); output: ./ksail-desktop.
	cd $(DESKTOP_DIR) && go build -tags desktop -o ../ksail-desktop .

desktop-app: ui ## Build the macOS KSail.app bundle (macOS only); output: ./KSail.app.
	cd $(DESKTOP_DIR) && go build -tags desktop -ldflags "-s -w" -o ksail-desktop .
	bash $(DESKTOP_DIR)/scripts/make-macos-app.sh "$(DESKTOP_DIR)/ksail-desktop" "KSail.app" "$(VERSION)"

test: ## Run the Go unit tests.
	go test ./...

generate: ## Regenerate ALL generated artifacts (JSON schema, CRD/deepcopy, reference docs, chat docs, mocks, web UI types). Ordering matters: schema before web UI types, docs before chat docs.
	go generate ./schemas/... ./pkg/apis/...
	go generate ./docs/...
	go generate ./pkg/svc/chat/...
	mockery
	[ -d web/ui/node_modules ] || npm --prefix web/ui ci && npm --prefix web/ui run gen:types

codeql-autobuild-prepare:
	@bash .github/scripts/codeql-autobuild-prepare.sh "$$PPID"
