#!/usr/bin/env python3
"""Build pdfxonly_2b.pdf: a PDF/X output intent and no PDF/A one, at PDF/A-2b.

The catalog's sole output intent is a GTS_PDFX intent over a version 2.1 CMYK
printer profile, and the page paints a DeviceCMYK square and a DeviceRGB one.
Only a GTS_PDFA1 intent covers device colour under PDF/A (ISO 19005-1,
6.2.3.3; ISO 19005-2/3/4, 6.2.4.3), so every PDF/A level fails the device
colour rule while the intent itself is well formed.

The probe is what remediation does with the PDF/X intent. ISO 32000-1, 14.11.5
admits several intents, so a repair adds a GTS_PDFA1 intent beside it; ISO
19005-2, 6.2.3 t2 asks every intent carrying /DestOutputProfile to name one
indirect object, and the PDF/X profile is one PDF/A admits for CMYK, so the
PDF/A intent can name it and leave the PDF/X intent as it is. A repair that
replaces the whole /OutputIntents array drops the PDF/X intent.

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
                S=Name.GTS_PDFX,
                OutputConditionIdentifier=String("litho CMYK printer"),
                RegistryName=String("http://www.color.org"),
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
        Contents=pdf.make_indirect(
            Stream(pdf, b"0 0.6 1 0 k 20 20 60 60 re f\n0.8 0.1 0.1 rg 120 120 60 60 re f\n")
        ),
    )
)
pdf.pages.append(pikepdf.Page(page))

attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "pdfxonly_2b.pdf", min_version="1.7", fix_metadata_version=False)
