#!/usr/bin/env python3
"""Build pdfxnoprofile_2b.pdf: a PDF/A output intent naming no profile beside
a PDF/X one, at PDF/A-2b.

The catalog lists a GTS_PDFA1 intent carrying an /OutputConditionIdentifier
and no /DestOutputProfile, then a GTS_PDFX intent over a version 2.1 CMYK
printer profile. The page paints DeviceRGB. A GTS_PDFA1 intent covers device
colour through its destination profile (ISO 19005-2, 6.2.4.3), so this one
covers none, and the PDF/X intent never does: every PDF/A level fails the
device colour rule.

The probe is the repair of the PDF/A intent beside the PDF/X one. The PDF/X
profile is a CMYK one, which does not cover DeviceRGB, so the repair needs an
RGB profile. The profile-less GTS_PDFA1 intent is replaced, the PDF/X intent
is kept (ISO 32000-1, 14.11.5), and ISO 19005-2, 6.2.3 t2 asks both intents to
name the new profile object.

See ../README.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, String, attach_xmp, pikepdf, save, xmp_packet
from _icc import cmyk_printer_profile

pdf = pikepdf.new()

profile = Stream(pdf, cmyk_printer_profile())
profile.N = 4

pdf.Root.OutputIntents = Array(
    [
        pdf.make_indirect(
            Dictionary(
                Type=Name.OutputIntent,
                S=Name.GTS_PDFA1,
                OutputConditionIdentifier=String("sRGB IEC61966-2.1"),
                RegistryName=String("http://www.color.org"),
            )
        ),
        pdf.make_indirect(
            Dictionary(
                Type=Name.OutputIntent,
                S=Name.GTS_PDFX,
                OutputConditionIdentifier=String("litho CMYK printer"),
                DestOutputProfile=pdf.make_indirect(profile),
            )
        ),
    ]
)

page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 200, 200]),
        Resources=Dictionary(),
        Contents=pdf.make_indirect(Stream(pdf, b"0.8 0.1 0.1 rg 20 20 60 60 re f\n")),
    )
)
pdf.pages.append(pikepdf.Page(page))

attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "pdfxnoprofile_2b.pdf", min_version="1.7", fix_metadata_version=False)
