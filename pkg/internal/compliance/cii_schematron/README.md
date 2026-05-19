# Bundled CII Schematron

`Reader.ValidateFacturX` reads its default Schematron rule set from this directory via `embed.FS`. The auto-dispatch picks `en16931/` for Factur-X 1.0+ / ZUGFeRD 2.1+ payloads; legacy ZUGFeRD 1.0 / 2.0 fall back to the structural CII spine and XSD rules with no Schematron pass.

## Current bundle

`en16931/EN16931-CII-validation.sch` is the **preprocessed** EN 16931 CII Schematron published by CEN at <https://github.com/ConnectingEurope/eInvoicing-EN16931>. Preprocessing expands the upstream `abstract="true"` patterns into concrete rules, leaving a single self-contained file the loader can consume without abstract-pattern support.

~240 KB. Covers the EN 16931 business rules (BR-_ and BR-CO-_ assertions) that audit invoice totals, mandatory-field cardinalities, and code-list cross-checks. XRechnung is not bundled here yet: its Schematron sources ship as compiled XSLT in KoSIT's validator configuration and would need an extraction step.

## How to bump

When CEN publishes a newer EN 16931 release:

```sh
curl -fsSL "https://raw.githubusercontent.com/ConnectingEurope/eInvoicing-EN16931/master/cii/schematron/preprocessed/EN16931-CII-validation-preprocessed.sch" \
  -o pkg/internal/compliance/cii_schematron/en16931/EN16931-CII-validation.sch
curl -fsSL https://raw.githubusercontent.com/ConnectingEurope/eInvoicing-EN16931/master/LICENSE.txt \
  -o pkg/internal/compliance/cii_schematron/LICENSE-en16931.txt
```

Then `make test-unit` and adjust any drifted assertions. The Schematron evolves a few times a year as the CEN working group revises business rules.

## License

EUPL-1.2, copied verbatim into `LICENSE-en16931.txt` alongside the bundle.

## Why preprocessed

The non-preprocessed sources at `cii/schematron/EN16931-CII-validation.sch` use abstract patterns with parameter substitution (`is-a="…"` + `<let name="…" value="…"/>`). litho's Schematron loader rejects abstract patterns by design so unexpected constructs surface at load time rather than as silent evaluation gaps. CEN ships the preprocessed equivalent in `preprocessed/` precisely for tools that do not implement abstract-pattern expansion; using it lets litho stay narrow without losing rule coverage.
