#!/usr/bin/env python3
"""Build docsync_1b.pdf: document information against XMP under PDF/A-1b.

ISO 19005-1, 6.7.3 compares eight document information dictionary entries with
their XMP analogues. Every test reads `Key == null || Key == XMPKey`, so an entry
that is present must have a matching XMP property, and the interesting question
is what "matching" means. Four different answers are wrong in four different
ways, and each one was a real litho defect found against veraPDF:

| Info entry     | XMP side                              | Rule      |
| -------------- | ------------------------------------- | --------- |
| `/Title`       | `dc:title` written as an attribute    | `6.7.3/2` |
| `/Keywords`    | empty, `pdf:Keywords` undeclared      | `6.7.3/5` |
| `/Creator`     | `xmp:CreatorTool` with another value  | `6.7.3/6` |
| `/Producer`    | `pdf:Producer` absent from the packet | `6.7.3/7` |
| `/ModDate`     | an hour away from `xmp:ModifyDate`    | `6.7.3/8` |
| `/CreationDate` | `xmp:CreateDate`, same instant, other zone | silent |
| `/Author`      | `dc:creator` in an `rdf:Seq`, equal   | silent    |
| `/Subject`     | empty, `dc:description` declared empty | silent   |

Five defects and three controls sit on eight independent entries, which is the
masking probe: the clause splits one requirement across eight tests, so a check
that reports the first mismatch and stops reports one rule where five are due.

The three controls are the point of the file. Each is the exact shape a naive
comparison reports:

`/CreationDate` is `D:20220406201541+02'00'` against `2022-04-06T13:15:41-05:00`.
Those name the same instant, 18:15:41 UTC. The clause asks whether the two are
equivalent, not whether they are spelled alike, so comparing normalized local
wall-clock strings reports a mismatch that veraPDF does not.

`/Subject` is present but empty, and `dc:description` is declared with an empty
value. An empty string is not null, so the entry must still be compared; it
matches, because the comparison turns on whether the XMP property is DECLARED,
not on whether it carries text. `/Keywords` beside it is the same empty Info
entry whose property is *not* declared, and that one fails.

`/Author` uses `dc:creator` in an `rdf:Seq`. `/Title` uses `dc:title` written as
an attribute of `rdf:Description`. Both spellings look equal to a comparison that
reads a property's text, but `dc:title` and `dc:creator` are container-typed in
XMP, an `rdf:Alt` and an `rdf:Seq`, and veraPDF resolves them through that
container. An attribute is a simple value and answers nothing.

`pdf:Producer` is deliberately absent rather than mismatched, so the file also
covers the plainest failure: an Info entry with no XMP counterpart at all.

Beyond the clause the file carries no `/OutputIntents`, an independent defect, so
a synchronization verdict is visibly separate from it.

See ../FIXTURES.md for the authoritative expected firing set.
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

page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(Font=Dictionary(F1=font)),
        Contents=Stream(pdf, b"BT /F1 12 Tf 50 700 Td (docinfo and XMP) Tj ET\n"),
    )
)
pdf.pages.append(pikepdf.Page(page))

# The document information dictionary. Every entry is present, so every test has
# something to compare; /Subject and /Keywords are present but empty, which is
# still present.
pdf.docinfo = pdf.make_indirect(
    Dictionary(
        CreationDate=String("D:20220406201541+02'00'"),
        ModDate=String("D:20220406201541+02'00'"),
        Title=String("Synchronized Title"),
        Author=String("Ada Lovelace"),
        Subject=String(""),
        Keywords=String(""),
        Creator=String("litho corpus"),
        Producer=String("litho"),
    )
)

# The XMP packet. dc:title rides as an attribute of the second rdf:Description
# on purpose: it is a container-typed property, so an attribute does not answer
# the lookup even though its text equals /Title.
XMP = """<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about=""
    xmlns:xmp="http://ns.adobe.com/xap/1.0/"
    xmlns:dc="http://purl.org/dc/elements/1.1/">
   <!-- The same instant as /CreationDate, in another timezone: equivalent. -->
   <xmp:CreateDate>2022-04-06T13:15:41-05:00</xmp:CreateDate>
   <!-- An hour later than /ModDate: not equivalent. -->
   <xmp:ModifyDate>2022-04-06T19:15:41+02:00</xmp:ModifyDate>
   <!-- A different value from /Creator. -->
   <xmp:CreatorTool>another tool</xmp:CreatorTool>
   <!-- dc:creator in its rdf:Seq container, equal to /Author. -->
   <dc:creator><rdf:Seq><rdf:li>Ada Lovelace</rdf:li></rdf:Seq></dc:creator>
   <!-- dc:description declared and empty, like /Subject. -->
   <dc:description><rdf:Alt><rdf:li xml:lang="x-default"></rdf:li></rdf:Alt></dc:description>
  </rdf:Description>
  <!-- dc:title as an attribute: a simple value where an rdf:Alt is required. -->
  <rdf:Description rdf:about="" xmlns:dc="http://purl.org/dc/elements/1.1/"
    dc:title="Synchronized Title"/>
 </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>"""

metadata = Stream(pdf, XMP.encode("utf-8"))
metadata.Type = Name.Metadata
metadata.Subtype = Name.XML
pdf.Root.Metadata = pdf.make_indirect(metadata)

# Deliberately no /OutputIntents.
save(pdf, "docsync_1b.pdf", min_version="1.4")
