# Variable font instance goldens

`varfont.ttf` is a variable TrueType font made for the instancer's tests: five axes (one with an `avar` map), an intermediate master, sparse point sets, composites placed by offsets and by a 2x2 transform, a nested composite, `HVAR`, `VVAR`, `MVAR` and two named instances. `build_varfont.py` builds it with fontTools' `varLib` from masters it draws:

```sh
python3 pkg/internal/typography/varfont/testdata/build_varfont.py
```

`outlines_golden.json` is fontTools' instancer run on `varfont.ttf` at sixteen locations: the normalized coordinates, and for each glyph of each instance its resolved points, advance, left side bearing, box and vertical advance, with the font-wide values the instance changes. `instance_test.go` compares litho's instancer against it. `gen_outlines_golden.py` records it. Regenerate the golden from the repository root:

```sh
make outline-goldens
```
