#!/usr/bin/env python3
"""Build oirgb_2b.pdf: device colour images under an RGB output intent.

No corpus file reads DeviceRGB through an output intent that changes its
colours: every RGB destination profile the corpora carry is sRGB, which gives
each colour back as it came. Here the document's output intent holds an RGB
monitor profile of the sRGB colorants over a 1.8 gamma, so a DeviceRGB colour
it characterises is lighter than the same numbers read as sRGB. The litho
corpus runs the Writer mutations, so the redaction of page 1 decodes every
image here.

| Image  | Colour                     | Reading                         |
| ------ | -------------------------- | ------------------------------- |
| `RGB`  | `/DeviceRGB`               | through the output intent       |
| `Idx`  | `[/Indexed /DeviceRGB ..]` | its base through the intent     |
| `Gray` | `/DeviceGray`              | as sRGB grey: the intent is RGB |

ISO 32000-1, 14.11.5 has an output intent's destination profile characterise
the device the document was prepared for, and ISO 19005-2, 6.2.4.3 admits
DeviceRGB into a PDF/A file on the strength of an RGB destination profile and
DeviceGray on the strength of any output intent.

The document has no /Metadata, so the metadata clauses answer from elsewhere.

See ../README.md for the authoritative expected firing set.
"""

import struct

from _common import Array, Dictionary, Name, Stream, String, pikepdf, save

pdf = pikepdf.new()


def s15(v):
    """An s15Fixed16Number (ICC.1:2001-04, 5.3.5)."""
    return struct.pack(">i", round(v * 65536))


def xyz(x, y, z):
    return b"XYZ \x00\x00\x00\x00" + s15(x) + s15(y) + s15(z)


def text(s):
    return b"text\x00\x00\x00\x00" + s.encode("ascii") + b"\x00"


def desc(s):
    """A version 2 textDescriptionType (ICC.1:2001-04, 6.5.17)."""
    ascii_part = s.encode("ascii") + b"\x00"
    return (b"desc\x00\x00\x00\x00" + struct.pack(">I", len(ascii_part)) + ascii_part
            + struct.pack(">II", 0, 0) + struct.pack(">HB", 0, 0) + bytes(67))


def gamma(g):
    """A curveType holding one gamma, as u8Fixed8Number (6.5.3)."""
    return b"curv\x00\x00\x00\x00" + struct.pack(">IH", 1, round(g * 256))


def profile():
    """A version 2.1 RGB monitor profile: sRGB's D50 colorants, 1.8 gamma."""
    tags = [
        (b"desc", desc("litho RGB gamma 1.8")),
        (b"cprt", text("No copyright, use freely")),
        (b"wtpt", xyz(0.9642, 1.0, 0.8249)),
        (b"rXYZ", xyz(0.4361, 0.2225, 0.0139)),
        (b"gXYZ", xyz(0.3851, 0.7169, 0.0971)),
        (b"bXYZ", xyz(0.1431, 0.0606, 0.7141)),
        (b"rTRC", gamma(1.8)),
        (b"gTRC", gamma(1.8)),
        (b"bTRC", gamma(1.8)),
    ]
    offset = 128 + 4 + 12 * len(tags)
    table, body = struct.pack(">I", len(tags)), b""
    for sig, data in tags:
        while len(data) % 4:
            data += b"\x00"
        table += sig + struct.pack(">II", offset + len(body), len(data))
        body += data
    header = bytearray(128)
    header[8:12] = struct.pack(">I", 0x02100000)
    header[12:16] = b"mntr"
    header[16:20] = b"RGB "
    header[20:24] = b"XYZ "
    header[24:36] = struct.pack(">6H", 2026, 1, 1, 0, 0, 0)
    header[36:40] = b"acsp"
    header[68:80] = s15(0.9642) + s15(1.0) + s15(0.8249)
    data = bytes(header) + table + body
    return struct.pack(">I", len(data)) + data[4:]


def image(samples, width, color_space):
    stream = Stream(pdf, samples)
    stream.Type = Name.XObject
    stream.Subtype = Name.Image
    stream.Width = width
    stream.Height = 1
    stream.ColorSpace = color_space
    stream.BitsPerComponent = 8
    return pdf.make_indirect(stream)


ramp = bytes(range(0, 256, 32))
IMAGES = (
    ("RGB", image(bytes(b for v in ramp for b in (v, 255 - v, 128)), len(ramp), Name.DeviceRGB)),
    ("Idx", image(bytes(range(4)), 4,
                  Array([Name.Indexed, Name.DeviceRGB, 3, String(bytes([200, 80, 40, 40, 200, 80, 128, 128, 128, 0, 0, 0]))]))),
    ("Gray", image(ramp, len(ramp), Name.DeviceGray)),
)

xobjects = Dictionary()
lines = []
y = 700
for name, ref in IMAGES:
    xobjects["/" + name] = ref
    lines.append(b"q 80 0 0 40 50 %d cm /%s Do Q" % (y, name.encode()))
    y -= 60

page = pdf.make_indirect(Dictionary(
    Type=Name.Page,
    MediaBox=Array([0, 0, 612, 792]),
    Resources=Dictionary(XObject=xobjects),
    Contents=Stream(pdf, b"\n".join(lines) + b"\n")))
pdf.pages.append(pikepdf.Page(page))

dest = Stream(pdf, profile())
dest.N = 3
pdf.Root.OutputIntents = Array([pdf.make_indirect(Dictionary(
    Type=Name.OutputIntent,
    S=Name.GTS_PDFA1,
    OutputConditionIdentifier=String("litho RGB gamma 1.8"),
    DestOutputProfile=pdf.make_indirect(dest),
))])

# Deliberately no /Metadata.
save(pdf, "oirgb_2b.pdf")
