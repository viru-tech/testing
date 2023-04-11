# suppress output, run `make XXX V=` to be verbose
V := @

OUT_DIR = ./bin

.PHONY: build
build:
	@echo BUILDING clickhouse
	$(V)go build -o ${OUT_DIR}/clickhouse ./clickhouse
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
	$(V)GOPRIVATE=${VCS}/* go mod tidy -compat=1.20
	$(V)GOPRIVATE=${VCS}/* go mod vendor
	$(V)git add vendor go.mod go.sum

