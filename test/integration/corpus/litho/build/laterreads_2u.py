#!/usr/bin/env python3
"""Build laterreads_2u.pdf: what a later remediation fixer reads of an earlier one.

Each page carries a construct a remediation fixer read past what an earlier
fixer, or an earlier page, had already settled. No gated corpus file reaches
them:

Page 1 states no /Resources, and no node of its page tree does, and it has no
/Contents. Its one annotation, a Square, draws an appearance stream that
carries an /OPI key (ISO 19005-2, 6.2.9 t1) and names, in its own /Resources,
an image with /Interpolate true (6.2.8 t3). The graphics repair skipped the
page, its appearances with it.

Page 2's /Annots is an indirect array: a Screen annotation, which no PDF/A part
permits, with /CA 0.5 and no appearance, and a Square with /CA 0.5 and no
appearance. The annotations fixer removes the Screen from the array and nulls
it; annot-appearance (from part 2) and annot-opacity (part 1) then read the
array as the file wrote it and wrote the Screen back, repaired, as an orphan.

Page 3's /Annots is an indirect array too: a Hidden FileAttachment written
inline, whose inline file specification has /F, no /UF and an embedded text
file, and a Square. annot-flags removes the FileAttachment, which shows
nowhere, and wrote the filtered array into the page, leaving the array object
as the file wrote it, for an incremental update to keep and for filespec-fuf
to repair and write back.

Pages 4 and 5 show text with unembedded ZapfDingbats fonts, which carry no
/ToUnicode (ISO 19005-2, 6.2.11.7.2):

  Zp  named by the /Resources of pages 4 and 5, shown on page 5 alone. The
      fonts repair decided on page 4, which shows no code with it.
  Zf  named by the form page 5 draws, and by no page.
  Za  named by the appearance of page 5's annotation, and by no page.

Every annotation but the FileAttachment has /F 4. The image on page 1 is in an
ICCBased space, and the file carries a CMYK PDF/A output intent: veraPDF looks
up a /Default space for a device space in the /Resources page 1 lacks, and a
repair of device colour would give the page /Resources ahead of the graphics
repair. The XMP packet claims PDF/A-2u.

See ../FIXTURES.md for the authoritative expected firing set.
"""

import struct

from _common import (
    Array,
    Dictionary,
    Name,
    Stream,
    String,
    attach_xmp,
    pikepdf,
    save,
    xmp_packet,
)

PRINT = 4
HIDDEN = 2
ICC_HEADER_SIZE = 128
ICC_VERSION_2_1 = 0x02100000

pdf = pikepdf.new()


def icc_profile(device_class, colour_space, components):
    """A structurally minimal ICC profile veraPDF will parse, as remreach_2b.py
    builds it."""
    tag_table = struct.pack(">I", 0)
    header = bytearray(ICC_HEADER_SIZE)
    header[0:4] = struct.pack(">I", ICC_HEADER_SIZE + len(tag_table))
    header[4:8] = b"LTHO"
    header[8:12] = struct.pack(">I", ICC_VERSION_2_1)
    header[12:16] = device_class
    header[16:20] = colour_space
    header[20:24] = b"XYZ "
    header[36:40] = b"acsp"
    header[68:80] = struct.pack(">III", 0x0000F6D6, 0x00010000, 0x0000D32D)
    stream = Stream(pdf, bytes(header) + tag_table)
    stream.N = components
    return pdf.make_indirect(stream)


def form(body, resources, **extra):
    """A form XObject drawing body against resources."""
    s = Stream(pdf, body)
    s.Type = Name.XObject
    s.Subtype = Name.Form
    s.BBox = Array([0, 0, 20, 20])
    s.Resources = resources
    for key, value in extra.items():
        s[Name("/" + key)] = value
    return pdf.make_indirect(s)


def annot(subtype, y, flags=PRINT, **extra):
    """An annotation at height y."""
    return Dictionary(
        Type=Name.Annot,
        Subtype=subtype,
        Rect=Array([50, y, 70, y + 20]),
        F=flags,
        **extra,
    )


def page(resources, content, annots=None):
    """A page drawing content; None leaves /Resources or /Contents out."""
    d = Dictionary(Type=Name.Page, MediaBox=Array([0, 0, 612, 792]))
    if content is not None:
        d.Contents = pdf.make_indirect(Stream(pdf, content))
    if resources is not None:
        d.Resources = resources
    if annots is not None:
        d.Annots = annots
    p = pdf.make_indirect(d)
    pdf.pages.append(pikepdf.Page(p))
    return p


def blank_ap():
    """An empty /AP /N form, which 6.3.3 asks for and which paints nothing."""
    return Dictionary(N=form(b"", Dictionary()))


def zapf():
    """An unembedded ZapfDingbats, with no /Encoding and no /ToUnicode."""
    return pdf.make_indirect(
        Dictionary(Type=Name.Font, Subtype=Name.Type1, BaseFont=Name.ZapfDingbats)
    )


# Page 1: no /Resources anywhere above the page.
interp = Stream(pdf, b"\x80\x80\x80")
interp.Type = Name.XObject
interp.Subtype = Name.Image
interp.Width = 1
interp.Height = 1
interp.ColorSpace = Array([Name.ICCBased, icc_profile(b"mntr", b"RGB ", 3)])
interp.BitsPerComponent = 8
interp.Interpolate = True
ap_opi = form(
    b"q 20 0 0 20 0 0 cm /Im Do Q\n",
    Dictionary(XObject=Dictionary(Im=pdf.make_indirect(interp))),
    OPI=Dictionary(Version=1.3),
)
page(None, None, Array([pdf.make_indirect(annot(Name.Square, 700, AP=Dictionary(N=ap_opi)))]))

# Page 2: a forbidden Screen beside a Square, in an indirect /Annots.
screen = pdf.make_indirect(annot(Name.Screen, 700, CA=0.5))
square = pdf.make_indirect(annot(Name.Square, 660, CA=0.5))
page(Dictionary(), b"", pdf.make_indirect(Array([screen, square])))

# Page 3: a Hidden FileAttachment written inline, in an indirect /Annots.
payload = Stream(pdf, b"litho corpus attachment\n")
payload.Type = Name.EmbeddedFile
attachment = annot(
    Name.FileAttachment,
    700,
    flags=HIDDEN,
    AP=blank_ap(),
    FS=Dictionary(Type=Name.Filespec, F=String("a.txt"), EF=Dictionary(F=pdf.make_indirect(payload))),
)
kept = pdf.make_indirect(annot(Name.Square, 660, AP=blank_ap()))
page(Dictionary(), b"", pdf.make_indirect(Array([attachment, kept])))

# Pages 4 and 5: one font shown past the first page naming it, one a form
# alone names, one an appearance alone names.
zp, zf, za = zapf(), zapf(), zapf()
page(Dictionary(Font=Dictionary(Z=zp)), b"q Q\n")
fm = form(b"BT /D 12 Tf 2 2 Td (#$) Tj ET\n", Dictionary(Font=Dictionary(D=zf)))
ap_font = form(b"BT /D 12 Tf 2 2 Td (%&) Tj ET\n", Dictionary(Font=Dictionary(D=za)))
page(
    Dictionary(Font=Dictionary(Z=zp), XObject=Dictionary(Fm=fm)),
    b"BT /Z 12 Tf 50 700 Td (!\") Tj ET\nq /Fm Do Q\n",
    Array([pdf.make_indirect(annot(Name.Square, 600, AP=Dictionary(N=ap_font), Contents=String("mark")))]),
)

# The PDF/A output intent makes the initial DeviceGray the text paints in
# conforming, so the output-intent repair, which runs before graphics, gives
# page 1 no /Resources of its own.
pdf.Root.OutputIntents = Array(
    [
        pdf.make_indirect(
            Dictionary(
                Type=Name.OutputIntent,
                S=Name.GTS_PDFA1,
                OutputConditionIdentifier=String("litho destination"),
                DestOutputProfile=icc_profile(b"prtr", b"CMYK", 4),
            )
        )
    ]
)
attach_xmp(pdf, xmp_packet(part=2, conformance="U"))
save(pdf, "laterreads_2u.pdf", min_version="1.7")
