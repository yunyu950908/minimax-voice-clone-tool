BIN_DIR := bin
APP := $(BIN_DIR)/minimax

.PHONY: build run test tidy clean

# 去除编译路径信息，便于生成可复现二进制
GO_BUILD_FLAGS ?= -trimpath
# 剔除符号表和调试信息，并清空 buildid，降低体积
GO_LDFLAGS ?= -s -w -buildid=
# 要求外部链接器移除额外符号，进一步压缩产物
GO_EXTLDFLAGS ?= -Wl,-x

build:
	mkdir -p $(BIN_DIR)
	GOFLAGS="$(GO_BUILD_FLAGS)" go build -ldflags "$(GO_LDFLAGS) -extldflags '$(GO_EXTLDFLAGS)'" -o $(APP) ./cmd/minimax

run:
	go run ./cmd/minimax

test:
	go test ./...

tidy:
	go mod tidy

clean:
	rm -rf $(BIN_DIR)
