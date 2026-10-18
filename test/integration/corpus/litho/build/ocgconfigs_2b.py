#!/usr/bin/env python3
"""Build ocgconfigs_2b.pdf: an /Order left incomplete in /Configs alone.

ISO 19005-2, 6.9 t3 and ISO 19005-4, 6.10 t3 require the /Order array of any
optional content configuration dictionary to reference every OCG in the file.
veraPDF tests it on each configuration, the default /D and every /Configs
entry (ISO 32000-1, Table 100). The one corpus file with an incomplete /Configs
/Order, complex_ua2.pdf, also fails /D, so a check reading /D alone reported
the rule there anyway and nothing isolated the alternate configurations.

Two OCGs, "Shown" and "Hidden", and four configurations, each named:

| Configuration      | /Order                         | Verdict           |
| ------------------ | ------------------------------ | ----------------- |
| /D "Default"       | both OCGs                      | silent            |
| /Configs[0] direct | "Shown" alone                  | 6.9 t3            |
| /Configs[1] direct | both, inside a labelled group  | silent, a control |
| /Configs[2] object | empty                          | 6.9 t3            |

The page draws nothing and the XMP packet claims PDF/A-2b. Every count was
confirmed with live veraPDF 1.30.2. See ../README.md for the authoritative
expected firing set.
"""

from _common import Array, Dictionary, Name, String, attach_xmp, pikepdf, save, xmp_packet

pdf = pikepdf.new()
pdf.add_blank_page(page_size=(612, 792))

shown = pdf.make_indirect(Dictionary(Type=Name.OCG, Name=String("Shown")))
hidden = pdf.make_indirect(Dictionary(Type=Name.OCG, Name=String("Hidden")))
empty = pdf.make_indirect(Dictionary(Name=String("Empty"), Order=Array([])))

pdf.Root.OCProperties = Dictionary(
    OCGs=Array([shown, hidden]),
    D=Dictionary(Name=String("Default"), Order=Array([shown, hidden]), OFF=Array([hidden])),
    Configs=Array(
        [
            Dictionary(Name=String("Partial"), Order=Array([shown])),
            Dictionary(Name=String("Grouped"), Order=Array([Array([String("Layers"), shown, hidden])])),
            empty,
        ]
    ),
)
attach_xmp(pdf, xmp_packet(part=2, conformance="B"))

save(pdf, "ocgconfigs_2b.pdf")
