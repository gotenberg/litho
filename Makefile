.PHONY: lint
lint: ## Lint Golang codebase
	golangci-lint run

.PHONY: lint-todo
lint-todo: ## Find TODOs in Golang codebase
	golangci-lint run --no-config --enable godox

.PHONY: fmt
fmt: ## Format Golang codebase and "optimize" the dependencies
	go fix ./...
	golangci-lint fmt
	go mod tidy