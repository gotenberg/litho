"""Record fontTools' reading of CFF Type 2 charstrings into outlines_golden.json.

For each bare CFF font, in charset order: the pen calls fontTools' Type 2
interpreter draws (a seac endchar decomposed into its base and accent glyphs)
and the advance width it computes. outline_test.go compares litho's reader
against it. Run through `make outline-goldens`, which pins fontTools.
"""

import io
import json
import os
import sys

from fontTools.cffLib import CFFFontSet
from fontTools.pens.basePen import BasePen

HERE = os.path.dirname(os.path.abspath(__file__))

# Each golden key names a font file, read from the path beside it.
FONTS = [
    ("type2_ops.cff", os.path.join(HERE, "type2_ops.cff")),
    ("g5.cff", os.path.join(HERE, "g5.cff")),
    ("frutiger_subr_width.cff", os.path.join(HERE, "frutiger_subr_width.cff")),
    ("cid_fdarray_matrix.cff", os.path.join(HERE, "cid_fdarray_matrix.cff")),
    (
        "lato_bold_subset.cff",
        os.path.join(HERE, "..", "..", "..", "compliance", "testdata", "lato_bold_subset.cff"),
    ),
]


class RecordingPen(BasePen):
    """Records moveTo, lineTo, curveTo and closePath as M, L, C and Z rows.

    BasePen decomposes a component, the form a seac endchar reaches the pen
    in, by drawing the named glyph from the glyph set through its transform.
    """

    def __init__(self, glyph_set):
        super().__init__(glyph_set)
        self.ops = []

    def _moveTo(self, pt):
        self.ops.append(["M", *pt])

    def _lineTo(self, pt):
        self.ops.append(["L", *pt])

    def _curveToOne(self, pt1, pt2, pt3):
        self.ops.append(["C", *pt1, *pt2, *pt3])

    def _closePath(self):
        self.ops.append(["Z"])


def record(path):
    with open(path, "rb") as fh:
        data = fh.read()
    font_set = CFFFontSet()
    font_set.decompile(io.BytesIO(data), None)
    top = font_set[font_set.fontNames[0]]
    char_strings = top.CharStrings
    glyphs = []
    for gid, name in enumerate(top.charset):
        cs = char_strings[name]
        pen = RecordingPen(char_strings)
        cs.draw(pen)
        glyphs.append({"gid": gid, "name": name, "ops": pen.ops, "advance": cs.width})
    return {"glyphs": glyphs}


def main():
    out = sys.argv[1] if len(sys.argv) > 1 else os.path.join(HERE, "outlines_golden.json")
    goldens = {key: record(path) for key, path in FONTS}
    with open(out, "w") as fh:
        json.dump(goldens, fh)
        fh.write("\n")


if __name__ == "__main__":
    main()
