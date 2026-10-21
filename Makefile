.DEFAULT_GOAL := all
SHELL := /bin/sh

GO ?= go
DIST_DIR ?= dist
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || printf 'dev')

# 进程跟踪依赖 Linux，架构范围与 tracer 的实现、现有发布矩阵保持一致。
TARGETS := linux-amd64 linux-arm64 linux-armv5 linux-armv6 linux-armv7
ASSET_NAMES := linux-x86_64 linux-arm64 linux-armv5 linux-armv6 linux-armv7
LDFLAGS := -X github.com/mzz2017/gg/cmd.Version=$(VERSION) -s -w -buildid=

.PHONY: all build help clean $(TARGETS)

all: build

build: $(TARGETS)

$(TARGETS):
	@set -eu; \
	command -v openssl >/dev/null; \
	arm=''; \
	asset="$@"; \
	case "$@" in \
		linux-amd64) arch=amd64; asset=linux-x86_64 ;; \
		linux-arm64) arch=arm64 ;; \
		linux-armv5) arch=arm; arm=5 ;; \
		linux-armv6) arch=arm; arm=6 ;; \
		linux-armv7) arch=arm; arm=7 ;; \
	esac; \
	mkdir -p "$(DIST_DIR)"; \
	output="$(DIST_DIR)/gg-$$asset"; \
	rm -f "$$output.dgst"; \
	printf 'Building %s (%s)\n' "$$output" "$(VERSION)"; \
	CGO_ENABLED=0 GOOS=linux GOARCH="$$arch" GOARM="$$arm" \
		"$(GO)" build -trimpath -ldflags "$(LDFLAGS)" -o "$$output" .; \
	for algorithm in md5 sha1 sha256 sha512; do \
		checksum=$$(openssl dgst "-$$algorithm" "$$output"); \
		printf '%s\n' "$$checksum" | sed 's/([^)]*)//g'; \
	done > "$$output.dgst"

# 仅清理本文件生成的产物，避免删除自定义输出目录中的其他文件。
clean:
	@set -eu; \
	for asset in $(ASSET_NAMES); do \
		rm -f "$(DIST_DIR)/gg-$$asset" "$(DIST_DIR)/gg-$$asset.dgst"; \
	done

help:
	@printf '%s\n' \
		'make / make build    Build all 5 supported Linux targets and checksums' \
		'make linux-amd64     Build Linux x86_64 only' \
		'make linux-arm64     Build Linux arm64 only' \
		'make linux-armv5     Build Linux ARMv5 (also: linux-armv6, linux-armv7)' \
		'make clean           Remove generated binaries and checksums' \
		'Overrides: VERSION=v1.2.3 DIST_DIR=dist GO=go'
