#!/usr/bin/env python3
"""Build xfatable_ua1.pdf: a dynamic XFA form beside a conforming table.

One ISO 14289-1 rule the coverage scan showed unreached, beside a table that
exists to stay silent:

| Construct                                          | Rule      |
| -------------------------------------------------- | --------- |
| an XFA configuration asking for dynamic rendering  | `7.15/1`  |

`7.15/1` reads `dynamicRender != 'required'`, which is not a PDF key at all: it
lives inside the XFA packet the `/AcroForm` carries, so reaching the rule means
writing the XFA XML rather than setting a dictionary entry.

The table carries a real `/RowSpan` and conforms, so a firing against it is
over-suppression. It is worth carrying because the table geometry checks are the
ones most likely to condemn a legitimate span, and because a document whose only
defect sits in the XFA packet would let a check that stops at the first
structural complaint look correct.

`7.2/41` is deliberately absent. It reads `numberOfColumnWithWrongRowSpan ==
null`, and two constructs were tried without reaching it: a span in an earlier
row SHIFTS the following row's cells rather than colliding with them, so it reads
as a column count mismatch and answers `7.2/42`, which `tablespan_ua2.pdf`
already covers; a span overrunning the last row answers `7.2/43`, the other
branch of that same column-span requirement, and litho reported neither. That
second construct is a real divergence, recorded in the README rather than planted
here, because the grid model it turns on is not yet understood well enough to
plant a fixture on.

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

# One marked-content sequence per table cell, plus the leading paragraph.
CELL_COUNT = 3
content = [b"/P <</MCID 0>> BDC\nBT /F1 12 Tf 50 750 Td (tables and XFA) Tj ET\nEMC\n"]
for mcid in range(1, CELL_COUNT + 1):
    y = 720 - mcid * 20
    content.append(
        b"/TD <</MCID %d>> BDC\nBT /F1 10 Tf 50 %d Td (cell %d) Tj ET\nEMC\n"
        % (mcid, y, mcid)
    )

page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(Font=Dictionary(F1=font)),
        Contents=Stream(pdf, b"".join(content)),
        StructParents=0,
        Tabs=Name.S,
    )
)
pdf.pages.append(pikepdf.Page(page))

struct_root = pdf.make_indirect(Dictionary(Type=Name.StructTreeRoot))
document = pdf.make_indirect(
    Dictionary(
        Type=Name.StructElem, S=Name.Document, P=struct_root, Lang=String("en-US")
    )
)
paragraph = pdf.make_indirect(
    Dictionary(Type=Name.StructElem, S=Name.P, P=document, Pg=page, K=0)
)

kids = [paragraph]


def cell(parent, mcid, row_span=None):
    """One TD element over a marked-content sequence, optionally spanning rows."""
    d = Dictionary(Type=Name.StructElem, S=Name.TD, P=parent, Pg=page, K=mcid)
    if row_span is not None:
        d.A = Dictionary(O=Name.Table, RowSpan=row_span)
    made = pdf.make_indirect(d)
    kids.append(made)
    return made


def table(rows_spec):
    """A Table element over TR rows. rows_spec is a list of cell descriptions."""
    tbl = pdf.make_indirect(
        Dictionary(Type=Name.StructElem, S=Name.Table, P=document, Pg=page)
    )
    rows = []
    for spec in rows_spec:
        tr = pdf.make_indirect(
            Dictionary(Type=Name.StructElem, S=Name.TR, P=tbl, Pg=page)
        )
        tr.K = Array([cell(tr, mcid, span) for mcid, span in spec])
        rows.append(tr)
        kids.append(tr)
    tbl.K = Array(rows)
    kids.append(tbl)
    return tbl


# A conforming table carrying a real /RowSpan: the first column's cell covers
# both rows, and the second row supplies only the cell that span does not cover.
# It is a control rather than a defect, and it is here because the geometry
# checks are the ones most likely to condemn a legitimate span: its silence
# shows they accept one rather than merely ignoring the attribute.
#
# 7.2 t41 is deliberately not planted. It reads
# `numberOfColumnWithWrongRowSpan == null`, and neither construct tried reaches
# it: a span in an earlier row SHIFTS the following row's cells rather than
# colliding with them, so it reads as a column count mismatch and answers t42,
# which `tablespan_ua2.pdf` already covers; and a span overrunning the last row
# answers t43, the other branch of that same column-span requirement. See the
# README for the divergence that second construct exposed.
table_ok = table([[(1, 2), (2, None)], [(3, None)]])

document.K = Array([paragraph, table_ok])
struct_root.K = Array([document])
struct_root.ParentTree = pdf.make_indirect(Dictionary(Nums=Array([0, Array(kids)])))
struct_root.ParentTreeNextKey = 1

pdf.Root.StructTreeRoot = struct_root
pdf.Root.MarkInfo = Dictionary(Marked=True)
pdf.Root.Lang = String("en-US")
pdf.Root.ViewerPreferences = Dictionary(DisplayDocTitle=True)

# 7.15 t1: the XFA configuration packet asks for dynamic rendering. XFA rides as
# an array of alternating packet name and stream, so the configuration can be
# addressed on its own.
XFA_CONFIG = b"""<config xmlns="http://www.xfa.org/schema/xci/3.0/">
 <acrobat>
  <acrobat7>
   <dynamicRender>required</dynamicRender>
  </acrobat7>
 </acrobat>
</config>"""
XFA_TEMPLATE = b"""<template xmlns="http://www.xfa.org/schema/xfa-template/3.0/">
 <subform name="form1"/>
</template>"""

pdf.Root.AcroForm = Dictionary(
    Fields=Array([]),
    XFA=Array(
        [
            String("config"),
            pdf.make_indirect(Stream(pdf, XFA_CONFIG)),
            String("template"),
            pdf.make_indirect(Stream(pdf, XFA_TEMPLATE)),
        ]
    ),
)

XMP = """<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about=""
    xmlns:dc="http://purl.org/dc/elements/1.1/"
    xmlns:pdfuaid="http://www.aiim.org/pdfua/ns/id/">
   <dc:title><rdf:Alt><rdf:li xml:lang="x-default">dynamic XFA and table spans</rdf:li></rdf:Alt></dc:title>
   <pdfuaid:part>1</pdfuaid:part>
  </rdf:Description>
 </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>"""
metadata = Stream(pdf, XMP.encode("utf-8"))
metadata.Type = Name.Metadata
metadata.Subtype = Name.XML
pdf.Root.Metadata = pdf.make_indirect(metadata)

save(pdf, "xfatable_ua1.pdf", min_version="1.7")
