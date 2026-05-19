# Bundled CII Schematron

`Reader.ValidateFacturX` reads its default Schematron rule set from this directory via `embed.FS`. The auto-dispatch picks `en16931/` for Factur-X 1.0+ / ZUGFeRD 2.1+ payloads; legacy ZUGFeRD 1.0 / 2.0 fall back to the structural CII spine and XSD rules with no Schematron pass.

## Current bundles

`en16931/EN16931-CII-validation.sch` is the **preprocessed** EN 16931 CII Schematron published by CEN at <https://github.com/ConnectingEurope/eInvoicing-EN16931>. Preprocessing expands the upstream `abstract="true"` patterns into concrete rules, leaving a single self-contained file the loader can consume without abstract-pattern support.

`xrechnung/XRechnung-CII-validation.sch` plus `xrechnung/common.sch` are the German CIUS rules published by KoSIT at <https://github.com/itplr-kosit/xrechnung-schematron>. The include reference was rewritten from `../common.sch` to `common.sch` so both files sit next to each other inside the bundle directory; the rule content is otherwise verbatim.

~290 KB total. EN 16931 covers the cross-EU baseline (BR-\* and BR-CO-\* business rules: totals consistency, mandatory cardinalities, code-list cross-checks). XRechnung adds the German-specific BR-DE-\* rules on top. The validator auto-routes XRECHNUNG-level documents to the XRechnung schema and everything else to EN 16931.

## How to bump

When CEN publishes a newer EN 16931 release:

```sh
curl -fsSL "https://raw.githubusercontent.com/ConnectingEurope/eInvoicing-EN16931/master/cii/schematron/preprocessed/EN16931-CII-validation-preprocessed.sch" \
  -o pkg/internal/compliance/cii_schematron/en16931/EN16931-CII-validation.sch
curl -fsSL https://raw.githubusercontent.com/ConnectingEurope/eInvoicing-EN16931/master/LICENSE.txt \
  -o pkg/internal/compliance/cii_schematron/LICENSE-en16931.txt
```

When KoSIT publishes a newer XRechnung release:

```sh
BASE='https://raw.githubusercontent.com/itplr-kosit/xrechnung-schematron/master/src/validation/schematron'
curl -fsSL "$BASE/cii/XRechnung-CII-validation.sch" \
  -o pkg/internal/compliance/cii_schematron/xrechnung/XRechnung-CII-validation.sch
curl -fsSL "$BASE/common.sch" \
  -o pkg/internal/compliance/cii_schematron/xrechnung/common.sch
sed -i '' 's|href="../common.sch"|href="common.sch"|' \
  pkg/internal/compliance/cii_schematron/xrechnung/XRechnung-CII-validation.sch
curl -fsSL https://raw.githubusercontent.com/itplr-kosit/xrechnung-schematron/master/LICENSE \
  -o pkg/internal/compliance/cii_schematron/LICENSE-xrechnung.txt
```

Then `make test-unit` and adjust any drifted assertions. The Schematron evolves a few times a year as CEN and KoSIT revise business rules.

## License

Both rule sets ship under permissive licenses copied verbatim alongside the bundle:

- EN 16931: EUPL-1.2 (`LICENSE-en16931.txt`).
- XRechnung: Apache-2.0 (`LICENSE-xrechnung.txt`).

## Why preprocessed

The non-preprocessed sources at `cii/schematron/EN16931-CII-validation.sch` use abstract patterns with parameter substitution (`is-a="…"` + `<let name="…" value="…"/>`). litho's Schematron loader rejects abstract patterns by design so unexpected constructs surface at load time rather than as silent evaluation gaps. CEN ships the preprocessed equivalent in `preprocessed/` precisely for tools that do not implement abstract-pattern expansion; using it lets litho stay narrow without losing rule coverage.
