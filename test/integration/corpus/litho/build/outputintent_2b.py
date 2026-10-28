#!/usr/bin/env python3
"""Build outputintent_2b.pdf: OutputIntent and ICC destination profiles.

The coverage scan showed ISO 19005-2, 6.2.3 unreached across all four part-2
and part-3 profiles. It constrains the output intent that establishes a file's
destination colour space: what the ICC profile itself may declare, how several
intents must relate, and what a PDF/X intent may carry.

Every fixture in the corpus so far omits `/OutputIntents` entirely, which is
what keeps the colour clauses reachable elsewhere. This one is the opposite
case and the only fixture that declares one, so it also exercises the code
paths that only run when a destination profile is present.

The three tests are independent and are planted together, which is the masking
probe: a check that reports the first offending intent and stops reports one
rule where three are due.

| Intent | Construct                                                    | Rule      |
| ------ | ------------------------------------------------------------ | --------- |
| A      | ICC profile whose device class is `scnr`                     | `6.2.3/1` |
| B      | a second intent whose destination profile is another object  | `6.2.3/2` |
| B      | `/S /GTS_PDFX` carrying `/DestOutputProfileRef`              | `6.2.3/3` |

Intent B's profile is well formed, a `prtr` device class over `RGB `, so it is
the control for test 1: the verdict lands on the offending profile alone rather
than on every profile in the array.

The ICC profiles are built here rather than shipped. A destination profile only
has to be structurally sound for these tests, which read the header, so each is
a 128-byte header plus an empty tag table. veraPDF parses profile bytes eagerly
and throws on a placeholder, so the header carries a real size, an `acsp`
signature and a PCS illuminant.

Test 1 is reached through the device class rather than through the data colour
space. Both routes fire it, but a profile declaring `Lab ` makes litho report
ISO 19005-2, 6.2.4.3 t4 against the page's device colour operators while
veraPDF does not, and a fixture is not the place to carry that. See
../FIXTURES.md.

See ../FIXTURES.md for the authoritative expected firing set.
"""

import struct

from _common import Array, Dictionary, Name, Stream, String, pikepdf, save

pdf = pikepdf.new()

# ICC header offsets used below, per ISO 15076-1: 0 size, 8 version, 12 device
# class, 16 data colour space, 20 PCS, 36 'acsp' signature, 68 PCS illuminant.
ICC_HEADER_SIZE = 128


def icc_profile(device_class, colour_space):
    """A structurally minimal ICC profile with the chosen header fields."""
    tag_table = struct.pack(">I", 0)
    header = bytearray(ICC_HEADER_SIZE)
    header[0:4] = struct.pack(">I", ICC_HEADER_SIZE + len(tag_table))
    header[4:8] = b"litho"[:4]
    header[8:12] = struct.pack(">I", 0x02100000)
    header[12:16] = device_class
    header[16:20] = colour_space
    header[20:24] = b"XYZ "
    header[36:40] = b"acsp"
    header[68:80] = struct.pack(">III", 0x0000F6D6, 0x00010000, 0x0000D32D)
    return bytes(header) + tag_table


def destination_profile(device_class, colour_space):
    stream = Stream(pdf, icc_profile(device_class, colour_space))
    stream.N = 3
    return pdf.make_indirect(stream)


font = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.Type1,
        BaseFont=Name("/Helvetica"),
        Encoding=Name.WinAnsiEncoding,
    )
)

page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(Font=Dictionary(F1=font)),
        Contents=Stream(
            pdf,
            b"BT /F1 12 Tf 50 700 Td (output intents) Tj ET\n"
            b"0 0 0 rg 50 650 60 30 re f\n",
        ),
    )
)
pdf.pages.append(pikepdf.Page(page))

# Intent A: a scanner profile, which is neither an output nor a monitor profile.
intent_a = pdf.make_indirect(
    Dictionary(
        Type=Name.OutputIntent,
        S=Name.GTS_PDFA1,
        OutputConditionIdentifier=String("litho scanner profile"),
        DestOutputProfile=destination_profile(b"scnr", b"RGB "),
    )
)

# Intent B: a well-formed profile, but a second and distinct one, and a PDF/X
# intent carrying the reference key that ISO 15930-7 Annex A defines and this
# clause forbids.
intent_b = pdf.make_indirect(
    Dictionary(
        Type=Name.OutputIntent,
        S=Name.GTS_PDFX,
        OutputConditionIdentifier=String("litho printer profile"),
        DestOutputProfile=destination_profile(b"prtr", b"RGB "),
        DestOutputProfileRef=pdf.make_indirect(
            Dictionary(CheckSum=String("litho"))
        ),
    )
)

pdf.Root.OutputIntents = Array([intent_a, intent_b])

# Deliberately no /Metadata.
save(pdf, "outputintent_2b.pdf", min_version="1.7")
