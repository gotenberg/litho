"""Record fontTools' reading of TrueType glyf outlines into outlines_golden.json.

For each font, in glyph order: the resolved points of the glyph (composites
placed) with their on-curve flag, the contour end indices, the pen calls
fontTools draws from those points (quadratic segments raised to cubics by
BasePen) and the hmtx advance. outline_test.go compares litho's reader
against it. Run through `make outline-goldens`, which pins fontTools.
"""

import json
import os
import sys

from fontTools.pens.basePen import BasePen
from fontTools.ttLib import TTFont
from fontTools.ttLib.tables._g_l_y_f import Glyph, GlyphCoordinates

HERE = os.path.dirname(os.path.abspath(__file__))

# Each golden key names a font file, read from the path beside it.
FONTS = [
    ("outlines.ttf", os.path.join(HERE, "outlines.ttf")),
    (
        "cidfont_notdef_min.ttf",
        os.path.join(HERE, "..", "..", "..", "compliance", "testdata", "cidfont_notdef_min.ttf"),
    ),
]


class RecordingPen(BasePen):
    """Records moveTo, lineTo, curveTo and closePath as M, L, C and Z rows."""

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


def resolved(coords, ends, flags):
    """Returns a simple glyph holding a glyph's resolved points.

    Drawing a composite hands each component to the pen with its transform,
    and fontTools cannot build that transform for a component placed by
    matched points. Drawing the resolved points as one simple glyph draws
    every composite the way its points place it.
    """
    glyph = Glyph()
    glyph.numberOfContours = len(ends)
    glyph.coordinates = GlyphCoordinates(coords)
    glyph.endPtsOfContours = list(ends)
    glyph.flags = flags
    glyph.program = None
    return glyph


def record(path):
    font = TTFont(path)
    glyf = font["glyf"]
    hmtx = font["hmtx"]
    glyphs = []
    for gid, name in enumerate(font.getGlyphOrder()):
        coords, ends, flags = glyf[name].getCoordinates(glyf)
        pen = RecordingPen(None)
        resolved(coords, ends, flags).draw(pen, glyf)
        glyphs.append(
            {
                "gid": gid,
                "name": name,
                "points": [[x, y, flags[i] & 1] for i, (x, y) in enumerate(coords)],
                "ends": list(ends),
                "ops": pen.ops,
                "advance": hmtx[name][0],
            }
        )
    return {
        "unitsPerEm": font["head"].unitsPerEm,
        "numberOfHMetrics": font["hhea"].numberOfHMetrics,
        "glyphs": glyphs,
    }


def main():
    out = sys.argv[1] if len(sys.argv) > 1 else os.path.join(HERE, "outlines_golden.json")
    goldens = {key: record(path) for key, path in FONTS}
    with open(out, "w") as fh:
        json.dump(goldens, fh)
        fh.write("\n")


if __name__ == "__main__":
    main()
