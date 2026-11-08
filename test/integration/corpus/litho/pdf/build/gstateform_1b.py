#!/usr/bin/env python3
"""Build gstateform_1b.pdf: graphics states, form XObjects and an ICC mismatch.

Four rules the coverage scan showed unreached, on three unrelated parts of the
document, plus the pair that separates the two form XObject prohibitions:

| Construct                                          | Rule                    |
| -------------------------------------------------- | ----------------------- |
| an `ExtGState` `/SMask` that is not `/None`        | `6.4/1` (A-1b)          |
| an `ExtGState` carrying `/HTO`                     | `6.2.5/3` (A-4)         |
| a form XObject with `/Subtype2 /PS`                | `6.2.5/1` (A-1b)        |
| an ICC profile whose `/N` contradicts its header   | `6.2.3.2/2` (A-1b)      |
| a form XObject carrying `/OPI`                     | `6.2.4/2` (A-1b), `6.2.8.1/1` (A-4) |

The two offending form XObjects sit side by side because the prohibitions they
break are one rule in some parts and two in others. ISO 19005-1 splits them:
6.2.4 test 2 answers for `/OPI` on any XObject, 6.2.5 test 1 for a PostScript
one. ISO 19005-2 and -3 fold both into 6.2.9 test 1. ISO 19005-4, 6.2.8.1 covers
`/OPI` alone, because ISO 32000-2 removed PostScript XObjects from the format
and left part 4 nothing to prohibit. A file carrying only one of the two would
let a checker that reported the wrong one of those rules still look correct.

Every graphics state is invoked with `gs` and every form is drawn with `Do`.
An `ExtGState` that no content stream selects is not part of the page's state,
and a validator that judged it anyway would be answering for a resource the
page never uses. That distinction was measured, not assumed: the same
dictionaries left unused fire nothing in veraPDF.

Each defect is paired with a conforming twin that differs only in the offending
entry: `/SMask /None` against the soft mask, a plain `ExtGState` against `/HTO`,
a plain form against the PostScript and OPI ones, and an ICC profile whose `/N`
agrees with its header against the one where it does not. The twins share the
page, so over-suppression and over-reporting both show up as a count.

See ../FIXTURES.md for the authoritative expected firing set.
"""

import struct

from _common import Array, Dictionary, Name, Stream, String, attach_xmp, pikepdf, save, xmp_packet

pdf = pikepdf.new()

font = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.Type1,
        BaseFont=Name("/Helvetica"),
        Encoding=Name.WinAnsiEncoding,
    )
)

ICC_HEADER_SIZE = 128


def icc_profile(colour_space):
    """A structurally minimal ICC profile declaring the given header space."""
    tag_table = struct.pack(">I", 0)
    header = bytearray(ICC_HEADER_SIZE)
    header[0:4] = struct.pack(">I", ICC_HEADER_SIZE + len(tag_table))
    header[4:8] = b"lith"
    header[8:12] = struct.pack(">I", 0x02100000)
    header[12:16] = b"prtr"
    header[16:20] = colour_space
    header[20:24] = b"XYZ "
    header[36:40] = b"acsp"
    header[68:80] = struct.pack(">III", 0x0000F6D6, 0x00010000, 0x0000D32D)
    return bytes(header) + tag_table


def icc_based(colour_space, components):
    """An ICCBased colour space whose /N may or may not match the header."""
    stream = Stream(pdf, icc_profile(colour_space))
    stream.N = components
    return pdf.make_indirect(Array([Name.ICCBased, pdf.make_indirect(stream)]))


def form(content, **extra):
    """One form XObject, optionally carrying a forbidden entry."""
    stream = Stream(pdf, content)
    stream.Type = Name.XObject
    stream.Subtype = Name.Form
    stream.BBox = Array([0, 0, 40, 40])
    stream.Resources = Dictionary()
    for key, value in extra.items():
        setattr(stream, key, value)
    return pdf.make_indirect(stream)


# The soft mask's own group. It is a transparency group, which is itself
# forbidden at part 1, so the mask stays visibly separate from the state that
# selects it.
mask = form(b"0 0 40 40 re f\n", Group=Dictionary(S=Name.Transparency, CS=Name.DeviceGray))

ext_gstates = Dictionary(
    # 6.4 t1: a soft mask that is not /None.
    GSmask=pdf.make_indirect(
        Dictionary(Type=Name.ExtGState, SMask=Dictionary(S=Name.Luminosity, G=mask))
    ),
    # The control: the one soft mask value the clause allows.
    GSnone=pdf.make_indirect(Dictionary(Type=Name.ExtGState, SMask=Name("/None"))),
    # 6.2.5 t3 at part 4: a halftone origin.
    GShto=pdf.make_indirect(Dictionary(Type=Name.ExtGState, HTO=Name.Default)),
    # The control: a graphics state whose entries no part objects to.
    GSplain=pdf.make_indirect(Dictionary(Type=Name.ExtGState, LW=1)),
)

xobjects = Dictionary(
    # 6.2.5 t1 at part 1: a PostScript form XObject. Part 4 has no rule for it.
    FmPS=form(b"0 0 40 40 re f\n", Subtype2=Name.PS),
    # 6.2.4 t2 at part 1, 6.2.8.1 t1 at part 4: the OPI key.
    FmOPI=form(b"0 0 40 40 re f\n", OPI=Dictionary(Version=Dictionary())),
    # The control: a form that carries neither.
    FmOK=form(b"0 0 40 40 re f\n"),
)

colour_spaces = Dictionary(
    # 6.2.3.2 t2: three components declared over a four-component header.
    CSbad=icc_based(b"CMYK", 3),
    # The control: the same header with the component count it implies.
    CSok=icc_based(b"CMYK", 4),
)

CONTENT = b"""BT /F1 12 Tf 50 750 Td (graphics states, forms and ICC profiles) Tj ET
q /GSmask gs 50 700 40 40 re f Q
q /GSnone gs 100 700 40 40 re f Q
q /GShto gs 150 700 40 40 re f Q
q /GSplain gs 200 700 40 40 re f Q
q 1 0 0 1 50 640 cm /FmPS Do Q
q 1 0 0 1 100 640 cm /FmOPI Do Q
q 1 0 0 1 150 640 cm /FmOK Do Q
q /CSbad cs 0 0 0 sc 50 580 40 40 re f Q
q /CSok cs 0 0 0 0 sc 100 580 40 40 re f Q
"""

page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(
            Font=Dictionary(F1=font),
            ExtGState=ext_gstates,
            XObject=xobjects,
            ColorSpace=colour_spaces,
        ),
        Contents=Stream(pdf, CONTENT),
    )
)
pdf.pages.append(pikepdf.Page(page))

attach_xmp(pdf, xmp_packet(part=1, conformance="B"))

# Deliberately no /OutputIntents, an independent defect that keeps the ICC
# component count answering for itself rather than for a missing destination.
save(pdf, "gstateform_1b.pdf", min_version="1.7")
