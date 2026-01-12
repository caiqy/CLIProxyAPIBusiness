WEB_DIR := web
WEB_DIST := $(WEB_DIR)/dist
EMBED_DIST := internal/webui/dist
BIN_DIR := bin
BIN := $(BIN_DIR)/cpab

.PHONY: build web-build web-embed backend-build clean

build: web-embed backend-build

web-build:
	cd $(WEB_DIR) && npm run build

web-embed: web-build
	rm -rf $(EMBED_DIST)
	mkdir -p $(dir $(EMBED_DIST))
	cp -R $(WEB_DIST) $(EMBED_DIST)

backend-build:
	mkdir -p $(BIN_DIR)
	go build -o $(BIN) ./cmd/business

clean:
	rm -rf $(BIN_DIR) $(EMBED_DIST)
