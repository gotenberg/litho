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
	$(CORPUS)/lib/fetch-fonts.sh $(FIXTURE_FONTS)
	go run ./$(CORPUS)/litho/text/build -fonts $(FIXTURE_FONTS)

WPT_DIR := test/integration/testdata/wpt

.PHONY: wpt-fetch
wpt-fetch: ## Download the web-platform-tests reftests of the wpt corpus at the commit test/integration/corpus/versions.env pins (hash-checked), with Ahem and the fixture fonts
	$(CORPUS)/lib/fetch-fonts.sh $(FIXTURE_FONTS)
	$(CORPUS)/wpt/fetch.sh $(WPT_DIR) $(FIXTURE_FONTS)

RESVG_DIR := test/integration/testdata/resvg

.PHONY: resvg-fetch
resvg-fetch: ## Download the resvg tests of the resvg corpus at the commit test/integration/corpus/versions.env pins (hash-checked), with their Noto fonts
	$(CORPUS)/resvg/fetch.sh $(RESVG_DIR) $(FIXTURE_FONTS)

WEASYPRINT := test/integration/corpus/weasyprint

.PHONY: weasyprint-fixtures
weasyprint-fixtures: ## Extract the weasyprint corpus fixtures from the WeasyPrint release test/integration/corpus/versions.env pins (tarball and test font hash-checked), and fetch the fixture fonts
	@set -e; \
	pin() { v="$$(printenv "$$1" || true)"; [ -n "$$v" ] || v="$$(sed -n "s/^$$1=//p" test/integration/corpus/versions.env)"; printf '%s' "$$v"; }; \
	version="$$(pin WEASYPRINT_VERSION)"; sha="$$(pin WEASYPRINT_SHA256)"; font_sha="$$(pin WEASYPRINT_FONT_SHA256)"; \
	tmp="$$(mktemp -d)"; trap 'rm -rf "$$tmp"' EXIT; \
	curl -fsSL --retry 3 -o "$$tmp/weasyprint.tar.gz" "https://github.com/Kozea/WeasyPrint/archive/refs/tags/v$$version.tar.gz"; \
	got="$$(shasum -a 256 "$$tmp/weasyprint.tar.gz" | cut -d ' ' -f 1)"; \
	if [ "$$got" != "$$sha" ]; then echo "ERROR: WeasyPrint v$$version hashes to $$got, WEASYPRINT_SHA256 pins $$sha" >&2; exit 1; fi; \
	mkdir "$$tmp/x"; tar -xzf "$$tmp/weasyprint.tar.gz" -C "$$tmp/x"; \
	python3 -I $(WEASYPRINT)/extract.py "$$tmp/x/WeasyPrint-$$version" $(WEASYPRINT)/fixtures $(WEASYPRINT)/resources $(FIXTURE_FONTS)/weasyprint; \
	got="$$(shasum -a 256 $(FIXTURE_FONTS)/weasyprint/weasyprint.otf | cut -d ' ' -f 1)"; \
	if [ "$$got" != "$$font_sha" ]; then echo "ERROR: the WeasyPrint test font hashes to $$got, WEASYPRINT_FONT_SHA256 pins $$font_sha" >&2; exit 1; fi
	npx prettier --write $(WEASYPRINT)/fixtures
	$(CORPUS)/lib/fetch-fonts.sh $(FIXTURE_FONTS)

HTML_CORPORA := litho/html gotenberg/html

.PHONY: html-references
html-references: ## Rebuild the Chromium reference PDFs of the litho/html and gotenberg/html corpora with the Gotenberg image test/integration/corpus/versions.env pins (needs Docker). Vars: NAMES (space-separated sources, default all)
	@set -e; for c in $(HTML_CORPORA); do $(CORPUS)/lib/chromium.sh $(CORPUS)/$$c $(NAMES); done

.PHONY: html-fixtures
html-fixtures: ## Rebuild the litho PDFs of the litho/html and gotenberg/html corpora from their sources, with the fonts test/integration/corpus/versions.env pins (downloaded and hash-checked first)
	$(CORPUS)/lib/fetch-fonts.sh $(FIXTURE_FONTS)
	@set -e; for c in $(HTML_CORPORA); do go run ./$(CORPUS)/fixture/htmlbuild -corpus $(CORPUS)/$$c -fonts $(FIXTURE_FONTS); done

OFFICE_TESTDATA := test/integration/testdata
FORMATS ?= docx odt xlsx ods csv pptx odp

.PHONY: libreoffice-fetch
libreoffice-fetch: ## Download the LibreOffice test documents of the libreoffice corpora at the commit test/integration/corpus/versions.env pins (hash-checked), with the fixture fonts. Vars: FORMATS (space-separated, default all)
	$(CORPUS)/lib/fetch-fonts.sh $(FIXTURE_FONTS)
	@set -e; for f in $(FORMATS); do $(CORPUS)/libreoffice/fetch.sh $(CORPUS)/libreoffice/$$f $(OFFICE_TESTDATA)/libreoffice/$$f/sources; done

.PHONY: office-references
office-references: ## Build the LibreOffice reference PDFs of the libreoffice and gotenberg Office corpora into the testdata with the Gotenberg image test/integration/corpus/versions.env pins (needs Docker and make libreoffice-fetch). Vars: FORMATS (space-separated, default all), NAMES (space-separated references, default all)
	@set -e; for f in $(FORMATS); do for c in libreoffice gotenberg; do \
		$(CORPUS)/lib/libreoffice.sh $(CORPUS)/$$c/$$f $(OFFICE_TESTDATA)/$$c/$$f/sources $(OFFICE_TESTDATA)/$$c/$$f/references $(NAMES); \
	done; done

.PHONY: office-fixtures
office-fixtures: ## Rebuild the litho PDFs of the gotenberg Office corpora from their sources, with the fonts test/integration/corpus/versions.env pins (downloaded and hash-checked first). Vars: FORMATS (space-separated, default all)
	$(CORPUS)/lib/fetch-fonts.sh $(FIXTURE_FONTS)
	@set -e; for f in $(FORMATS); do go run ./$(CORPUS)/fixture/build -corpus $(CORPUS)/gotenberg/$$f -fonts $(FIXTURE_FONTS); done

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
