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
test-integration: ## Run integration tests
	go test -timeout 5m -tags=integration -v github.com/gotenberg/litho/test/integration
