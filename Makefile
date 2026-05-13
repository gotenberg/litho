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

CORPUS_RUN_FLAGS :=
ifdef CORPUS_JSON_OUTPUT
CORPUS_RUN_FLAGS += --format=json
endif
ifdef CORPUS_NAME
CORPUS_RUN_FLAGS += --corpus=$(CORPUS_NAME)
endif
ifdef CORPUS_FEATURE
CORPUS_RUN_FLAGS += --feature=$(CORPUS_FEATURE)
endif
ifdef CORPUS_PROFILE
CORPUS_RUN_FLAGS += --profile=$(CORPUS_PROFILE)
endif
ifdef CORPUS_WORKERS
CORPUS_RUN_FLAGS += --workers=$(CORPUS_WORKERS)
endif

CORPUS_INIT_FLAGS :=
ifdef CORPUS_NAME
CORPUS_INIT_FLAGS += --corpus=$(CORPUS_NAME)
endif

.PHONY: corpus
corpus: ## Score litho across the (corpus, feature) matrix; auto-provisions. Vars: CORPUS_JSON_OUTPUT, CORPUS_NAME, CORPUS_FEATURE, CORPUS_PROFILE, CORPUS_WORKERS
	go run ./test/integration/cmd/corpus init $(CORPUS_INIT_FLAGS)
	go run ./test/integration/cmd/corpus run $(CORPUS_RUN_FLAGS)

.PHONY: corpus-baseline
corpus-baseline: ## Rewrite test/integration/baseline.json with the current matrix scores
	go run ./test/integration/cmd/corpus init $(CORPUS_INIT_FLAGS)
	go run ./test/integration/cmd/corpus baseline $(CORPUS_RUN_FLAGS)

.PHONY: corpus-check
corpus-check: ## Fail if any cell regressed against test/integration/baseline.json
	go run ./test/integration/cmd/corpus init $(CORPUS_INIT_FLAGS)
	go run ./test/integration/cmd/corpus check $(CORPUS_RUN_FLAGS)

.PHONY: clean-corpus
clean-corpus: ## Remove all corpus testdata (forces re-provisioning on next make corpus)
	rm -rf test/integration/testdata
