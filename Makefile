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

CORPUS := test/integration/corpus
FIXTURE_FONTS := test/integration/testdata/fonts

.PHONY: text-fixtures
text-fixtures: ## Regenerate the litho/text corpus PDFs from their text sources, with the fonts test/integration/corpus/versions.env pins (downloaded and hash-checked first)
	go run ./test/integration/cmd/corpus fetch --corpus=fonts
	go run ./$(CORPUS)/litho/text/build -fonts $(FIXTURE_FONTS)

WEASYPRINT := test/integration/corpus/weasyprint

.PHONY: weasyprint-fixtures
weasyprint-fixtures: ## Extract the weasyprint corpus fixtures from the WeasyPrint release test/integration/corpus/versions.env pins (tarball and test font hash-checked), and fetch the fixture fonts
	go run ./test/integration/cmd/corpus select --corpus=weasyprint
	npx prettier --write $(WEASYPRINT)/fixtures
	go run ./test/integration/cmd/corpus fetch --corpus=fonts

# comma and space spell the Make lists the corpus CLI takes comma-separated.
comma := ,
empty :=
space := $(empty) $(empty)

HTML_CORPORA := litho/html gotenberg/html

.PHONY: html-references
html-references: ## Rebuild the Chromium reference PDFs of the litho/html and gotenberg/html corpora with the Gotenberg image test/integration/corpus/versions.env pins (needs Docker). Vars: NAMES (space-separated sources, default all)
	go run ./test/integration/cmd/corpus references --corpus=$(subst $(space),$(comma),$(HTML_CORPORA)) --names="$(NAMES)"

.PHONY: html-fixtures
html-fixtures: ## Rebuild the litho PDFs of the litho/html and gotenberg/html corpora from their sources, with the fonts test/integration/corpus/versions.env pins (downloaded and hash-checked first)
	go run ./test/integration/cmd/corpus fetch --corpus=fonts
	@set -e; for c in $(HTML_CORPORA); do go run ./test/integration/fixture/htmlbuild -corpus $(CORPUS)/$$c -fonts $(FIXTURE_FONTS); done

OFFICE_TESTDATA := test/integration/testdata
SOURCES ?= libreoffice gotenberg poi openxmlsdk govdocs
FORMATS ?= docx odt xlsx ods csv pptx odp doc xls ppt

.PHONY: office-references
office-references: ## Build the LibreOffice reference PDFs of the Office corpora into the testdata with the Gotenberg image test/integration/corpus/versions.env pins (needs Docker and make corpus-fetch). Vars: SOURCES, FORMATS (space-separated, default all), NAMES (space-separated references, default all)
	go run ./test/integration/cmd/corpus references --corpus=$(subst $(space),$(comma),$(strip $(foreach s,$(SOURCES),$(foreach f,$(FORMATS),$(if $(wildcard $(CORPUS)/$(s)/$(f)/manifest.json),$(s)/$(f)))))) --names="$(NAMES)"

.PHONY: office-fixtures
office-fixtures: ## Rebuild the litho PDFs of the gotenberg Office corpora from their sources, with the fonts test/integration/corpus/versions.env pins (downloaded and hash-checked first). Vars: FORMATS (space-separated, default all)
	go run ./test/integration/cmd/corpus fetch --corpus=fonts
	@set -e; for f in $(FORMATS); do [ -d $(CORPUS)/gotenberg/$$f ] || continue; go run ./test/integration/fixture/build -corpus $(CORPUS)/gotenberg/$$f -fonts $(FIXTURE_FONTS); done

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
ifdef CORPUS_ALLOW_VERSION_DRIFT
CORPUS_RUN_FLAGS += --allow-version-drift
endif

CORPUS_INIT_FLAGS :=
ifdef CORPUS_NAME
CORPUS_INIT_FLAGS += --corpus=$(CORPUS_NAME)
endif

.PHONY: corpus
corpus: ## Score litho across the (corpus, feature) matrix; auto-provisions. Vars: CORPUS_JSON_OUTPUT, CORPUS_NAME, CORPUS_FEATURE, CORPUS_PROFILE, CORPUS_WORKERS, CORPUS_ALLOW_VERSION_DRIFT
	go run ./test/integration/cmd/corpus init $(CORPUS_INIT_FLAGS)
	go run ./test/integration/cmd/corpus run $(CORPUS_RUN_FLAGS)

CORPUS_FETCH_FLAGS :=
ifdef CORPUS_NAME
CORPUS_FETCH_FLAGS += --corpus=$(CORPUS_NAME)
endif
ifdef CORPUS_JOBS
CORPUS_FETCH_FLAGS += --jobs=$(CORPUS_JOBS)
endif

.PHONY: corpus-fetch
corpus-fetch: ## Download the fixture fonts and the files of the host-fetched corpora at the pins test/integration/corpus/versions.env holds (hash-checked, in parallel). Vars: CORPUS_NAME (corpora, sources or fonts, comma-separated; default all), CORPUS_JOBS
	go run ./test/integration/cmd/corpus fetch $(CORPUS_FETCH_FLAGS)

.PHONY: corpus-select
corpus-select: ## Rewrite the lists of one corpus from a checkout of its source at its pin, then format them. Vars: CORPUS_NAME (one corpus), CHECKOUT (none for govdocs)
	go run ./test/integration/cmd/corpus select --corpus=$(CORPUS_NAME) $(if $(CHECKOUT),--checkout=$(CHECKOUT))
	npx prettier --write test/integration/corpus/$(CORPUS_NAME)/*.json

.PHONY: corpus-init
corpus-init: ## Provision missing corpora via Docker at the versions test/integration/corpus/versions.env pins (an env var of the same name overrides one). Vars: CORPUS_NAME
	go run ./test/integration/cmd/corpus init $(CORPUS_INIT_FLAGS)

.PHONY: corpus-tools
corpus-tools: ## Compare the local verapdf, node, java, qpdf, pdfsig, pikepdf, fontTools and Pillow with the pinned versions
	go run ./test/integration/cmd/corpus tools

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
