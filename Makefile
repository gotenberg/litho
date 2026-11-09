.PHONY: help
help: ## Show the help
	@grep -hE '^[A-Za-z0-9_ \-]*?:.*##.*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

# golangci-lint runs at a pinned version, so a Homebrew upgrade cannot move the
# findings under the zero-warnings policy. An env var of the same name overrides
# it.
GOLANGCI_LINT_VERSION ?= v2.15.0
GOLANGCI_LINT = go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

.PHONY: fmt
fmt: ## Format Golang codebase and "optimize" the dependencies
	go fix ./...
	$(GOLANGCI_LINT) fmt
	go mod tidy

.PHONY: prettify
prettify: ## Format non-Golang codebase
	npx prettier --write .

.PHONY: lint
lint: ## Lint Golang codebase with the pinned golangci-lint (GOLANGCI_LINT_VERSION)
	$(GOLANGCI_LINT) run

.PHONY: lint-todo
lint-todo: ## Find TODOs in Golang codebase
	$(GOLANGCI_LINT) run --no-config --default=none --enable godox

# unparam publishes no releases, so the pin is a module pseudo-version. The
# 2025-10-27 commit, built on golang.org/x/tools v0.38.0, aborts on Go 1.27 with
# "package ... without types was imported"; the pinned one builds on v0.52.0.
# An env var of the same name overrides it.
UNPARAM_VERSION ?= v0.0.0-20261011093844-7a2c2c0b7aa6

.PHONY: lint-unparam
lint-unparam: ## Find unused parameters and results with the pinned unparam (UNPARAM_VERSION)
	go run mvdan.cc/unparam@$(UNPARAM_VERSION) ./...

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

# Runs natively on linux/amd64 only; elsewhere a 386 binary needs emulation.
.PHONY: test-unit-386
test-unit-386: ## Run unit tests on linux/386, where int is 32 bits
	CGO_ENABLED=0 GOOS=linux GOARCH=386 go test ./...

.PHONY: bench
bench: ## Run all benchmarks
	go test -run=^$$ -bench=. -benchmem ./...

OUTLINE_GOLDENS := pkg/internal/typography/truetype/testdata pkg/internal/typography/cff/testdata

.PHONY: outline-goldens
outline-goldens: ## Regenerate the TrueType and CFF outline goldens with the fontTools release test/integration/corpus/versions.env pins (an env var of the same name overrides it)
	@set -e; \
	version="$${FONTTOOLS_VERSION:-$$(sed -n 's/^FONTTOOLS_VERSION=//p' test/integration/corpus/versions.env)}"; \
	venv="$$(mktemp -d)"; trap 'rm -rf "$$venv"' EXIT; \
	python3 -m venv "$$venv"; \
	"$$venv/bin/pip" install --quiet --disable-pip-version-check "fonttools==$$version"; \
	for dir in $(OUTLINE_GOLDENS); do "$$venv/bin/python" -I "$$dir/gen_outlines_golden.py"; done; \
	npx prettier --write $(addsuffix /outlines_golden.json,$(OUTLINE_GOLDENS))

ICC_GOLDENS := pkg/internal/graphics/icc/testdata

.PHONY: icc-goldens
icc-goldens: ## Regenerate the ICC transform goldens with the Pillow (littleCMS) release test/integration/corpus/versions.env pins (an env var of the same name overrides it)
	@set -e; \
	version="$${PILLOW_VERSION:-$$(sed -n 's/^PILLOW_VERSION=//p' test/integration/corpus/versions.env)}"; \
	venv="$$(mktemp -d)"; trap 'rm -rf "$$venv"' EXIT; \
	python3 -m venv "$$venv"; \
	"$$venv/bin/pip" install --quiet --disable-pip-version-check "pillow==$$version"; \
	"$$venv/bin/python" -I $(ICC_GOLDENS)/gen_transform_golden.py; \
	npx prettier --write $(ICC_GOLDENS)/transform_golden.json

# The corpus CLI. A corpus goes through four steps before it is scored, each
# skipping what is already done; corpus-prepare runs every one it needs, and
# corpus, corpus-check and corpus-baseline run corpus-prepare first:
#
#   fetch -> build -> references -> goldens -> score
#
# Every target takes CORPUS, comma-separated corpus names, globs or sources
# (CORPUS=litho/docx,*/xlsx,poi; empty = all). Versions come from
# test/integration/corpus/versions.env; an env var of the same name overrides one.
CORPUS_CLI := go run ./test/integration/cmd/corpus
CORPUS_SELECT = $(if $(CORPUS),--corpus=$(CORPUS))
CORPUS_SCORE_FLAGS = $(CORPUS_SELECT) $(if $(FEATURE),--feature=$(FEATURE)) $(if $(PROFILE),--profile=$(PROFILE)) \
	$(if $(WORKERS),--workers=$(WORKERS)) $(if $(FORMAT),--format=$(FORMAT)) $(if $(ALLOW_VERSION_DRIFT),--allow-version-drift)

.PHONY: corpus
corpus: corpus-prepare ## Score litho across the (corpus, feature) matrix. Vars: CORPUS, FEATURE, PROFILE, WORKERS, FORMAT (text|json), ALLOW_VERSION_DRIFT
	$(CORPUS_CLI) run $(CORPUS_SCORE_FLAGS)

.PHONY: corpus-check
corpus-check: corpus-prepare ## Score, and fail if any cell regressed against test/integration/baseline.json. Vars: as corpus
	$(CORPUS_CLI) check $(CORPUS_SCORE_FLAGS)

.PHONY: corpus-baseline
corpus-baseline: corpus-prepare ## Score, and write the scores to test/integration/baseline.json. Vars: as corpus
	$(CORPUS_CLI) baseline $(CORPUS_SCORE_FLAGS)

.PHONY: corpus-prepare
corpus-prepare: ## Run every corpus step that is missing or stale: fetch, build (into the testdata only), references, goldens (needs Docker). Vars: CORPUS
	$(CORPUS_CLI) prepare $(CORPUS_SELECT)

.PHONY: corpus-fetch
corpus-fetch: ## Step 1: download the fixture fonts and the fetched corpora's files, hash-checked. Vars: CORPUS (or fonts), JOBS
	$(CORPUS_CLI) fetch $(CORPUS_SELECT) $(if $(JOBS),--jobs=$(JOBS))

.PHONY: corpus-build
corpus-build: ## Step 2: generate the inputs kept as sources in git: Office and hostile documents into the testdata, the committed litho/pdf and litho/text PDFs. Vars: CORPUS
	$(CORPUS_CLI) build $(CORPUS_SELECT)

.PHONY: corpus-references
corpus-references: ## Step 3: convert the sources with the pinned Gotenberg image into the Chromium and LibreOffice reference PDFs (needs Docker). Vars: CORPUS, FIXTURES
	$(CORPUS_CLI) references $(CORPUS_SELECT) $(if $(FIXTURES),--fixtures=$(FIXTURES))

.PHONY: corpus-goldens
corpus-goldens: ## Step 4: derive the goldens the corpora lack in the provisioning image (needs Docker). Vars: CORPUS
	$(CORPUS_CLI) goldens $(CORPUS_SELECT)

.PHONY: corpus-update
corpus-update: ## Maintainers: rewrite one corpus's committed lists from its source at its pin, then format them. Vars: CORPUS (one corpus), CHECKOUT (none for govdocs and weasyprint)
	$(CORPUS_CLI) update --corpus=$(CORPUS) $(if $(CHECKOUT),--checkout=$(CHECKOUT))
	npx prettier --write test/integration/corpus/$(CORPUS)/*.json $(if $(filter weasyprint,$(CORPUS)),test/integration/corpus/weasyprint/fixtures)

.PHONY: corpus-tools
corpus-tools: ## Compare the local verapdf, node, java, qpdf, pdfsig, fontTools and Pillow with the pinned versions
	$(CORPUS_CLI) tools

.PHONY: corpus-clean
corpus-clean: ## Remove all corpus testdata; the next corpus-prepare starts over
	rm -rf test/integration/testdata
