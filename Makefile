VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/bernard-linux/bernard/internal/version.Version=$(VERSION)
BIN := bin
PREFIX ?= /usr

.PHONY: all build window test vet install clean

all: vet test build

# Moteur et agent : binaires Go autonomes, sans dépendance.
build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN)/ ./cmd/bernard ./cmd/bernard-agent

# Fenêtre dédiée : nécessite gcc, pkg-config et libwebkit2gtk-4.1-dev.
window:
	cd cmd/bernard-window && PKG_CONFIG_PATH=$(CURDIR)/packaging/pkgconfig CGO_ENABLED=1 \
		go build -trimpath -ldflags "-s -w" -o $(CURDIR)/$(BIN)/bernard-window .

test:
	go test -race ./...

vet:
	gofmt -l . | (! grep .) || (echo "Fichiers non formatés (gofmt -w .)"; exit 1)
	go vet ./...

install: build window
	install -Dm755 $(BIN)/bernard        $(DESTDIR)$(PREFIX)/bin/bernard
	install -Dm755 $(BIN)/bernard-agent  $(DESTDIR)$(PREFIX)/bin/bernard-agent
	install -Dm755 $(BIN)/bernard-window $(DESTDIR)$(PREFIX)/bin/bernard-window
	install -Dm644 packaging/polkit/io.github.bernard_linux.bernard.policy \
		$(DESTDIR)$(PREFIX)/share/polkit-1/actions/io.github.bernard_linux.bernard.policy
	install -Dm644 packaging/applications/io.github.bernard_linux.bernard.desktop \
		$(DESTDIR)$(PREFIX)/share/applications/io.github.bernard_linux.bernard.desktop
	install -Dm644 packaging/icons/bernard.svg \
		$(DESTDIR)$(PREFIX)/share/icons/hicolor/scalable/apps/bernard.svg

clean:
	rm -rf $(BIN)
