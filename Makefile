# suppress output, run `make XXX V=` to be verbose
V := @

# Build
OUT_DIR = ./bin
LD_FLAGS = -ldflags "-s -v -w"
BUILD_CMD = CGO_ENABLED=1 go build -o ${OUT_DIR}/${NAME} ${LD_FLAGS}

.PHONY: build
build: clean
	@echo BUILDING clickhouse
	$(V)${BUILD_CMD} clickhouse
	@echo DONE

.PHONY: lint
lint:
	$(V)golangci-lint run

.PHONY: clean
clean:
	$(V)golangci-lint cache clean
	@echo "Removing $(OUT_DIR)"
	$(V)rm -rf $(OUT_DIR)

.PHONY: vendor
vendor:
	$(V)GOPRIVATE=${VCS}/* go mod tidy -compat=1.19
	$(V)GOPRIVATE=${VCS}/* go mod vendor
	$(V)git add vendor go.mod go.sum

