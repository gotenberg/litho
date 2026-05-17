# Bundled CII XSDs

`Reader.ValidateFacturX` reads its default XSD set from this directory via `embed.FS`. The auto-dispatch picks `d16b/` for Factur-X 1.0+ / ZUGFeRD 2.1+ payloads; legacy ZUGFeRD 1.0 / 2.0 fall back to the structural CII spine rules.

## Current state

`d16b/CrossIndustryInvoice_100pD16B.xsd` is a **placeholder** until the bundle is bumped. It declares the root element and a wildcard child so the validator pipeline runs end to end. Real type-level checks require the canonical UN/CEFACT D16B schemas.

## How to bump

1. Pull the latest CEN/EN 16931 artefacts:

   ```sh
   git clone --depth 1 https://github.com/ConnectingEurope/eInvoicing-EN16931 /tmp/en16931
   ```

2. Copy the CII XSDs into `d16b/`:

   ```sh
   cp /tmp/en16931/cii/xsd/*.xsd pkg/internal/compliance/cii_schemas/d16b/
   ```

   The root file must remain named `CrossIndustryInvoice_100pD16B.xsd`. Imported XSDs (`ReusableAggregateBusinessInformationEntity_*.xsd`, `QualifiedDataType_*.xsd`, `UnqualifiedDataType_*.xsd`) sit alongside it.

3. Confirm the `make test-unit` suite still passes; bump tests if facet wording shifted.

4. Update the version pin in the `Version` constant of `cii_schemas.go`.

## License

The CEN/EN 16931 artefacts are published under **EUPL-1.2** (or compatible).
Copy the upstream `LICENSE` next to the bundle as `LICENSE-en16931.txt` when bumping. Pure-placeholder commits like this one do not need the license file.

## Why not auto-download

A `make` target that fetches at build time was considered and rejected: it pins network availability into the build, and reproducing a tagged release ten years from now should not require GitHub still serving the same path. Bundle once, bump deliberately.
