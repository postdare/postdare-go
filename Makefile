VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
WEB_DIST := internal/webui/dist
LDFLAGS := -X main.version=$(VERSION)
PREFIX ?= /opt/postdare-go

.PHONY: web build release test install-scripts

web:
	cd web && npm ci && npm run build
	rm -rf $(WEB_DIST)
	mkdir -p $(WEB_DIST)
	cp -R web/dist/. $(WEB_DIST)/
	touch $(WEB_DIST)/.gitkeep

build:
	mkdir -p bin
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/postdare-go .

release: web
	mkdir -p bin
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/postdare-go-linux-amd64 .
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/postdare-go-linux-arm64 .

test:
	go vet ./...
	go test ./...

# Capture scripts ship with the binary that reads their output, so a release can
# install both and the pair never drifts. Machine-specific settings stay out of
# the script: see /etc/postdare-go/ai-review.env.
install-scripts:
	install -d $(DESTDIR)$(PREFIX)/bin
	install -m 0755 examples/ai-review $(DESTDIR)$(PREFIX)/bin/ai-review
