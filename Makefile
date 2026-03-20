.PHONY: fmt
fmt: ## Format Golang codebase and "optimize" the dependencies
	go fix ./...
	golangci-lint fmt
	go mod tidy

.PHONY: lint
lint: ## Lint Golang codebase
	golangci-lint run

.PHONY: lint-todo
lint-todo: ## Find TODOs in Golang codebase
	golangci-lint run --no-config --enable godox

.PHONY: test-unit
test-unit: ## Run unit tests
	go test -race ./...

.PHONY: test-integration
test-integration: ## Run integration tests (corpus auto-generated via testcontainers if missing)
	go test -timeout 25m -tags=integration -v github.com/gotenberg/litho/test/integration

.PHONY: clean-corpus
clean-corpus: ## Remove all corpus data (forces regeneration on next test-integration)
	rm -rf test/integration/testdata/pdf.js test/integration/testdata/verapdf
