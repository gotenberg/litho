# Bundled CII Schematron

`Reader.ValidateFacturX` reads its default Schematron rule set from this directory via `embed.FS`. The auto-dispatch picks `en16931/` for Factur-X 1.0+ / ZUGFeRD 2.1+ payloads; legacy ZUGFeRD 1.0 / 2.0 fall back to the structural CII spine and XSD rules with no Schematron pass.

## Current bundles

`en16931/EN16931-CII-validation.sch` is the **preprocessed** EN 16931 CII Schematron published by CEN at <https://github.com/ConnectingEurope/eInvoicing-EN16931>. Preprocessing expands the upstream `abstract="true"` patterns into concrete rules, leaving a single self-contained file the loader can consume without abstract-pattern support.

`xrechnung/XRechnung-CII-validation.sch` plus `xrechnung/common.sch` are the German CIUS rules published by KoSIT at <https://github.com/itplr-kosit/xrechnung-schematron>. The include reference was rewritten from `../common.sch` to `common.sch` so both files sit next to each other inside the bundle directory; the rule content is otherwise verbatim.

`facturx_profiles/FACTUR-X_*.sch` are the per-profile schematrons sourced from Mustang's `validator/src/main/resources/schematron/ZF_240/` directory. They go through `cmd/gen-facturx-schematron` to inline external code-list lookups (`document('FACTUR-X_*_codedb.xml')/...`) the antchfx XPath engine cannot resolve.

`zugferd1p0/ZUGFeRD_1p0.sch` is Mustang's `validator/src/main/resources/ZUGFeRD_1p0.sch`, the legacy ZUGFeRD 1.0 rule set keyed off the `urn:ferd:CrossIndustryDocument:invoice:1p0` namespace. The v100-family EN 16931 / XRechnung / Factur-X profile rules do not match v1's element structure, so v1 documents route through this dedicated pass.

`peppol/PEPPOL-EN16931-CII.sch` is OpenPEPPOL's PEPPOL BIS 3.0 rule set for CII (<https://github.com/OpenPEPPOL/peppol-bis-invoice-3>). It ships **opt-in** because R001 / R010 / R020 require a PEPPOL business-process identifier that Factur-X / ZUGFeRD documents legitimately omit; the validator does not auto-overlay it. Callers that want PEPPOL coverage thread `compliance.WithPeppolSchematron(s)` after `cii_schematron.LoadPeppol(ctx)`.

EN 16931 covers the cross-EU baseline (BR-\* and BR-CO-\* business rules: totals consistency, mandatory cardinalities, code-list cross-checks). XRechnung adds the German-specific BR-DE-\* rules on top. The validator auto-routes XRECHNUNG-level documents to the XRechnung schema and everything else to EN 16931. ZUGFeRD 1.0 documents take the dedicated `zugferd1p0/` pass.

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
# Pull from the tagged release ZIP, NOT the master branch sources -
# the release pipeline merges PEPPOL-EN16931-* rules into the
# XRechnung schematron before publishing, and the master branch
# sources drop them entirely.
TAG=v2.5.0
ZIP=$(curl -sfSL "https://api.github.com/repos/itplr-kosit/xrechnung-schematron/releases/tags/${TAG}" \
  | python3 -c 'import json,sys; print([a["browser_download_url"] for a in json.load(sys.stdin)["assets"] if a["name"].endswith(".zip")][0])')
TMP=$(mktemp -d)
curl -sfSL "$ZIP" -o "$TMP/release.zip"
unzip -p "$TMP/release.zip" schematron/cii/XRechnung-CII-validation.sch \
  > pkg/internal/compliance/cii_schematron/xrechnung/XRechnung-CII-validation.sch
unzip -p "$TMP/release.zip" schematron/common.sch \
  > pkg/internal/compliance/cii_schematron/xrechnung/common.sch
unzip -p "$TMP/release.zip" LICENSE \
  > pkg/internal/compliance/cii_schematron/LICENSE-xrechnung.txt
sed -i '' 's|href="../common.sch"|href="common.sch"|' \
  pkg/internal/compliance/cii_schematron/xrechnung/XRechnung-CII-validation.sch
rm -rf "$TMP"
```

When Mustang publishes a new ZUGFeRD 1.0 schematron release:

```sh
curl -fsSL "https://raw.githubusercontent.com/ZUGFeRD/mustangproject/master/validator/src/main/resources/ZUGFeRD_1p0.sch" \
  -o pkg/internal/compliance/cii_schematron/zugferd1p0/ZUGFeRD_1p0.sch
```

When OpenPEPPOL publishes a newer BIS 3.0 release:

```sh
TAG=v3.0.20
curl -fsSL "https://raw.githubusercontent.com/OpenPEPPOL/peppol-bis-invoice-3/${TAG}/rules/sch/PEPPOL-EN16931-CII.sch" \
  -o pkg/internal/compliance/cii_schematron/peppol/PEPPOL-EN16931-CII.sch
```

Then `make test-unit` and adjust any drifted assertions. The Schematron evolves a few times a year as CEN and KoSIT revise business rules.

## License

Each rule set ships under its upstream licence; the project-level files are kept alongside the bundle:

- EN 16931: EUPL-1.2 (`LICENSE-en16931.txt`).
- XRechnung: Apache-2.0 (`LICENSE-xrechnung.txt`).
- ZUGFeRD 1.0: FeRD-AWV non-discriminatory usage grant (see the comment block at the top of `zugferd1p0/ZUGFeRD_1p0.sch`).
- PEPPOL BIS 3.0: redistributable per the OpenPEPPOL Service Desk terms (the upstream repo carries no LICENSE file at the tagged release).

## Why preprocessed

The non-preprocessed sources at `cii/schematron/EN16931-CII-validation.sch` use abstract patterns with parameter substitution (`is-a="…"` + `<let name="…" value="…"/>`). litho's Schematron loader rejects abstract patterns by design so unexpected constructs surface at load time rather than as silent evaluation gaps. CEN ships the preprocessed equivalent in `preprocessed/` precisely for tools that do not implement abstract-pattern expansion; using it lets litho stay narrow without losing rule coverage.
