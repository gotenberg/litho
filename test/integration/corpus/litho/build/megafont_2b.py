#!/usr/bin/env python3
"""Build megafont2b.pdf: a deliberately complex PDF/A-2b fixture packing MANY
independent violations plus several "must NOT fire" constructs that exercise the
usage-scoping guards added by the recent font-width fixes.

Rule 2 of the corpus task: a firing rule must not hide an unmatched rule.
Specifically this checks that the usage-scoping guards (unrendered glyphs skipped)
do not suppress a TRUE positive on a rendered glyph of the SAME font, and that
each violation class is reported independently.

Intended firings (litho and veraPDF must agree exactly):
  A. Type3 font T3A: RENDERED code 65 width mismatch          -> 6.2.11.5
  B. Type3 font T3B: UNRENDERED code 66 width mismatch        -> must NOT fire
  C. simple TrueType font, not embedded                        -> 6.2.11.4.1
  D. DeviceRGB fill with no OutputIntent                       -> 6.2.4.3
  E. DeviceCMYK fill with no OutputIntent                      -> 6.2.4.3
  F. invalid rendering intent name in content                  -> 6.2.6
  G. /Interpolate true on an image                             -> 6.2.8.3
  H. transparency group with no blending colour space          -> 6.2.10
  I. no XMP metadata on the catalog                            -> 6.6.4
  J. /Movie annotation (forbidden annotation type)             -> 6.3.2
"""

import zlib

import pikepdf
from _common import save
from pikepdf import Array, Dictionary, Name, Stream, String


pdf = pikepdf.new()

# --- Type3 font T3A: code 65 IS rendered, its /Widths entry disagrees with d0.
# CharProc declares an advance of 800; /Widths says 500 -> real mismatch.
t3a_a = Stream(pdf, b"800 0 d0\n0 0 700 700 re f\n")
t3a_b = Stream(pdf, b"800 0 d0\n0 0 600 600 re f\n")
t3a = Dictionary(
    Type=Name.Font,
    Subtype=Name.Type3,
    FontBBox=Array([0, 0, 1000, 1000]),
    FontMatrix=Array([0.001, 0, 0, 0.001, 0, 0]),
    CharProcs=Dictionary(gA=t3a_a, gB=t3a_b),
    Encoding=Dictionary(
        Type=Name.Encoding, Differences=Array([65, Name("/gA"), 66, Name("/gB")])
    ),
    FirstChar=65,
    LastChar=66,
    # code 65 rendered -> 500 vs d0 800 = MISMATCH (must fire)
    # code 66 rendered -> 600 matches d0? no: d0 is 800 too. Keep 66 consistent
    # by rendering it with a matching width so only 65 is the violation.
    Widths=Array([500, 800]),
    Resources=Dictionary(),
)

# --- Type3 font T3B: code 66 is NEVER rendered but its /Widths disagrees.
# Must NOT fire (usage-scoping guard, commit c55de28).
t3b_a = Stream(pdf, b"600 0 d0\n0 0 500 500 re f\n")
t3b_b = Stream(pdf, b"900 0 d0\n0 0 400 400 re f\n")
t3b = Dictionary(
    Type=Name.Font,
    Subtype=Name.Type3,
    FontBBox=Array([0, 0, 1000, 1000]),
    FontMatrix=Array([0.001, 0, 0, 0.001, 0, 0]),
    CharProcs=Dictionary(gC=t3b_a, gD=t3b_b),
    Encoding=Dictionary(
        Type=Name.Encoding, Differences=Array([65, Name("/gC"), 66, Name("/gD")])
    ),
    FirstChar=65,
    LastChar=66,
    # code 65 rendered, 600 == d0 600 -> OK.
    # code 66 NOT rendered, 100 != d0 900 -> must be skipped.
    Widths=Array([600, 100]),
    Resources=Dictionary(),
)

# --- Simple TrueType font with no font program at all.
tt = Dictionary(
    Type=Name.Font,
    Subtype=Name.TrueType,
    BaseFont=Name("/Helvetica"),
    Encoding=Name.WinAnsiEncoding,
    FirstChar=65,
    LastChar=66,
    Widths=Array([722, 722]),
    FontDescriptor=Dictionary(
        Type=Name.FontDescriptor,
        FontName=Name("/Helvetica"),
        Flags=32,
        FontBBox=Array([-166, -225, 1000, 931]),
        ItalicAngle=0,
        Ascent=718,
        Descent=-207,
        CapHeight=718,
        StemV=88,
        # deliberately no /FontFile2
    ),
)

# --- An image with /Interpolate true, in DeviceRGB.
raw = bytes([255, 0, 0, 0, 255, 0, 0, 0, 255, 255, 255, 0])
img = Stream(pdf, zlib.compress(raw))
img.Type = Name.XObject
img.Subtype = Name.Image
img.Width = 2
img.Height = 2
img.ColorSpace = Name.DeviceRGB
img.BitsPerComponent = 8
img.Filter = Name.FlateDecode
img.Interpolate = True

# --- A transparency-group form XObject with no /CS on the group.
form = Stream(pdf, b"0 0 1 rg 0 0 50 50 re f\n")
form.Type = Name.XObject
form.Subtype = Name.Form
form.BBox = Array([0, 0, 50, 50])
form.Group = Dictionary(Type=Name.Group, S=Name.Transparency)
form.Resources = Dictionary()

content = b"""q
/BadIntent ri
1 0 0 rg
50 700 100 50 re f
0 0 0 1 k
50 640 100 50 re f
BT /T3A 12 Tf 50 600 Td (AB) Tj ET
BT /T3B 12 Tf 50 570 Td (A) Tj ET
BT /TT1 12 Tf 50 540 Td (AB) Tj ET
q 200 0 0 100 50 400 cm /Im1 Do Q
q 1 0 0 1 50 300 cm /Fm1 Do Q
Q
"""

page = Dictionary(
    Type=Name.Page,
    MediaBox=Array([0, 0, 612, 792]),
    Resources=Dictionary(
        Font=Dictionary(T3A=pdf.make_indirect(t3a), T3B=pdf.make_indirect(t3b), TT1=pdf.make_indirect(tt)),
        XObject=Dictionary(Im1=img, Fm1=form),
    ),
    Contents=Stream(pdf, content),
)
pdf.pages.append(pikepdf.Page(pdf.make_indirect(page)))

# --- A forbidden /Movie annotation.
movie = Dictionary(
    Type=Name.Annot,
    Subtype=Name.Movie,
    Rect=Array([400, 700, 500, 750]),
    F=4,
    T=String("clip"),
    Movie=Dictionary(F=String("clip.mov")),
)
pdf.pages[0].Annots = Array([pdf.make_indirect(movie)])

# Deliberately no /Metadata on the catalog and no /OutputIntents.
save(pdf, "megafont_2b.pdf")
