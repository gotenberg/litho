# Bundled CII XSDs

`Reader.ValidateFacturX` reads its default XSD set from this directory via `embed.FS`. The auto-dispatch picks `d16b/` for Factur-X 1.0+ / ZUGFeRD 2.1+ payloads; legacy ZUGFeRD 1.0 / 2.0 fall back to the structural CII spine rules.

## Current bundle

`d16b/` ships the canonical UN/CEFACT D16B SCRDM (Decoupled Code List Schema Modules) subset published by CEN as part of the EN 16931 artefacts at <https://github.com/ConnectingEurope/eInvoicing-EN16931>.

Four files, ~140 KB total:

- `CrossIndustryInvoice_100pD16B.xsd` (root)
- `CrossIndustryInvoice_QualifiedDataType_100pD16B.xsd`
- `CrossIndustryInvoice_ReusableAggregateBusinessInformationEntity_100pD16B.xsd`
- `CrossIndustryInvoice_UnqualifiedDataType_100pD16B.xsd`

The "decoupled" variant validates structure but not code-list enumeration. That's appropriate for litho: code lists evolve faster than the XSDs and live elsewhere (Mustang or KoSIT typically handle them via Schematron in `pkg/internal/schematron`).

## How to bump

When CEN/EN 16931 publishes a newer revision:

```sh
BASE='https://raw.githubusercontent.com/ConnectingEurope/eInvoicing-EN16931/master/cii/schema/D16B%20SCRDM%20(Subset)/uncoupled%20clm/CII/uncefact/data/standard'
for f in \
  CrossIndustryInvoice_100pD16B.xsd \
  CrossIndustryInvoice_QualifiedDataType_100pD16B.xsd \
  CrossIndustryInvoice_ReusableAggregateBusinessInformationEntity_100pD16B.xsd \
  CrossIndustryInvoice_UnqualifiedDataType_100pD16B.xsd; do
  curl -fsSL "$BASE/$f" -o "pkg/internal/compliance/cii_schemas/d16b/$f"
done
curl -fsSL https://raw.githubusercontent.com/ConnectingEurope/eInvoicing-EN16931/master/LICENSE.txt \
  -o pkg/internal/compliance/cii_schemas/LICENSE-en16931.txt
```

Then `make test-unit` and adjust any drifted assertions (rare; the schema changes annually but the high-level structure is stable).

## License

UN/CEFACT publishes the source XSDs under a perpetual permission-to-redistribute-with-attribution clause, reproduced verbatim in the `<!-- Copyright (C) UN/CEFACT ... -->` header at the top of each XSD. The CEN packaging adds EUPL-1.2 on top; the upstream `LICENSE.txt` is mirrored here as `LICENSE-en16931.txt`. Both notices must be preserved on redistribution.

## Why not auto-download

A `make` target that fetches at build time was considered and rejected: it pins network availability into the build, and reproducing a tagged release ten years from now should not require GitHub still serving the same path. Bundle once, bump deliberately.
