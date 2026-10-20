# TrueType outline goldens

`outlines_golden.json` is fontTools' reading of `outlines.ttf` and of `compliance/testdata/cidfont_notdef_min.ttf`: for each glyph, its resolved points (composites placed) with their on-curve flag, its contour ends, the pen calls fontTools draws from those points (quadratic segments raised to cubics) and its hmtx advance. `outline_test.go` compares the glyf reader against it.

`gen_outlines_golden.py` records it. Regenerate both outline goldens from the repository root:

```sh
make outline-goldens
```

The target installs the fontTools release `FONTTOOLS_VERSION` in `test/integration/corpus/versions.env` pins into a throwaway virtualenv, runs the generators and formats their output with Prettier. `FONTTOOLS_VERSION=4.61.0 make outline-goldens` records with another release.
