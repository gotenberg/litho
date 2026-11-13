# Variable font instance goldens

`varfont.ttf` is a variable TrueType font made for the instancer's tests: five axes (one with an `avar` map), an intermediate master, sparse point sets, composites placed by offsets and by a 2x2 transform, a nested composite, `HVAR`, `VVAR`, `MVAR` and two named instances. `build_varfont.py` builds it with fontTools' `varLib` from masters it draws:

```sh
python3 pkg/internal/typography/varfont/testdata/build_varfont.py
```

`varfont_cff2.otf` is a variable font with CFF2 outlines made for the same tests: two axes (one with an `avar` map), an intermediate master, cubic curves, a glyph whose curves take more operands than one Type 2 operator holds, blended Private DICT stems, `HVAR`, `MVAR` and two named instances. `build_varfont_cff2.py` builds it with fontTools' `varLib` from CFF masters it draws:

```sh
python3 pkg/internal/typography/varfont/testdata/build_varfont_cff2.py
```

`outlines_golden.json` is fontTools' instancer run on `varfont.ttf` at sixteen locations: the normalized coordinates, and for each glyph of each instance its resolved points, advance, left side bearing, box and vertical advance, with the font-wide values the instance changes. `instance_test.go` compares litho's instancer against it. `cff2_golden.json` is fontTools' glyph set of `varfont_cff2.otf` at eight locations: for each glyph, its pen calls, unrounded, and its advance with the `HVAR` delta; `cff2_test.go` compares litho's CFF2 instances against it. `gen_outlines_golden.py` records both. Regenerate the goldens from the repository root:

```sh
make outline-goldens
```
