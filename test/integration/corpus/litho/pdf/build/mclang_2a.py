#!/usr/bin/env python3
"""Build mclang_2a.pdf: marked-content /Lang entries PDF/A level A does not judge.

ISO 19005-2, 6.7.4 holds every /Lang entry, "in the document catalog
dictionary or in a structure element dictionary or property list", to the
language tag grammar. veraPDF 1.30.2 judges the property list of every BDC
operator its content walk reaches, inline or named through /Properties, and
nothing else; each shape below was measured with a crafted file at PDF/A-1a and
PDF/A-2a. litho searched the raw bytes of each page's content for "/Lang" and
read every property list of the page /Properties, and reported the invalid
tags here:

| Where                                              | /Lang   | Rule   |
| -------------------------------------------------- | ------- | ------ |
| inline DP                                          | `3z`    | silent |
| /P1 in /Properties, which no operator names        | `4w`    | silent |
| "/Lang (7t)" inside an /ActualText string          | `7t`    | silent |
| a DP naming /P2, a property list holding `8s`      | `8s`    | silent |
| inline BDC                                         | `en-US` | silent |
| BDC naming /P0, a property list holding `fr`       | `fr`    | silent |

The file is tagged, with a structure tree of one /Document element that owns
no content, so it fails no test of 6.7.4 at all. Its sibling mcform_2a.pdf
carries the shapes veraPDF does judge and litho missed.

See ../FIXTURES.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, String, pikepdf, save, xmp_packet

pdf = pikepdf.new()

content = b"\n".join(
    [
        b"/Span <</Lang (3z)>> DP",
        b"/Span <</ActualText (/Lang (7t))>> BDC EMC",
        b"/Span /P2 DP",
        b"/Span <</Lang (en-US)>> BDC EMC",
        b"/Span /P0 BDC EMC",
    ]
)

page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(
            Properties=Dictionary(
                P0=pdf.make_indirect(Dictionary(Lang=String("fr"))),
                P1=pdf.make_indirect(Dictionary(Lang=String("4w"))),
                P2=pdf.make_indirect(Dictionary(Lang=String("8s"))),
            ),
        ),
        Contents=Stream(pdf, content + b"\n"),
    )
)
pdf.pages.append(pikepdf.Page(page))

root = pdf.make_indirect(Dictionary(Type=Name.StructTreeRoot))
root.K = pdf.make_indirect(Dictionary(Type=Name.StructElem, S=Name.Document, P=root))
pdf.Root.StructTreeRoot = root
pdf.Root.MarkInfo = Dictionary(Marked=True)

metadata = Stream(pdf, xmp_packet(part=2, conformance="A"))
metadata.Type = Name.Metadata
metadata.Subtype = Name.XML
pdf.Root.Metadata = pdf.make_indirect(metadata)

save(pdf, "mclang_2a.pdf", preserve_pdfa=False, fix_metadata_version=False)
