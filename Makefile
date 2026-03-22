.PHONY: build build-all clean test

SRC_DIR := src
BIN_DIR := bin
BINARY := phoenix-logger

build:
	cd $(SRC_DIR) && go build -o ../$(BIN_DIR)/$(BINARY)-$$(uname -s | tr '[:upper:]' '[:lower:]')-$$(uname -m | sed 's/x86_64/amd64/' | sed 's/aarch64/arm64/') .

build-all:
	cd $(SRC_DIR) && GOOS=darwin GOARCH=arm64 go build -o ../$(BIN_DIR)/$(BINARY)-darwin-arm64 .
	cd $(SRC_DIR) && GOOS=darwin GOARCH=amd64 go build -o ../$(BIN_DIR)/$(BINARY)-darwin-amd64 .
	cd $(SRC_DIR) && GOOS=linux GOARCH=arm64 go build -o ../$(BIN_DIR)/$(BINARY)-linux-arm64 .
	cd $(SRC_DIR) && GOOS=linux GOARCH=amd64 go build -o ../$(BIN_DIR)/$(BINARY)-linux-amd64 .

clean:
	rm -f $(BIN_DIR)/$(BINARY)-*

test:
	cd $(SRC_DIR) && go test -v ./...
