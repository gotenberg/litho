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

COMPOSE_FONTS := test/integration/testdata/fonts

.PHONY: compose-fixtures
compose-fixtures: ## Regenerate the compose corpus PDFs from their text sources, with the fonts test/integration/corpus/versions.env pins (downloaded and hash-checked first)
	test/integration/corpus/compose/fetch-fonts.sh $(COMPOSE_FONTS)
	go run ./test/integration/corpus/compose/build -fonts $(COMPOSE_FONTS)

WPT_DIR := test/integration/testdata/wpt

.PHONY: wpt-fetch
wpt-fetch: ## Download the web-platform-tests reftests of the wpt corpus at the commit test/integration/corpus/versions.env pins (hash-checked), with Ahem and the fixture fonts
	test/integration/corpus/compose/fetch-fonts.sh $(COMPOSE_FONTS)
	test/integration/corpus/wpt/fetch.sh $(WPT_DIR) $(COMPOSE_FONTS)

SVG_DIR := test/integration/testdata/svg

.PHONY: svg-fetch
svg-fetch: ## Download the resvg tests of the svg corpus at the commit test/integration/corpus/versions.env pins (hash-checked), with their Noto fonts
	test/integration/corpus/svg/fetch.sh $(SVG_DIR) $(COMPOSE_FONTS)

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
	python3 -I $(WEASYPRINT)/extract.py "$$tmp/x/WeasyPrint-$$version" $(WEASYPRINT)/fixtures $(WEASYPRINT)/resources $(COMPOSE_FONTS)/weasyprint; \
	got="$$(shasum -a 256 $(COMPOSE_FONTS)/weasyprint/weasyprint.otf | cut -d ' ' -f 1)"; \
	if [ "$$got" != "$$font_sha" ]; then echo "ERROR: the WeasyPrint test font hashes to $$got, WEASYPRINT_FONT_SHA256 pins $$font_sha" >&2; exit 1; fi
	npx prettier --write $(WEASYPRINT)/fixtures
	test/integration/corpus/compose/fetch-fonts.sh $(COMPOSE_FONTS)

HTML_CORPUS := test/integration/corpus/html

.PHONY: html-chromium
html-chromium: ## Rebuild the html corpus's Chromium reference PDFs with the Gotenberg image test/integration/corpus/versions.env pins (needs Docker). Vars: HTML_NAMES (space-separated sources, default all)
	$(HTML_CORPUS)/chromium.sh $(HTML_CORPUS)/sources $(HTML_CORPUS)/chromium $(HTML_NAMES)

.PHONY: html-fixtures
html-fixtures: ## Rebuild the html corpus's litho PDFs from its sources, with the fonts test/integration/corpus/versions.env pins (downloaded and hash-checked first)
	test/integration/corpus/compose/fetch-fonts.sh $(COMPOSE_FONTS)
	go run ./test/integration/corpus/html/build -fonts $(COMPOSE_FONTS)

OFFICE_TESTDATA := test/integration/testdata

.PHONY: docx-fetch
docx-fetch: ## Download the LibreOffice test documents of the docx corpus at the commit test/integration/corpus/versions.env pins (hash-checked), with the fixture fonts
	test/integration/corpus/compose/fetch-fonts.sh $(COMPOSE_FONTS)
	test/integration/corpus/lib/fetch-office.sh test/integration/corpus/docx $(OFFICE_TESTDATA)/docx/sources

.PHONY: odt-fetch
odt-fetch: ## Download the LibreOffice test documents of the odt corpus at the commit test/integration/corpus/versions.env pins (hash-checked), with the fixture fonts
	test/integration/corpus/compose/fetch-fonts.sh $(COMPOSE_FONTS)
	test/integration/corpus/lib/fetch-office.sh test/integration/corpus/odt $(OFFICE_TESTDATA)/odt/sources

.PHONY: docx-libreoffice
docx-libreoffice: ## Build the docx corpus's LibreOffice reference PDFs into the testdata with the Gotenberg image test/integration/corpus/versions.env pins (needs Docker and make docx-fetch). Vars: NAMES (space-separated references, default all)
	test/integration/corpus/lib/libreoffice.sh test/integration/corpus/docx $(OFFICE_TESTDATA)/docx/sources $(OFFICE_TESTDATA)/docx/libreoffice $(NAMES)

.PHONY: odt-libreoffice
odt-libreoffice: ## Build the odt corpus's LibreOffice reference PDFs into the testdata with the Gotenberg image test/integration/corpus/versions.env pins (needs Docker and make odt-fetch). Vars: NAMES (space-separated references, default all)
	test/integration/corpus/lib/libreoffice.sh test/integration/corpus/odt $(OFFICE_TESTDATA)/odt/sources $(OFFICE_TESTDATA)/odt/libreoffice $(NAMES)

.PHONY: docx-fixtures
docx-fixtures: ## Rebuild the docx corpus's litho PDFs from its committed sources, with the fonts test/integration/corpus/versions.env pins (downloaded and hash-checked first)
	test/integration/corpus/compose/fetch-fonts.sh $(COMPOSE_FONTS)
	go run ./test/integration/corpus/fixture/build -corpus test/integration/corpus/docx -fonts $(COMPOSE_FONTS)

.PHONY: odt-fixtures
odt-fixtures: ## Rebuild the odt corpus's litho PDFs from its committed sources, with the fonts test/integration/corpus/versions.env pins (downloaded and hash-checked first)
	test/integration/corpus/compose/fetch-fonts.sh $(COMPOSE_FONTS)
	go run ./test/integration/corpus/fixture/build -corpus test/integration/corpus/odt -fonts $(COMPOSE_FONTS)

.PHONY: xlsx-fetch
xlsx-fetch: ## Download the LibreOffice test spreadsheets of the xlsx corpus at the commit test/integration/corpus/versions.env pins (hash-checked), with the fixture fonts
	test/integration/corpus/compose/fetch-fonts.sh $(COMPOSE_FONTS)
	test/integration/corpus/lib/fetch-office.sh test/integration/corpus/xlsx $(OFFICE_TESTDATA)/xlsx/sources

.PHONY: xlsx-libreoffice
xlsx-libreoffice: ## Build the xlsx corpus's LibreOffice reference PDFs into the testdata with the Gotenberg image test/integration/corpus/versions.env pins (needs Docker and make xlsx-fetch). Vars: NAMES (space-separated references, default all)
	test/integration/corpus/lib/libreoffice.sh test/integration/corpus/xlsx $(OFFICE_TESTDATA)/xlsx/sources $(OFFICE_TESTDATA)/xlsx/libreoffice $(NAMES)

.PHONY: xlsx-fixtures
xlsx-fixtures: ## Rebuild the xlsx corpus's litho PDFs from its committed sources, with the fonts test/integration/corpus/versions.env pins (downloaded and hash-checked first)
	test/integration/corpus/compose/fetch-fonts.sh $(COMPOSE_FONTS)
	go run ./test/integration/corpus/fixture/build -corpus test/integration/corpus/xlsx -fonts $(COMPOSE_FONTS)

.PHONY: ods-fetch
ods-fetch: ## Download the LibreOffice test spreadsheets of the ods corpus at the commit test/integration/corpus/versions.env pins (hash-checked), with the fixture fonts
	test/integration/corpus/compose/fetch-fonts.sh $(COMPOSE_FONTS)
	test/integration/corpus/lib/fetch-office.sh test/integration/corpus/ods $(OFFICE_TESTDATA)/ods/sources

.PHONY: ods-libreoffice
ods-libreoffice: ## Build the ods corpus's LibreOffice reference PDFs into the testdata with the Gotenberg image test/integration/corpus/versions.env pins (needs Docker and make ods-fetch). Vars: NAMES (space-separated references, default all)
	test/integration/corpus/lib/libreoffice.sh test/integration/corpus/ods $(OFFICE_TESTDATA)/ods/sources $(OFFICE_TESTDATA)/ods/libreoffice $(NAMES)

.PHONY: ods-fixtures
ods-fixtures: ## Rebuild the ods corpus's litho PDFs from its committed sources, with the fonts test/integration/corpus/versions.env pins (downloaded and hash-checked first)
	test/integration/corpus/compose/fetch-fonts.sh $(COMPOSE_FONTS)
	go run ./test/integration/corpus/fixture/build -corpus test/integration/corpus/ods -fonts $(COMPOSE_FONTS)

.PHONY: csv-fetch
csv-fetch: ## Download the LibreOffice test spreadsheets of the csv corpus at the commit test/integration/corpus/versions.env pins (hash-checked), with the fixture fonts
	test/integration/corpus/compose/fetch-fonts.sh $(COMPOSE_FONTS)
	test/integration/corpus/lib/fetch-office.sh test/integration/corpus/csv $(OFFICE_TESTDATA)/csv/sources

.PHONY: csv-libreoffice
csv-libreoffice: ## Build the csv corpus's LibreOffice reference PDFs into the testdata with the Gotenberg image test/integration/corpus/versions.env pins (needs Docker and make csv-fetch). Vars: NAMES (space-separated references, default all)
	test/integration/corpus/lib/libreoffice.sh test/integration/corpus/csv $(OFFICE_TESTDATA)/csv/sources $(OFFICE_TESTDATA)/csv/libreoffice $(NAMES)

.PHONY: csv-fixtures
csv-fixtures: ## Rebuild the csv corpus's litho PDFs from its committed sources, with the fonts test/integration/corpus/versions.env pins (downloaded and hash-checked first)
	test/integration/corpus/compose/fetch-fonts.sh $(COMPOSE_FONTS)
	go run ./test/integration/corpus/fixture/build -corpus test/integration/corpus/csv -fonts $(COMPOSE_FONTS)

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
