PREFIX     ?= $(HOME)/.local
BINDIR     ?= $(PREFIX)/bin
AUTOSTART  ?= $(HOME)/.config/autostart
GO         ?= go

BIN        := bin/xorway
DESKTOP    := packaging/xorway.desktop

.PHONY: all build run vet fmt install uninstall clean

all: build

build:
	CGO_ENABLED=0 $(GO) build -trimpath -o $(BIN) ./cmd/xorway

run: build
	./$(BIN) --debug

vet:
	$(GO) vet ./...

fmt:
	gofmt -w cmd internal

install: build
	install -Dm755 $(BIN) $(DESTDIR)$(BINDIR)/xorway
	sed 's|@BINDIR@|$(BINDIR)|' $(DESKTOP) > xorway.desktop.tmp
	install -Dm644 xorway.desktop.tmp $(DESTDIR)$(AUTOSTART)/xorway.desktop
	rm -f xorway.desktop.tmp
	@echo "XorWay installé dans $(BINDIR) ; démarrage automatique à la prochaine session."

uninstall:
	-pkill -x xorway
	rm -f $(DESTDIR)$(BINDIR)/xorway
	rm -f $(DESTDIR)$(AUTOSTART)/xorway.desktop

clean:
	rm -rf bin
