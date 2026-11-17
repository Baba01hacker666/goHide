VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
CC      ?= gcc

.PHONY: build shim test smoke clean install

build: shim
	go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/gohide .
	@echo "built: bin/gohide bin/libgohide.so ($(VERSION))"

shim:
	@mkdir -p bin
	$(CC) -shared -fPIC -O2 -D_GNU_SOURCE -o bin/libgohide.so shim/shim.c

test:
	go vet ./...
	go test ./...

smoke: build
	@out="$$(./bin/gohide -n 'kworker/3:1' -- /bin/sh -c 'cat /proc/self/comm; cat /proc/self/cmdline | tr "\0" " "; echo')"; \
	echo "$$out"; \
	echo "$$out" | head -1 | grep -qx 'kworker/3:1' && echo "smoke OK"

clean:
	rm -rf bin dist

install: build
	install -Dm755 bin/gohide ~/.local/bin/gohide
	install -Dm755 bin/libgohide.so ~/.local/lib/libgohide.so
	@echo "note: run 'gohide preload list' for system-wide hiding (root)"
