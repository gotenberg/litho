# Bundled CII XSDs

`Reader.ValidateFacturX` and `Reader.ValidateZUGFeRD` read their XSD sets from this directory via `embed.FS`. The validator routes documents by variant + XMP profile to the matching schema set; the fallback path uses `d16b/` for any v100 CII payload whose conformance level doesn't pick a per-profile bundle.

## Current bundles

`d16b/` ships the canonical UN/CEFACT D16B SCRDM (Decoupled Code List Schema Modules) subset published by CEN as part of the EN 16931 artefacts at <https://github.com/ConnectingEurope/eInvoicing-EN16931>.

Four files, ~140 KB total:

- `CrossIndustryInvoice_100pD16B.xsd` (root)
- `CrossIndustryInvoice_QualifiedDataType_100pD16B.xsd`
- `CrossIndustryInvoice_ReusableAggregateBusinessInformationEntity_100pD16B.xsd`
- `CrossIndustryInvoice_UnqualifiedDataType_100pD16B.xsd`

The "decoupled" variant validates structure but not code-list enumeration. That's appropriate for litho: code lists evolve faster than the XSDs and live in the Schematron rule set under `pkg/internal/compliance/cii_schematron/`.

`facturx_profiles/{MINIMUM,BASIC-WL,BASIC,EN16931,EXTENDED}/` carry the per-profile Factur-X 1.0.07 XSD sets published by FNFE-MPE. Each level tightens the cross-profile D16B set: MINIMUM strips most of BasicInformation, BASIC-WL trims line-item detail, EN 16931 enforces the cross-EU baseline, EXTENDED carries Factur-X-specific extensions. The validator picks the matching schema by XMP fx:ConformanceLevel and applies the FACTUR-X profile XSDs to `urn:factur-x.eu:1p0:*` documents.

`zugferd1p0/` is the legacy ZUGFeRD 1.0 XSD set published by the ZUGFeRD project. ZUGFeRD 1.0 uses the `urn:ferd:CrossIndustryDocument:invoice:1p0` namespace with v12/v15 ram/qdt/udt child types; the v100 D16B schemas pre-date that namespace and do not match.

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

When a new Factur-X profile or ZUGFeRD 1.0 XSD revision is published upstream, refresh from the canonical Git mirror:

```sh
git clone --depth 1 https://github.com/ZUGFeRD/mustangproject.git /tmp/zf
for p in MINIMUM BASIC-WL BASIC EN16931 EXTENDED; do
  cp /tmp/zf/validator/src/main/resources/schema/ZF_240/$p/*.xsd \
    pkg/internal/compliance/cii_schemas/facturx_profiles/$p/
done
cp /tmp/zf/validator/src/main/resources/schema/ZF_10/*.xsd \
  pkg/internal/compliance/cii_schemas/zugferd1p0/
```

## License

UN/CEFACT publishes the source XSDs under a perpetual permission-to-redistribute-with-attribution clause, reproduced verbatim in the `<!-- Copyright (C) UN/CEFACT ... -->` header at the top of each XSD. The CEN packaging adds EUPL-1.2 on top; the upstream `LICENSE.txt` is mirrored here as `LICENSE-en16931.txt`. Both notices must be preserved on redistribution.

## Why not auto-download

A `make` target that fetches at build time was considered and rejected: it pins network availability into the build, and reproducing a tagged release ten years from now should not require GitHub still serving the same path. Bundle once, bump deliberately.
