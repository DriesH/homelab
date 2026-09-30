VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
# Listed explicitly so Go doesn't scan web/node_modules.
GO_PKGS := ./cmd/... ./internal/... ./web
BUNDLE := dist/homelab-$(VERSION)-linux-amd64

.PHONY: web build bundle test dev-api dev-web

web:
	cd web && npm ci && npm run build

build: web
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BUNDLE)/homelab ./cmd/homelab
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BUNDLE)/homelab-agent ./cmd/homelab-agent
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BUNDLE)/stacks/arr/homelab-arr ./cmd/homelab-arr

bundle: build
	install -m 0755 deploy/install.sh $(BUNDLE)/install.sh
	install -m 0644 deploy/lib.sh deploy/homelab.service deploy/homelab-agent.service $(BUNDLE)/
	install -m 0755 deploy/stacks/arr/install.sh $(BUNDLE)/stacks/arr/install.sh
	install -m 0644 deploy/stacks/arr/compose.yaml deploy/stacks/arr/recyclarr.yml $(BUNDLE)/stacks/arr/
	tar -czf $(BUNDLE).tar.gz -C dist $(notdir $(BUNDLE))

test:
	go vet $(GO_PKGS)
	go test $(GO_PKGS)
	cd web && npm run lint && npm run typecheck

# Needs PROXMOX_URL, PROXMOX_TOKEN_ID and PROXMOX_TOKEN_SECRET, and an admin
# created with: HOMELAB_DATA_DIR=.data go run ./cmd/homelab admin
dev-api:
	HOMELAB_DEV=1 HOMELAB_HTTP_ADDR=127.0.0.1:8080 HOMELAB_DATA_DIR=.data go run ./cmd/homelab serve

dev-web:
	cd web && npm run dev
