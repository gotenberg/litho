.PHONY: help
help: ## Show the help
	@grep -hE '^[A-Za-z0-9_ \-]*?:.*##.*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

.PHONY: fmt
fmt: ## Format Golang codebase and "optimize" the dependencies
	go fix ./...
	golangci-lint fmt
	go mod tidy

.PHONY: prettify
prettify: ## Format non-Golang codebase
	npx prettier --write .

.PHONY: lint
lint: ## Lint Golang codebase
	golangci-lint run

.PHONY: lint-todo
lint-todo: ## Find TODOs in Golang codebase
	golangci-lint run --no-config --enable godox

.PHONY: lint-prettier
lint-prettier: ## Lint non-Golang codebase
	npx prettier --check .

# go install golang.org/x/tools/cmd/godoc@latest
.PHONY: godoc
godoc: ## Run a webserver with litho godoc
	$(info http://localhost:6060/pkg/github.com/gotenberg/litho)
	godoc -http=:6060

.PHONY: test-unit
test-unit: ## Run unit tests
	go test -race ./...

.PHONY: bench
bench: ## Run all benchmarks
	go test -run=^$$ -bench=. -benchmem ./...

.PHONY: corpus-verapdf
corpus-verapdf: ## Run VeraPDF corpus comparison (text output)
	go run ./test/integration/cmd/corpus verapdf

.PHONY: corpus-verapdf-json
corpus-verapdf-json: ## Run VeraPDF corpus comparison (JSON output)
	go run ./test/integration/cmd/corpus verapdf --format json

.PHONY: corpus-verapdf-init
corpus-verapdf-init: ## Provision VeraPDF corpus via Docker, then compare
	go run ./test/integration/cmd/corpus verapdf --init

.PHONY: corpus-pdfjs
corpus-pdfjs: ## Run PDF.js corpus comparison (text output)
	go run ./test/integration/cmd/corpus pdfjs

.PHONY: corpus-pdfjs-json
corpus-pdfjs-json: ## Run PDF.js corpus comparison (JSON output)
	go run ./test/integration/cmd/corpus pdfjs --format json

.PHONY: corpus-pdfjs-init
corpus-pdfjs-init: ## Provision PDF.js corpus via Docker, then compare
	go run ./test/integration/cmd/corpus pdfjs --init

.PHONY: clean-corpus
clean-corpus: ## Remove all corpus data (forces regeneration on next test-integration)
	rm -rf test/integration/testdata/pdf.js test/integration/testdata/verapdf
