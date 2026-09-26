#!/usr/bin/env python3
"""Build repairleft_4.pdf: repairs that reach inherited colour spaces, keep the
line a text operator moves to, and touch only what a finding names.

The first page states no /Resources and inherits the root node's, which holds
every colour space, the font and a form inline (ISO 32000-1, 7.7.3.4). The
detectors read inherited resources, and so does veraPDF, but three colour
repairs read the page's own /Resources alone, so they repaired nothing in the
round that found the defects. One corpus page inherits a DeviceN the first
repair completes, and none a Separation pair or an ICCBased space the others
repair.

| Name  | Construct, inherited from the root node                  | A-4         |
| ----- | -------------------------------------------------------- | ----------- |
| `CSN` | a DeviceN over SpotA and SpotB with no `/Attributes`     | `6.2.4.4/1` |
| `CS1` | a Separation naming SpotC over one tint transform        | `6.2.4.4/2` |
| `CS2` | a Separation naming SpotC over another                   | `6.2.4.4/2` |
| `CS3` | an ICCBased space over the OutputIntent's own profile    | `6.2.4.2/3` |
| `Fm0` | a form painting in the same ICCBased space of its own    | `6.2.4.2/3` |

The page also shows text with the two operators that do more than show a
string: `string '` moves to the next line first, and `aw ac string "` also sets
the word and character spacing (ISO 32000-1, 9.4.3, Table 109). Each shows
`(\\x01)`, whose code 1 names no glyph under WinAnsiEncoding and shows
`.notdef`, and a visible line follows each. Stripping the control byte empties
the string, and the repair dropped the whole operation with it, so every later
line drew one line higher. No corpus page the `.notdef` repair runs on holds
either operator.

The font is the conforming non-symbolic TrueType of `ttcmap_2b.pdf`, embedded,
with `/Widths` from code 1 so the `.notdef` width agrees with the dictionary.

The first page carries its own XMP packet, whose header holds the `bytes`
attribute every packet is denied (`6.7.2.1/2`), while the catalog packet needs
no repair. The metadata repair returned as soon as it found nothing to patch in
the catalog packet, before it reached the page's.

Two more pages paint DeviceCMYK under a transparency group blending in a device
space: DeviceRGB, which no RGB OutputIntent covers (`6.2.4.3/2`), and
DeviceCMYK, which the CMYK OutputIntent does. The group repair replaced both
with ICCBased sRGB, changing how the conforming group composites. No corpus
file where that repair runs carries a device blending space no finding names.

The tree is built by hand: pikepdf's page list pushes inherited attributes onto
each page, which would delete the inheritance the fixture exists for.

See ../README.md for the authoritative expected firing set.
"""

import struct

import _truetype
from _common import Array, Dictionary, Name, Stream, String, attach_xmp, pikepdf, save, xmp_packet

pdf = pikepdf.new()

ICC_HEADER_SIZE = 128
ICC_VERSION_2_1 = 0x02100000


def icc_profile(device_class, colour_space, tag):
    """A structurally minimal ICC profile, as iccbased_4.py builds it."""
    tag_table = struct.pack(">I", 0)
    header = bytearray(ICC_HEADER_SIZE)
    header[0:4] = struct.pack(">I", ICC_HEADER_SIZE + len(tag_table))
    header[4:8] = tag
    header[8:12] = struct.pack(">I", ICC_VERSION_2_1)
    header[12:16] = device_class
    header[16:20] = colour_space
    header[20:24] = b"XYZ "
    header[36:40] = b"acsp"
    header[68:80] = struct.pack(">III", 0x0000F6D6, 0x00010000, 0x0000D32D)
    return bytes(header) + tag_table


destination = Stream(pdf, icc_profile(b"prtr", b"CMYK", b"OUTI"))
destination.N = 4
destination = pdf.make_indirect(destination)
pdf.Root.OutputIntents = Array(
    [
        pdf.make_indirect(
            Dictionary(
                Type=Name.OutputIntent,
                S=Name.GTS_PDFA1,
                OutputConditionIdentifier=String("litho destination"),
                DestOutputProfile=destination,
            )
        )
    ]
)

LATIN = {0x41: "A", 0x42: "B"}
program = _truetype.build([(3, 1, LATIN), (1, 0, LATIN)])
font_file = Stream(pdf, program)
font_file.Length1 = len(program)
FIRST, LAST = 1, 66
font = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.TrueType,
        BaseFont=Name("/LithoRepairSans"),
        FirstChar=FIRST,
        LastChar=LAST,
        Widths=Array([_truetype.ADVANCE_WIDTH] * (LAST - FIRST + 1)),
        FontDescriptor=pdf.make_indirect(
            Dictionary(
                Type=Name.FontDescriptor,
                FontName=Name("/LithoRepairSans"),
                Flags=32,  # non-symbolic, ISO 32000-1, Table 123
                ItalicAngle=0,
                Ascent=800,
                Descent=-200,
                CapHeight=700,
                StemV=80,
                FontBBox=Array([0, -200, 550, 700]),
                FontFile2=pdf.make_indirect(font_file),
            )
        ),
        Encoding=Name.WinAnsiEncoding,
    )
)


def tint(c1):
    """A Type 2 tint transform from one spot component to CMYK."""
    return pdf.make_indirect(
        Dictionary(FunctionType=2, Domain=Array([0, 1]), C0=Array([0, 0, 0, 0]), C1=Array(c1), N=1)
    )


tint_n = Stream(pdf, b"{ pop pop 0 0 0 0 }")
tint_n.FunctionType = 4
tint_n.Domain = Array([0, 1, 0, 1])
tint_n.Range = Array([0, 1, 0, 1, 0, 1, 0, 1])
# The form's space carries the destination profile's bytes in an object of its
# own: test 3 compares profiles by content, and a distinct object keeps the
# form's verdict from folding into the page's.
form_profile = Stream(pdf, icc_profile(b"prtr", b"CMYK", b"OUTI"))
form_profile.N = 4
form = Stream(pdf, b"/CS0 cs 0 0 0 1 scn 0 0 10 10 re f")
form.Type = Name.XObject
form.Subtype = Name.Form
form.BBox = Array([0, 0, 10, 10])
form.Resources = Dictionary(ColorSpace=Dictionary(CS0=Array([Name.ICCBased, pdf.make_indirect(form_profile)])))

root = pdf.Root.Pages
root.MediaBox = Array([0, 0, 612, 792])
root.Resources = Dictionary(
    Font=Dictionary(F1=font),
    ColorSpace=Dictionary(
        CSN=Array([Name.DeviceN, Array([Name("/SpotA"), Name("/SpotB")]), Name.DeviceCMYK, pdf.make_indirect(tint_n)]),
        CS1=Array([Name.Separation, Name("/SpotC"), Name.DeviceCMYK, tint([1, 0, 0, 0])]),
        CS2=Array([Name.Separation, Name("/SpotC"), Name.DeviceCMYK, tint([0, 1, 0, 0])]),
        CS3=Array([Name.ICCBased, destination]),
    ),
    XObject=Dictionary(Fm0=pdf.make_indirect(form)),
)

content = (
    b"q /CSN cs 0.5 0.5 scn 50 50 20 20 re f Q\n"
    b"q /CS1 cs 1 scn 80 50 20 20 re f Q\n"
    b"q /CS2 cs 1 scn 110 50 20 20 re f Q\n"
    b"q /CS3 cs 0 0 0 1 scn 140 50 20 20 re f Q\n"
    b"q 1 0 0 1 170 50 cm /Fm0 Do Q\n"
    b"BT /F1 12 Tf 14 TL 72 700 Td (AB) Tj\n"
    b"2 1 (\x01) \" (AB) Tj\n"
    b"(\x01) ' (BA) Tj ET"
)
page_packet = Stream(
    pdf,
    b'<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d" bytes="512"?>\n'
    b'<x:xmpmeta xmlns:x="adobe:ns:meta/">\n'
    b' <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">\n'
    b'  <rdf:Description rdf:about="" xmlns:xmp="http://ns.adobe.com/xap/1.0/">\n'
    b"   <xmp:CreatorTool>litho corpus fixture</xmp:CreatorTool>\n"
    b"  </rdf:Description>\n"
    b" </rdf:RDF>\n"
    b"</x:xmpmeta>\n"
    b'<?xpacket end="w"?>',
)
page_packet.Type = Name.Metadata
page_packet.Subtype = Name.XML
page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        Parent=root,
        Contents=pdf.make_indirect(Stream(pdf, content)),
        Metadata=pdf.make_indirect(page_packet),
    )
)


def group_page(blending):
    """A page painting DeviceCMYK under a transparency group blending in a
    device space, with resources of its own."""
    return pdf.make_indirect(
        Dictionary(
            Type=Name.Page,
            Parent=root,
            Resources=Dictionary(),
            Group=Dictionary(Type=Name.Group, S=Name.Transparency, CS=blending),
            Contents=pdf.make_indirect(Stream(pdf, b"0 0 0 1 k 50 50 20 20 re f")),
        )
    )


root.Kids = Array([page, group_page(Name.DeviceRGB), group_page(Name.DeviceCMYK)])
root.Count = 3

# The packet ends before the EOL that precedes endstream, which /Length must
# not count (ISO 32000-2, 7.3.8.1).
attach_xmp(pdf, xmp_packet(part=4, rev=2020).rstrip(b"\n"))
save(pdf, "repairleft_4.pdf", min_version="2.0", preserve_pdfa=False, fix_metadata_version=False)
