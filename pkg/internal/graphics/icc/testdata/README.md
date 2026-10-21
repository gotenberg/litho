# ICC transform goldens

`transform_golden.json` is littleCMS's reading of the conversions between the profiles the package ships (`../profiles`) and the synthetic profiles of this directory, recorded through Pillow's `ImageCms` with optimisation off (`NOOPTIMIZE`), so littleCMS evaluates each profile's own tables. For each pair of profiles, each of the four rendering intents and black point compensation off and on (absolute colorimetric only off), it holds the 8-bit colours a grid of 8-bit inputs converts to. Pillow's `LAB` mode stores a\* and b\* as signed bytes. `link_golden_test.go` compares `NewLink` against it.

The synthetic profiles reach the table types and curve kinds the shipped ones do not. `TestTestdataProfilesAreCommitted` pins each to its builder in `testprofiles_test.go`:

- `v4-cmyk-lut.icc`: a version 4 CMYK output profile over Lab whose tables are `lutAtoBType` and `lutBtoAType`, resampled from the stock CMYK printer profile in the version 4 Lab encoding, behind parametric and sampled identity curves.
- `v4-rgb-display.icc`: a version 4 matrix and tone curve RGB display profile with parametric curves of types 2, 3 and 4.
- `v4-rgb-lut-xyz.icc`: a version 4 RGB display profile over XYZ whose `lutAtoBType` and `lutBtoAType` tables spell sRGB out through their curves, matrices and CLUTs.

Rewrite the synthetic profiles after changing a builder:

```sh
go test ./pkg/internal/graphics/icc -run TestTestdataProfilesAreCommitted -update-icc-testdata
```

`gen_transform_golden.py` records the golden. Regenerate it from the repository root:

```sh
make icc-goldens
```

The target installs the Pillow release `PILLOW_VERSION` in `test/integration/corpus/versions.env` pins into a throwaway virtualenv, runs the generator and formats its output with Prettier. The golden records the Pillow and littleCMS versions it came from. `PILLOW_VERSION=11.2.1 make icc-goldens` records with another release.
