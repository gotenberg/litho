"""Write varfont_cff2.otf, a variable font with CFF2 outlines made to run
the instancer.

The font is built from CFF masters with fontTools' varLib, as font tools
build CFF2 variable fonts (OpenType specification, CFF2 table), so its
charstrings blend their operands and its Private DICT blends its stems:

  - two axes: wght 200 to 900 (default 200, as Source Sans 3 VF starts at
    its lightest) with an 'avar' segment map, and wdth 75 to 100 (default
    100);
  - masters at the default, at each axis end and an intermediate master at
    wght 600, so the VariationStore holds regions of one and two axes and
    an intermediate region;
  - glyphs of lines and cubic curves, one of three contours, an empty
    space and a glyph whose curves take more operands than one Type 2
    operator holds;
  - 'HVAR', 'MVAR' (x-height, cap height) and named instances, one with a
    PostScript name.

Run it with fontTools installed: python3 build_varfont_cff2.py. The glyphs
are drawn here; the font is part of litho's tests.
"""

import io
import os

from fontTools.designspaceLib import (
    AxisDescriptor,
    DesignSpaceDocument,
    InstanceDescriptor,
    SourceDescriptor,
)
from fontTools.fontBuilder import FontBuilder
from fontTools.pens.t2CharStringPen import T2CharStringPen
from fontTools.ttLib import TTFont
from fontTools.varLib import build

HERE = os.path.dirname(os.path.abspath(__file__))
ORDER = [".notdef", "space", "O", "H", "wave", "dot"]
CMAP = {0x20: "space", 0x4F: "O", 0x48: "H", 0x7E: "wave", 0x2E: "dot"}


def oval(pen, cx, cy, rx, ry, clockwise):
    """Draws an ellipse from its bottom, counter-clockwise or clockwise."""
    k = 0.5523
    if clockwise:
        rx = -rx
    pen.moveTo((cx, cy - ry))
    pen.curveTo((cx + k * rx, cy - ry), (cx + rx, cy - k * ry), (cx + rx, cy))
    pen.curveTo((cx + rx, cy + k * ry), (cx + k * rx, cy + ry), (cx, cy + ry))
    pen.curveTo((cx - k * rx, cy + ry), (cx - rx, cy + k * ry), (cx - rx, cy))
    pen.curveTo((cx - rx, cy - k * ry), (cx - k * rx, cy - ry), (cx, cy - ry))
    pen.closePath()


def rect(pen, x0, y0, x1, y1):
    pen.moveTo((x0, y0))
    pen.lineTo((x1, y0))
    pen.lineTo((x1, y1))
    pen.lineTo((x0, y1))
    pen.closePath()


def master(wght, wdth):
    """Returns one master: stems thicken with wght, glyphs widen with
    wdth."""
    stem = 30 + (wght - 200) // 4
    wide = wdth / 100
    fb = FontBuilder(1000, isTTF=False)
    fb.setupGlyphOrder(ORDER)
    fb.setupCharacterMap(CMAP)
    charStrings, metrics = {}, {}

    def glyph(name, width, draw):
        pen = T2CharStringPen(width, None)
        draw(pen)
        charStrings[name] = pen.getCharString()
        metrics[name] = width

    glyph(".notdef", 500, lambda p: (rect(p, 50, 0, 450, 700), rect(p, 100, 650, 400, 50)))
    glyph("space", int(200 * wide) + stem // 4, lambda p: None)
    w = int(640 * wide) + stem
    glyph("O", w, lambda p: (oval(p, w / 2, 350, w / 2 - 30, 360, False), oval(p, w / 2, 350, w / 2 - 30 - stem, 360 - stem, True)))
    h = int(620 * wide) + stem

    def draw_h(p):
        rect(p, 60, 0, 60 + stem, 700)
        rect(p, h - 60 - stem, 0, h - 60, 700)
        rect(p, 60 + stem, 330, h - 60 - stem, 330 + stem // 2)

    glyph("H", h, draw_h)

    def draw_wave(p):
        p.moveTo((20, 300))
        x = 20
        for i in range(12):
            p.curveTo((x + 20, 300 + 60 + stem // 3), (x + 40, 300 + 60 + stem // 3), (x + 60, 300))
            x += 60
        p.lineTo((x, 300 - stem))
        for i in range(12):
            p.curveTo((x - 20, 300 - stem - 60), (x - 40, 300 - stem - 60), (x - 60, 300 - stem))
            x -= 60
        p.closePath()

    glyph("wave", int(760 * wide), draw_wave)
    glyph("dot", 60 + stem * 2, lambda p: rect(p, 30, 0, 30 + stem, stem))

    fb.setupCFF("LithoVarCFF-ExtraLight", {"FullName": "LithoVarCFF ExtraLight"}, charStrings, {"BlueValues": [-10, 0, 690, 700 + stem // 10], "StdVW": stem, "StdHW": stem // 2})
    fb.setupHorizontalMetrics({name: (metrics[name], 0) for name in ORDER})
    fb.setupHorizontalHeader(ascent=800, descent=-200)
    fb.setupNameTable({"familyName": "LithoVarCFF", "styleName": "ExtraLight", "psName": "LithoVarCFF-ExtraLight"})
    fb.setupOS2(sxHeight=480 + wght // 20, sCapHeight=700, sTypoAscender=800, sTypoDescender=-200, usWinAscent=900, usWinDescent=250, usWeightClass=200)
    fb.setupPost()
    # varLib reads the masters' CFF tables as compiled: a master goes
    # through its bytes.
    buf = io.BytesIO()
    fb.font.save(buf)
    buf.seek(0)
    return TTFont(buf)


def main():
    doc = DesignSpaceDocument()
    for tag, lo, default, hi, mapping in (
        ("wght", 200, 200, 900, [(200, 200), (600, 520), (900, 900)]),
        ("wdth", 75, 100, 100, None),
    ):
        a = AxisDescriptor()
        a.tag = a.name = tag
        a.minimum, a.default, a.maximum = lo, default, hi
        if mapping:
            a.map = mapping
        doc.addAxis(a)
    sources = [(200, 100), (900, 100), (520, 100), (200, 75), (900, 75)]
    for i, (wght, wdth) in enumerate(sources):
        s = SourceDescriptor()
        s.font = master(wght, wdth)
        s.location = {"wght": wght, "wdth": wdth}
        s.name = "master%d" % i
        doc.addSource(s)
    for style, wght, ps in (("Regular", 400, None), ("Bold", 700, "LithoVarCFF-BoldInstance")):
        ins = InstanceDescriptor()
        ins.styleName = style
        ins.familyName = "LithoVarCFF"
        ins.location = {"wght": wght, "wdth": 100}
        if ps:
            ins.postScriptFontName = ps
        doc.addInstance(ins)
    font, _, _ = build(doc)
    font.save(os.path.join(HERE, "varfont_cff2.otf"))


if __name__ == "__main__":
    main()
