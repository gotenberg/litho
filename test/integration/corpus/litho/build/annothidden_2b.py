#!/usr/bin/env python3
"""Build annothidden_2b.pdf: annotations that show neither on screen nor in print.

ISO 19005-2, 6.3.2 requires every annotation's Print flag set and its Hidden,
Invisible, NoView and ToggleNoView flags clear. ISO 32000-1, Table 165 keeps a
Hidden annotation off the screen and off paper, and a NoView one off the
screen, printing it only with Print set. PDF/A remediation removes an
annotation that shows nowhere rather than make it visible, with what goes with
it, and repairs the flags of every other one. The constructs no other corpus
file carries sit here, each with its own verdict:

Page 1, whose /Annots is an indirect array:

  * "note", a Text annotation, /F 6 (Hidden, Print), with a pop-up that has no
    /F (6.3.2 test 1 exempts a Popup) and a visible reply, a Text annotation
    whose /IRT names "note" (ISO 32000-1, Table 170). The three go together.
  * "toggle", a Square, /F 288 (NoView, ToggleNoView): it shows on screen when
    the pointer rolls over it, so it stays with its flags repaired.
  * "shown", a Square, /F 4: conforming, the control.
  * "note2", a Text annotation, /F 4, whose pop-up is /F 2 (Hidden): the pop-up
    goes and "note2" loses its /Popup.

Page 2, whose /Annots is a direct array:

  * a Square written inline in /Annots, /F 2 (Hidden).
  * field "pair" with two widgets as /Kids, the first /F 2 (Hidden), the second
    /F 4: the field keeps its second widget.
  * field "lone" whose only widget is /F 32 (NoView without Print): the field
    goes with it, and /CO loses it.
  * field "group", a non-terminal field whose only child "item" is merged with
    its widget, /F 6: "item" goes, and "group" with it.
  * field "sig", a signed signature, /F 2 (Hidden): the signature is reached
    through the field, so the widget stays with its flags repaired. Its
    /Contents is the openssl CMS blob of sigcms_2b.pdf
    (pkg/internal/compliance/testdata/cms_real.der), and its /ByteRange is
    patched after the save to cover the file, so ISO 19005-2, 6.4.3 is silent.

Every annotation but the pop-ups carries an empty /AP /N form, so 6.3.3 is
silent, and the pages draw nothing. The XMP packet claims PDF/A-2b.

See ../README.md for the authoritative expected firing set.
"""

import os
import re

from _common import (
    Array,
    Dictionary,
    Name,
    Stream,
    String,
    attach_xmp,
    out_path,
    pikepdf,
    xmp_packet,
)

HERE = os.path.dirname(os.path.abspath(__file__))
CMS_REAL = os.path.join(
    HERE, "..", "..", "..", "..", "..", "pkg", "internal", "compliance", "testdata", "cms_real.der"
)
# A fixed-width placeholder the post-save patch overwrites in place.
PLACEHOLDER = 9999999999

pdf = pikepdf.new()


def appearance():
    """An empty /AP /N form: 6.3.3 asks for one, and it paints nothing."""
    s = Stream(pdf, b"")
    s.Type = Name.XObject
    s.Subtype = Name.Form
    s.BBox = Array([0, 0, 20, 20])
    s.Resources = Dictionary()
    return Dictionary(N=pdf.make_indirect(s))


def annot(subtype, flags, y, **extra):
    """An annotation at height y with an empty appearance, /F flags or none."""
    d = Dictionary(
        Type=Name.Annot,
        Subtype=subtype,
        Rect=Array([50, y, 70, y + 20]),
        AP=appearance(),
        **extra,
    )
    if flags is not None:
        d.F = flags
    return d


def page(annots):
    """A page drawing nothing."""
    p = pdf.make_indirect(
        Dictionary(
            Type=Name.Page,
            MediaBox=Array([0, 0, 612, 792]),
            Resources=Dictionary(),
            Contents=pdf.make_indirect(Stream(pdf, b"")),
            Annots=annots,
        )
    )
    pdf.pages.append(pikepdf.Page(p))
    return p


# ISO 32000-1, Table 165: bit 2 Hidden, bit 3 Print, bit 6 NoView, bit 9
# ToggleNoView.
HIDDEN, PRINT, NOVIEW, TOGGLE = 2, 4, 32, 256

note = pdf.make_indirect(annot(Name.Text, HIDDEN | PRINT, 700, Contents=String("note")))
popup = pdf.make_indirect(
    Dictionary(Type=Name.Annot, Subtype=Name.Popup, Rect=Array([80, 680, 200, 740]), Parent=note)
)
note.Popup = popup
reply = pdf.make_indirect(annot(Name.Text, PRINT, 660, Contents=String("reply"), IRT=note))
toggle = pdf.make_indirect(annot(Name.Square, NOVIEW | TOGGLE, 620))
shown = pdf.make_indirect(annot(Name.Square, PRINT, 580))
note2 = pdf.make_indirect(annot(Name.Text, PRINT, 540, Contents=String("note2")))
popup2 = pdf.make_indirect(
    Dictionary(
        Type=Name.Annot,
        Subtype=Name.Popup,
        Rect=Array([80, 520, 200, 580]),
        F=HIDDEN,
        Parent=note2,
    )
)
note2.Popup = popup2
page(pdf.make_indirect(Array([note, popup, reply, toggle, shown, note2, popup2])))

inline_square = annot(Name.Square, HIDDEN, 700)

pair = pdf.make_indirect(Dictionary(FT=Name.Tx, T=String("pair")))
pair_hidden = pdf.make_indirect(annot(Name.Widget, HIDDEN, 660, Parent=pair))
pair_shown = pdf.make_indirect(annot(Name.Widget, PRINT, 620, Parent=pair))
pair.Kids = Array([pair_hidden, pair_shown])

lone = pdf.make_indirect(Dictionary(FT=Name.Tx, T=String("lone")))
lone_widget = pdf.make_indirect(annot(Name.Widget, NOVIEW, 580, Parent=lone))
lone.Kids = Array([lone_widget])

group = pdf.make_indirect(Dictionary(T=String("group")))
item = pdf.make_indirect(
    annot(Name.Widget, HIDDEN | PRINT, 540, FT=Name.Tx, T=String("item"), V=String("x"), Parent=group)
)
group.Kids = Array([item])

with open(CMS_REAL, "rb") as fh:
    der = fh.read()
signature = pdf.make_indirect(
    Dictionary(
        Type=Name.Sig,
        Filter=Name("/Adobe.PPKLite"),
        SubFilter=Name("/adbe.pkcs7.detached"),
        Name=String("sig"),
        ByteRange=Array([0, PLACEHOLDER, PLACEHOLDER, PLACEHOLDER]),
        Contents=pikepdf.Object.parse(b"<" + der.hex().upper().encode() + b">"),
    )
)
sig = pdf.make_indirect(annot(Name.Widget, HIDDEN, 500, FT=Name.Sig, T=String("sig"), V=signature))

page(Array([inline_square, pair_hidden, pair_shown, lone_widget, item, sig]))

pdf.Root.AcroForm = Dictionary(
    Fields=Array([pair, lone, group, sig]),
    CO=Array([lone, pair]),
    SigFlags=3,
    DA=String("/Helv 0 Tf 0 g"),
)
attach_xmp(pdf, xmp_packet(part=2, conformance="B"))

path = out_path("annothidden_2b.pdf")
pdf.save(
    path,
    min_version="1.7",
    deterministic_id=True,
    compress_streams=False,
    stream_decode_level=pikepdf.StreamDecodeLevel.none,
)

# Patch the /ByteRange against where the /Contents landed, as sigcms_2b.py
# does: a signature cannot know its offset before the file is laid out.
data = bytearray(open(path, "rb").read())
total = len(data)
patched = 0
for obj in re.finditer(rb"\d+ 0 obj(.*?)endobj", bytes(data), re.S):
    body = obj.group(1)
    if b"/Type /Sig" not in body:
        continue
    base = obj.start(1)
    br = re.search(rb"/ByteRange \[ 0 %d %d %d \]" % (PLACEHOLDER, PLACEHOLDER, PLACEHOLDER), body)
    contents = re.search(rb"/Contents <[0-9a-fA-F]*>", body)
    if not (br and contents):
        raise SystemExit("the signature object is not in the expected shape")
    start = base + contents.start() + len(b"/Contents ")
    end = base + contents.end()
    replacement = b"/ByteRange [ 0 %010d %010d %010d ]" % (start, end, total - end)
    if len(replacement) != br.end() - br.start():
        raise SystemExit("the patched /ByteRange changes width, which would move an xref offset")
    data[base + br.start() : base + br.end()] = replacement
    patched += 1
if patched != 1:
    raise SystemExit("patched %d signatures, expected 1" % patched)

with open(path, "wb") as fh:
    fh.write(bytes(data))
print("wrote", path, os.path.getsize(path), "bytes")
