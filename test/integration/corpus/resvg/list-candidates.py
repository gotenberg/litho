#!/usr/bin/env python3
"""List the resvg tests the resvg corpus scores.

Usage: python3 -I list-candidates.py <resvg_checkout> > tests.json

Reads crates/resvg/tests/tests/ of a resvg checkout and prints, as JSON, the
tests the svgdraw feature converts: one object per test with its id (the
SVG's path under crates/resvg/tests/tests/), its reference PNG and the PNG's
size. A test qualifies when it sits in one of the AREAS directories, has a
reference PNG beside it, and uses none of the REJECT constructs: the
features litho's SVG renderer leaves out (filters, masks, patterns,
markers, animation, foreign objects, text on a path) or the text and
rendering properties it does not draw, uses no file outside itself, and
does not test undefined behaviour. Each
area keeps at most its cap, the candidates whose path hashes lowest
(SHA-256), so the choice stays put when resvg adds or removes other tests.
The candidate count and the commonest rejection reasons go to stderr.
Standard library only.
"""

import hashlib
import json
import os
import re
import struct
import sys
from collections import Counter

# Area directory under crates/resvg/tests/tests/, and the most tests it keeps.
AREAS = {
    "structure/svg": 16,
    "structure/g": 4,
    "structure/defs": 4,
    "structure/use": 10,
    "structure/symbol": 8,
    "structure/switch": 6,
    "structure/systemLanguage": 3,
    "structure/style": 10,
    "structure/style-attribute": 4,
    "structure/transform": 12,
    "shapes/rect": 12,
    "shapes/circle": 6,
    "shapes/ellipse": 6,
    "shapes/line": 6,
    "shapes/polyline": 4,
    "shapes/polygon": 4,
    "shapes/path": 16,
    "painting/fill": 10,
    "painting/fill-opacity": 3,
    "painting/fill-rule": 3,
    "painting/stroke": 5,
    "painting/stroke-dasharray": 6,
    "painting/stroke-dashoffset": 3,
    "painting/stroke-linecap": 3,
    "painting/stroke-linejoin": 4,
    "painting/stroke-miterlimit": 3,
    "painting/stroke-opacity": 3,
    "painting/stroke-width": 4,
    "painting/opacity": 4,
    "painting/display": 4,
    "painting/visibility": 4,
    "painting/color": 4,
    "paint-servers/linearGradient": 12,
    "paint-servers/radialGradient": 10,
    "paint-servers/stop": 6,
    "paint-servers/stop-color": 3,
    "paint-servers/stop-opacity": 3,
    "masking/clipPath": 10,
    "masking/clip-rule": 2,
    "text/text": 6,
    "text/tspan": 4,
    "text/text-anchor": 3,
    "text/font-size": 3,
}

# Constructs a test may not use, each with the reason it is rejected.
REJECT = [
    (r"<filter|\bfilter\s*[=:]", "filter"),
    (r"<mask|\bmask\s*[=:]", "mask"),
    (r"<pattern", "pattern"),
    (r"<marker|marker-(start|mid|end)|\bmarker\s*[=:]", "marker"),
    (r"<animate|<set\b", "animation"),
    (r"<foreignObject", "foreignObject"),
    (r"<textPath", "textPath"),
    (r"<image\b", "image"),
    (r"<script|\bon[a-z]+\s*=", "script"),
    (r"<!ENTITY", "XML entity"),
    (r"@import|@font-face|@media", "at-rule"),
    (r"context-fill|context-stroke", "context paint"),
    (r"mix-blend-mode|isolation|paint-order|vector-effect", "compositing"),
    (r"transform-origin|transform-box|\btransform\s*:", "CSS transform"),
    (r"shape-rendering|text-rendering|image-rendering|color-rendering|color-interpolation", "rendering hint"),
    (r"textLength|lengthAdjust|\brotate\s*=", "text adjustment"),
    (r"letter-spacing|word-spacing|text-decoration|baseline-shift|dominant-baseline|alignment-baseline", "text property"),
    (r"font-variant|font-stretch|font-kerning|font-feature|font-variation|writing-mode|direction|unicode-bidi", "text property"),
    (r"\bfont\s*[=:]|font-size-adjust", "font shorthand"),
    (r"spreadMethod\s*=\s*\"(reflect|repeat)\"", "spread method"),
    (r"<style[^>]*>[^<]*:(first|last|nth|not|hover)", "pseudo-class"),
    (r"href\s*=\s*\"(?!#)", "external reference"),
    (r"\bcalc\(|\bvar\(", "CSS function"),
    (r"\b\d*\.?\d+(vw|vh|vmin|vmax|ch|ic|lh|rlh|cap|rem)\b", "unit"),
]
REJECT = [(re.compile(p), why) for p, why in REJECT]

# resvg marks the tests whose expected rendering no specification decides
# with "(UB)", undefined behaviour, in their title.
UNDEFINED = re.compile(r"<title>[^<]*\(UB\)")


def png_size(path):
    with open(path, "rb") as f:
        head = f.read(24)
    if head[:8] != b"\x89PNG\r\n\x1a\n":
        return None
    return struct.unpack(">II", head[16:24])


def main():
    if len(sys.argv) != 2:
        sys.exit("usage: list-candidates.py <resvg_checkout>")
    root = os.path.join(sys.argv[1], "crates", "resvg", "tests", "tests")
    reasons = Counter()
    chosen = []
    total = 0
    for area, cap in sorted(AREAS.items()):
        d = os.path.join(root, area)
        if not os.path.isdir(d):
            sys.exit(f"missing area {area}")
        cands = []
        for name in sorted(os.listdir(d)):
            if not name.endswith(".svg"):
                continue
            rel = f"{area}/{name}"
            png = os.path.join(d, name[:-4] + ".png")
            if not os.path.isfile(png):
                reasons["no reference"] += 1
                continue
            with open(os.path.join(d, name), encoding="utf-8", errors="replace") as f:
                src = f.read()
            why = next((w for p, w in REJECT if p.search(src)), None)
            if not why and UNDEFINED.search(src):
                why = "undefined behaviour"
            if why:
                reasons[why] += 1
                continue
            size = png_size(png)
            if not size:
                reasons["bad reference"] += 1
                continue
            cands.append((hashlib.sha256(rel.encode()).hexdigest(), rel, size))
        total += len(cands)
        for _, rel, size in sorted(cands)[:cap]:
            chosen.append({"id": rel, "png": rel[:-4] + ".png", "width": size[0], "height": size[1]})
    chosen.sort(key=lambda t: t["id"])
    print(f"{total} candidates, {len(chosen)} chosen", file=sys.stderr)
    for why, n in reasons.most_common(12):
        print(f"  rejected {n}: {why}", file=sys.stderr)
    json.dump(chosen, sys.stdout, indent=2)
    print()


if __name__ == "__main__":
    main()
