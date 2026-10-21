# CFF outline goldens

`outlines_golden.json` is fontTools' reading of `type2_ops.cff`, `g5.cff`, `frutiger_subr_width.cff`, `cid_fdarray_matrix.cff` and `compliance/testdata/lato_bold_subset.cff`: for each glyph in charset order, the pen calls its Type 2 interpreter draws (a seac `endchar` decomposed into its base and accent glyphs) and the advance width. It also holds fontTools' reading of `cff2_ops.cff2`, a bare `CFF2` table: for each glyph in glyph order, the pen calls of the default instance, with an advance of 0 since CFF2 charstrings state no width. `outline_test.go` compares the charstring interpreter against it.

`build_cff2_ops.py` writes `cff2_ops.cff2` byte by byte, with no font library, so fontTools reads a table it did not write: blend and vsindex in charstrings and Private DICTs, a VariationStore of two ItemVariationData, two Font DICTs, local and global subroutines, hints, every curve operator and 121 operands at once. Rebuild it with `python3 build_cff2_ops.py`.

`gen_outlines_golden.py` records it. Regenerate both outline goldens from the repository root:

```sh
make outline-goldens
```

The target installs the fontTools release `FONTTOOLS_VERSION` in `test/integration/corpus/versions.env` pins into a throwaway virtualenv, runs the generators and formats their output with Prettier. `FONTTOOLS_VERSION=4.61.0 make outline-goldens` records with another release.
