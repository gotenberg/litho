#!/usr/bin/env python3
"""Build formcs_2b.pdf: the colour space a Form XObject inherits at PDF/A-2b.

ISO 19005-2, 6.2.4.3 forbids a device colour space the OutputIntent does not
cover. Whether a Form XObject uses one is not decidable from the form alone. Per
ISO 32000-1 §8.6.8 Table 74, sc/scn set components within the space cs/CS
already selected and never select one, and per §8.10.2 a form begins in the
graphics state in effect at the invoking Do. A bare `scn` inside a form
therefore paints in whatever space the invoker had selected, which may be an
ICCBased space the clause permits or the initial DeviceGray it does not.

Four forms are invoked from one page, differing only in the state at their Do,
which is the masking probe: the offending form is the third of four, and two of
its neighbours must stay silent. A check that reads a bare `scn` as proof that
some space was selected misses Fm2 entirely; a check that ignores inheritance
condemns Fm0 and Fm1 as well.

| Form | State at its Do                    | Form content   | Rule        |
| ---- | ---------------------------------- | -------------- | ----------- |
| Fm0  | ICCBased selected, still in scope  | bare `scn`     | silent      |
| Fm1  | nothing selected                   | `/CS0 cs` then `scn` | silent |
| Fm2  | ICCBased selected then `Q`-restored | bare `scn`    | `6.2.4.3/4` |
| Fm3  | ICCBased selected, still in scope  | `0 0 0 1 k`    | `6.2.4.3/3` |

Fm2 is the case that needs the restore: its `Do` sits after a `Q` that pops the
scope where the ICCBased space was selected, so the initial DeviceGray is back
in effect even though a `cs` operator appears earlier in the stream. Reading the
stream without honouring q/Q puts Fm2 in the same class as Fm0.

Fm3 is the control in the other direction. It inherits a permitted space and
then names a device space explicitly, so inheritance is no defence: the verdict
belongs to the operator, not to the entry state. It names DeviceCMYK rather than
DeviceGray deliberately, so it answers test 3 instead of test 4 and leaves Fm2
the only source of test 4. Sharing a test with Fm2 would let an implementation
that misses the inherited-state case still report the rule, which is the masking
this corpus exists to prevent.

The file declares no OutputIntent, which is what keeps test 4 reachable at all:
the test is `gOutputCS != null`, so a PDF/A OutputIntent of any destination
colour space permits DeviceGray and an RGB one silences the clause as
thoroughly as a grayscale one.

The page paints its own text inside the ICCBased scope rather than outside it.
Text drawn with no colour space selected is itself a DeviceGray use, and it
would report the clause from the page, masking both form verdicts at rule
granularity. Keeping the page silent leaves Fm2 and Fm3 as the only sources.

Beyond the clause the file carries no /Metadata, an independent defect, so a
colour verdict is visibly separate from it.

See ../FIXTURES.md for the authoritative expected firing set.
"""

import struct

from _common import Array, Dictionary, Name, Stream, pikepdf, save

pdf = pikepdf.new()

ICC_HEADER_SIZE = 128
ICC_VERSION_2_1 = 0x02100000


def icc_profile(colour_space, components, tag):
    """A structurally minimal ICC profile veraPDF will parse.

    veraPDF reads profile bytes eagerly and throws on a placeholder, so the
    header carries a real size, an `acsp` signature and a PCS illuminant.
    """
    tag_table = struct.pack(">I", 0)
    header = bytearray(ICC_HEADER_SIZE)
    header[0:4] = struct.pack(">I", ICC_HEADER_SIZE + len(tag_table))
    header[4:8] = tag
    header[8:12] = struct.pack(">I", ICC_VERSION_2_1)
    header[12:16] = b"mntr"
    header[16:20] = colour_space
    header[20:24] = b"XYZ "
    header[36:40] = b"acsp"
    header[68:80] = struct.pack(">III", 0x0000F6D6, 0x00010000, 0x0000D32D)
    body = bytes(header) + tag_table
    stream = Stream(pdf, body)
    stream.N = components
    return pdf.make_indirect(stream)


# A three-component ICCBased space, which 6.2.4.3 permits regardless of the
# OutputIntent: it is not a device space.
iccbased = pdf.make_indirect(Array([Name.ICCBased, icc_profile(b"RGB ", 3, b"ICCB")]))


def form(content):
    """One Form XObject carrying its own /Resources so /CS0 resolves inside."""
    stream = Stream(pdf, content)
    stream.Type = Name.XObject
    stream.Subtype = Name.Form
    stream.BBox = Array([0, 0, 60, 60])
    stream.Resources = Dictionary(ColorSpace=Dictionary(CS0=iccbased))
    return pdf.make_indirect(stream)


forms = Dictionary(
    # Paints with whatever the invoker selected. Silent when that is ICCBased.
    Fm0=form(b"0.2 0.4 0.6 scn 0 0 40 40 re f\n"),
    # Selects its own permitted space first, so its entry state cannot matter.
    Fm1=form(b"/CS0 cs 0.2 0.4 0.6 scn 0 0 40 40 re f\n"),
    # The same bare scn as Fm0, but invoked after the selection was popped.
    Fm2=form(b"0.2 0.4 0.6 scn 0 0 40 40 re f\n"),
    # Names a device space outright, which no inherited space excuses. CMYK
    # rather than gray so this lands on test 3 and leaves test 4 to Fm2.
    Fm3=form(b"0 0 0 1 k 0 0 40 40 re f\n"),
)

content = (
    # The page text is painted inside the ICCBased scope so the page itself
    # contributes no DeviceGray use to mask the form verdicts.
    b"q /CS0 cs 0.2 0.4 0.6 scn\n"
    b"  BT /F1 12 Tf 50 740 Td (inherited colour spaces) Tj ET\n"
    # Fm0 and Fm3 run inside the scope that selects the ICCBased space.
    b"  q 1 0 0 1 60 640 cm /Fm0 Do Q\n"
    b"  q 1 0 0 1 200 640 cm /Fm3 Do Q\n"
    b"Q\n"
    # Fm1 runs with nothing selected and selects its own space.
    b"q 1 0 0 1 60 560 cm /Fm1 Do Q\n"
    # Fm2 runs after the selection above was popped, so the initial DeviceGray
    # is back in effect and its bare scn paints in it.
    b"q /CS0 cs 0.2 0.4 0.6 scn Q\n"
    b"q 1 0 0 1 200 560 cm /Fm2 Do Q\n"
)

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
        Resources=Dictionary(
            Font=Dictionary(F1=font),
            XObject=forms,
            ColorSpace=Dictionary(CS0=iccbased),
        ),
        Contents=Stream(pdf, content),
    )
)
pdf.pages.append(pikepdf.Page(page))

# Deliberately no /Metadata.
save(pdf, "formcs_2b.pdf", min_version="1.7")
