#!/usr/bin/env python3
"""Build gstate_2b.pdf: transfer functions and halftones in graphics states.

The coverage scan showed ISO 19005-2, 6.2.5 unreached. It forbids transfer
functions and constrains halftones, and every one of its tests lives in an
ExtGState dictionary, so the whole clause is authorable without any unusual
content.

Six graphics states are applied on one page and five carry a different
defect, so a check that reports the first offending state and stops reports
one rule where five are due. The sixth is conforming, so over-suppression is
visible too.

See ../README.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, String, pikepdf, save

pdf = pikepdf.new()

font = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.Type1,
        BaseFont=Name("/Helvetica"),
        Encoding=Name.WinAnsiEncoding,
    )
)

# A PostScript calculator function, used as a transfer function below.
transfer = Stream(pdf, b"{ 1 exch sub }")
transfer.FunctionType = 4
transfer.Domain = Array([0, 1])
transfer.Range = Array([0, 1])
transfer = pdf.make_indirect(transfer)

# GS1: /TR is forbidden outright (6.2.5 t1).
gs1 = pdf.make_indirect(Dictionary(Type=Name.ExtGState, TR=transfer))

# GS2: /TR2 is allowed only with the value /Default (t2).
gs2 = pdf.make_indirect(Dictionary(Type=Name.ExtGState, TR2=transfer))

# GS3: /HTP is forbidden outright (t3).
gs3 = pdf.make_indirect(
    Dictionary(Type=Name.ExtGState, HTP=Array([transfer]))
)

# GS4: a halftone must be type 1 or 5; type 6 is not permitted (t4).
gs4 = pdf.make_indirect(
    Dictionary(
        Type=Name.ExtGState,
        HT=pdf.make_indirect(
            Dictionary(
                Type=Name.Halftone,
                HalftoneType=6,
                Width=8,
                Height=8,
                TransferFunction=Name.Identity,
            )
        ),
    )
)

# GS5: a halftone shall not carry /HalftoneName (t5). Type 1 keeps the type
# test satisfied, so the name is the only defect this state contributes.
gs5 = pdf.make_indirect(
    Dictionary(
        Type=Name.ExtGState,
        HT=pdf.make_indirect(
            Dictionary(
                Type=Name.Halftone,
                HalftoneType=1,
                HalftoneName=String("named halftone"),
                Frequency=60,
                Angle=45,
                SpotFunction=Name.Round,
            )
        ),
    )
)

# GS6 conforms: /TR2 set to the one permitted value, a type 1 halftone with
# no name. A firing against it would be a false positive.
gs6 = pdf.make_indirect(
    Dictionary(
        Type=Name.ExtGState,
        TR2=Name.Default,
        HT=pdf.make_indirect(
            Dictionary(
                Type=Name.Halftone,
                HalftoneType=1,
                Frequency=60,
                Angle=45,
                SpotFunction=Name.Round,
            )
        ),
    )
)

content = b"""q /GS1 gs 0 0 0 rg 50 700 60 30 re f Q
q /GS2 gs 50 660 60 30 re f Q
q /GS3 gs 50 620 60 30 re f Q
q /GS4 gs 50 580 60 30 re f Q
q /GS5 gs 50 540 60 30 re f Q
q /GS6 gs 50 500 60 30 re f Q
BT /F1 12 Tf 50 460 Td (graphics states) Tj ET
"""

page = Dictionary(
    Type=Name.Page,
    MediaBox=Array([0, 0, 612, 792]),
    Resources=Dictionary(
        Font=Dictionary(F1=font),
        ExtGState=Dictionary(
            GS1=gs1, GS2=gs2, GS3=gs3, GS4=gs4, GS5=gs5, GS6=gs6
        ),
    ),
    Contents=Stream(pdf, content),
)
pdf.pages.append(pikepdf.Page(pdf.make_indirect(page)))

# Deliberately no /Metadata and no /OutputIntents.
save(pdf, "gstate_2b.pdf", min_version="1.7")
