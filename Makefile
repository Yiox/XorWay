PREFIX     ?= $(HOME)/.local
BINDIR     ?= $(PREFIX)/bin
AUTOSTART  ?= $(HOME)/.config/autostart
APPDIR     ?= $(PREFIX)/share/applications
GO         ?= go
VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

BIN        := bin/xorway
DESKTOP    := internal/install/xorway.desktop

.PHONY: all build run vet test fmt install uninstall clean

all: build

build:
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o $(BIN) ./cmd/xorway

run: build
	./$(BIN) --debug

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

fmt:
	gofmt -w cmd internal

install: build
	install -Dm755 $(BIN) $(DESTDIR)$(BINDIR)/xorway
	sed 's|@BINDIR@|$(BINDIR)|' $(DESKTOP) > xorway.desktop.tmp
	install -Dm644 xorway.desktop.tmp $(DESTDIR)$(AUTOSTART)/xorway.desktop
	install -Dm644 xorway.desktop.tmp $(DESTDIR)$(APPDIR)/xorway.desktop
	rm -f xorway.desktop.tmp
	@echo "XorWay installed in $(BINDIR); it will start automatically with the next session."

uninstall:
	-pkill -x xorway
	rm -f $(DESTDIR)$(BINDIR)/xorway
	rm -f $(DESTDIR)$(AUTOSTART)/xorway.desktop
	rm -f $(DESTDIR)$(APPDIR)/xorway.desktop

clean:
	rm -rf bin
