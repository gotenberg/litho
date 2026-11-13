"""Write varfont.ttf, a variable TrueType font made to run the instancer.

The font is built from masters with fontTools' varLib, as font tools build
variable fonts, so its tables hold what such fonts hold (OpenType Font
Variations Overview):

  - three axes: wght 100 to 900 (default 400) with an 'avar' segment map,
    wdth 75 to 125 (default 100), and a private axis TEST 0 to 100;
  - masters at the default, at each axis end, and an intermediate master at
    wght 650, so 'gvar' holds shared tuples, embedded peaks and an
    intermediate region, and sparse point sets (varLib drops the deltas
    interpolation infers) that the instancer must infer back;
  - simple glyphs of one and two contours, a composite of two components
    placed by offsets, one scaled by a 2x2 transform, and a composite
    nesting another;
  - 'HVAR', 'VVAR' with 'vhea' and 'vmtx', 'MVAR' (x-height, cap height,
    typographic ascender) and named instances, one with a PostScript name.

Run it with fontTools installed: python3 build_varfont.py. The glyphs
are rectangles and triangles drawn here; the font is part of litho's tests.
"""

import os

from fontTools.designspaceLib import (
    AxisDescriptor,
    DesignSpaceDocument,
    InstanceDescriptor,
    SourceDescriptor,
)
from fontTools.fontBuilder import FontBuilder
from fontTools.pens.ttGlyphPen import TTGlyphPen
from fontTools.ttLib.tables._g_l_y_f import GlyphComponent
from fontTools.varLib import build

HERE = os.path.dirname(os.path.abspath(__file__))
ORDER = [".notdef", "space", "A", "B", "dot", "Adot", "Bscaled", "nested"]
CMAP = {0x20: "space", 0x41: "A", 0x42: "B", 0x2E: "dot", 0xC0: "Adot", 0xC1: "Bscaled", 0xC2: "nested"}


def rect(pen, x0, y0, x1, y1):
    pen.moveTo((x0, y0))
    pen.lineTo((x0, y1))
    pen.lineTo((x1, y1))
    pen.lineTo((x1, y0))
    pen.closePath()


def master(wght, wdth, test):
    """Returns one master: stems thicken with wght, glyphs widen with wdth,
    and TEST moves the apex of A and the dot."""
    stem = 40 + wght // 5
    wide = wdth / 100
    fb = FontBuilder(1000, isTTF=True)
    fb.setupGlyphOrder(ORDER)
    fb.setupCharacterMap(CMAP)
    glyphs, metrics = {}, {}

    pen = TTGlyphPen(None)
    rect(pen, 50, 0, 450, 700)
    rect(pen, 100, 50, 400, 650)
    glyphs[".notdef"] = pen.glyph()
    metrics[".notdef"] = 500

    glyphs["space"] = TTGlyphPen(None).glyph()
    metrics["space"] = int(200 * wide + wght // 20)

    w = int(600 * wide) + stem
    pen = TTGlyphPen(None)
    apex = w // 2 + test
    pen.moveTo((20, 0))
    pen.lineTo((apex, 700))
    pen.qCurveTo((apex + stem, 720), (w - 20, 0))
    pen.lineTo((w - 20 - stem, 0))
    pen.lineTo((apex, 600 - stem))
    pen.lineTo((20 + stem, 0))
    pen.closePath()
    rect(pen, 120, 200, w - 120, 200 + stem // 2)
    glyphs["A"] = pen.glyph()
    metrics["A"] = w

    pen = TTGlyphPen(None)
    rect(pen, 60, 0, 60 + stem, 700)
    pen.moveTo((60 + stem, 700))
    pen.qCurveTo((int(500 * wide), 690), (int(500 * wide), 520), (60 + stem, 360))
    pen.closePath()
    glyphs["B"] = pen.glyph()
    metrics["B"] = int(560 * wide) + stem // 2

    pen = TTGlyphPen(None)
    rect(pen, 30 + test, 0, 30 + test + stem, stem)
    glyphs["dot"] = pen.glyph()
    metrics["dot"] = 60 + stem + test

    def composite(parts):
        g = TTGlyphPen(None).glyph()
        g.numberOfContours = -1
        g.components = []
        for name, x, y, transform in parts:
            c = GlyphComponent()
            c.glyphName, c.x, c.y, c.flags = name, x, y, 0
            if transform:
                c.transform = transform
            g.components.append(c)
        return g

    glyphs["Adot"] = composite([("A", 0, 0, None), ("dot", w // 2 - stem // 2, 760 + test, None)])
    metrics["Adot"] = w
    glyphs["Bscaled"] = composite([("B", 10, 0, [[0.75, 0.25], [0, 1.25]])])
    metrics["Bscaled"] = metrics["B"]
    glyphs["nested"] = composite([("Adot", 0, 0, None), ("Bscaled", w, -20, None)])
    metrics["nested"] = w + metrics["B"]

    fb.setupGlyf(glyphs)
    hmtx = {}
    for name in ORDER:
        g = glyphs[name]
        g.recalcBounds(fb.font["glyf"])
        hmtx[name] = (metrics[name], getattr(g, "xMin", 0))
    fb.setupHorizontalMetrics(hmtx)
    fb.setupHorizontalHeader(ascent=800, descent=-200)
    fb.setupVerticalMetrics({name: (1000 + stem, 100) for name in ORDER})
    fb.setupVerticalHeader(ascent=500, descent=-500)
    fb.setupNameTable({"familyName": "LithoVar", "styleName": "Regular", "psName": "LithoVar-Regular"})
    fb.setupOS2(sxHeight=500 + wght // 10, sCapHeight=700 + test, sTypoAscender=800 + wght // 20, sTypoDescender=-200, usWinAscent=900, usWinDescent=250)
    fb.setupPost()
    return fb.font


def main():
    doc = DesignSpaceDocument()
    for tag, lo, default, hi, mapping in (
        ("wght", 100, 400, 900, [(100, 100), (400, 400), (650, 600), (900, 900)]),
        ("wdth", 75, 100, 125, None),
        ("TEST", 0, 0, 100, None),
    ):
        a = AxisDescriptor()
        a.tag = a.name = tag
        a.minimum, a.default, a.maximum = lo, default, hi
        if mapping:
            a.map = mapping
        doc.addAxis(a)
    sources = [
        (400, 100, 0),
        (100, 100, 0),
        (900, 100, 0),
        (600, 100, 0),
        (400, 75, 0),
        (400, 125, 0),
        (400, 100, 100),
    ]
    for i, (wght, wdth, test) in enumerate(sources):
        s = SourceDescriptor()
        s.font = master(wght, wdth, test)
        s.location = {"wght": wght, "wdth": wdth, "TEST": test}
        s.name = "master%d" % i
        doc.addSource(s)
    for style, wght, ps in (("Light", 300, None), ("Bold", 700, "LithoVar-BoldInstance")):
        ins = InstanceDescriptor()
        ins.styleName = style
        ins.familyName = "LithoVar"
        ins.location = {"wght": wght, "wdth": 100, "TEST": 0}
        if ps:
            ins.postScriptFontName = ps
        doc.addInstance(ins)
    font, _, _ = build(doc)
    font.save(os.path.join(HERE, "varfont.ttf"))


if __name__ == "__main__":
    main()
