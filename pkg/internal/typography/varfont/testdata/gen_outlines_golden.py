"""Record fontTools' instances of varfont.ttf into outlines_golden.json.

For each location, in user coordinates: the normalized coordinates fontTools'
instancer computes (2.14 units), and the full instance it writes: for each
glyph in glyph order, its resolved points (composites placed), its advance
and left side bearing, its bounding box and its vertical advance; and the
font-wide values the instance changes. instance_test.go compares litho's
instancer against it. Run through `make outline-goldens`, which pins
fontTools.
"""

import io
import json
import logging
import os

from fontTools.ttLib import TTFont
from fontTools.varLib import instancer

HERE = os.path.dirname(os.path.abspath(__file__))

LOCATIONS = [
    {},
    {"wght": 100},
    {"wght": 300},
    {"wght": 525},
    {"wght": 650},
    {"wght": 777.7},
    {"wght": 900},
    {"wdth": 80},
    {"wdth": 125},
    {"TEST": 33},
    {"wght": 840, "wdth": 90, "TEST": 70},
    {"wght": 50, "wdth": 200},
    {"slnt": -12},
    {"slnt": -5.5, "opsz": 48},
    {"opsz": 9},
    {"opsz": 144, "wght": 700, "slnt": -3},
]

FIELDS = {
    "OS/2": ["sxHeight", "sCapHeight", "sTypoAscender", "sTypoDescender", "usWeightClass", "usWidthClass"],
    "hhea": ["ascent", "descent", "advanceWidthMax"],
    "head": ["xMin", "yMin", "xMax", "yMax"],
}


def num(v):
    """Returns v as an int when it is whole: a transformed component's
    points are not."""
    return int(v) if v == int(v) else v


def main():
    logging.disable(logging.CRITICAL)
    path = os.path.join(HERE, "varfont.ttf")
    golden = []
    for loc in LOCATIONS:
        vf = TTFont(path)
        full = {a.axisTag: loc.get(a.axisTag, a.defaultValue) for a in vf["fvar"].axes}
        axes = {a.axisTag: (a.minValue, a.defaultValue, a.maxValue) for a in vf["fvar"].axes}
        avar = vf["avar"].segments
        normalized = [
            round(instancer.normalize(full[tag], axes[tag], avar.get(tag)) * 16384) for tag in axes
        ]
        out = instancer.instantiateVariableFont(vf, full)
        buf = io.BytesIO()
        out.save(buf)
        buf.seek(0)
        inst = TTFont(buf)
        glyf = inst["glyf"]
        glyphs = []
        for name in inst.getGlyphOrder():
            g = glyf[name]
            coords, _, _ = g.getCoordinates(glyf)
            advance, lsb = inst["hmtx"][name]
            glyphs.append(
                {
                    "name": name,
                    "points": [[num(x), num(y)] for x, y in coords],
                    "advance": advance,
                    "lsb": lsb,
                    "box": [getattr(g, k, 0) for k in ("xMin", "yMin", "xMax", "yMax")],
                    "vadvance": inst["vmtx"][name][0],
                }
            )
        tables = {t: {f: getattr(inst[t], f) for f in fields} for t, fields in FIELDS.items()}
        golden.append({"location": full, "normalized": normalized, "glyphs": glyphs, "tables": tables})
    with open(os.path.join(HERE, "outlines_golden.json"), "w") as f:
        json.dump(golden, f)
        f.write("\n")


if __name__ == "__main__":
    main()
