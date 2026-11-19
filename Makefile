VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
CC      ?= gcc

.PHONY: build core test smoke clean install

build: core
	go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/gohide .
	@echo "built: bin/gohide bin/gohide-zapper ($(VERSION))"

# ptrace engine, vendored from hackerschoice/zapper (core/zapper.c)
core:
	@mkdir -p bin
	$(CC) -O2 -DZVERSION='"$(VERSION:v%=%)"' -o bin/gohide-zapper core/zapper.c

test:
	go vet ./...
	go test ./...

smoke: build
	@out="$$(./bin/gohide /bin/sh -c 'ps -p $$$$ -o args=')"; \
	echo "$$out"; \
	echo "$$out" | grep -q '^\[kworker/' && echo "smoke OK"

clean:
	rm -rf bin dist

install: build
	install -Dm755 bin/gohide ~/.local/bin/gohide
	install -Dm755 bin/gohide-zapper ~/.local/bin/gohide-zapper
	@echo "installed gohide + ptrace engine to ~/.local/bin"
