#!/usr/bin/env python3
"""Build syntax_1b.pdf: stream syntax, actions and the XMP header at PDF/A-1b.

The coverage scan showed ISO 19005-1 6.1.7 and 6.6.1 unreached between them. The
two are independent axes, which is what makes them worth putting in one file: a
check that abandons a document after the first syntax complaint reports one rule
where four are due.

6.7.5, which forbids `bytes` and `encoding` in the XMP packet header, is not
here. pikepdf reserialises any packet assigned to the catalog, reordering
attributes and rewriting the header, so those two attributes survive neither
authoring nor a length-preserving patch. Reaching that clause needs a
hand-assembled file.

| Construct                                           | Rule      |
| --------------------------------------------------- | --------- |
| a stream whose `/Length` is not its byte count      | `6.1.7/1` |
| a stream keyword followed by a lone CARRIAGE RETURN | `6.1.7/2` |
| a named action other than the four permitted        | `6.6.1/2` |
| a form field carrying an `/A` action                | `6.6.1/3` |

The two 6.1.7 defects sit on separate streams deliberately. A single stream that
both mis-declares its length and breaks its end-of-line would leave the second
condition unproven, because a parser that gives up on the first problem never
reaches it. Each defective stream sits beside the page content stream, which is
well formed and is the control for both.

`6.6.1` splits by object: test 2 judges a named action, so it rides on an
outline item, while test 3 judges a form field, so it needs an AcroForm field
carrying `/A`. Each has a conforming neighbour: a `/NextPage` named action, which
is one of the four the clause permits, and a second field with no action at all.

Both 6.1.7 defects are beyond pikepdf, which computes `/Length` from the data and
emits a conforming end-of-line. They are patched into the saved bytes,
each replacement the same length as what it replaces, so no cross-reference
offset moves. The builder refuses any patch that would change a length.

See ../FIXTURES.md for the authoritative expected firing set.
"""

import re

from _common import Array, Dictionary, Name, Stream, String, out_path, pikepdf, save

pdf = pikepdf.new()

font = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.Type1,
        BaseFont=Name("/Helvetica"),
        Encoding=Name.WinAnsiEncoding,
    )
)

# The two streams that carry the 6.1.7 defects. Each is a Form XObject so the
# page can invoke it, and each is marked with a distinctive payload so the
# byte patches below can find exactly one occurrence.
BAD_LENGTH_MARK = b"% litho bad length marker\n"
BAD_EOL_MARK = b"% litho bad eol marker\n"


def marked_form(mark):
    stream = Stream(pdf, mark + b"0.5 0.5 0.5 rg 0 0 20 20 re f\n")
    stream.Type = Name.XObject
    stream.Subtype = Name.Form
    stream.BBox = Array([0, 0, 20, 20])
    stream.Resources = Dictionary()
    return pdf.make_indirect(stream)


bad_length = marked_form(BAD_LENGTH_MARK)
bad_eol = marked_form(BAD_EOL_MARK)


def appearance():
    ap = Stream(pdf, b"0 0 0 rg 0 0 60 20 re f\n")
    ap.Type = Name.XObject
    ap.Subtype = Name.Form
    ap.BBox = Array([0, 0, 60, 20])
    return pdf.make_indirect(ap)


# 6.6.1 t3: a form field performing an action, beside one that performs none.
field_with_action = pdf.make_indirect(
    Dictionary(
        Type=Name.Annot,
        Subtype=Name.Widget,
        Rect=Array([50, 600, 110, 620]),
        F=4,
        FT=Name.Btn,
        T=String("acts"),
        AP=Dictionary(N=appearance()),
        A=Dictionary(S=Name.Named, N=Name.NextPage),
    )
)
field_without_action = pdf.make_indirect(
    Dictionary(
        Type=Name.Annot,
        Subtype=Name.Widget,
        Rect=Array([50, 560, 110, 580]),
        F=4,
        FT=Name.Btn,
        T=String("inert"),
        AP=Dictionary(N=appearance()),
    )
)

page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(
            Font=Dictionary(F1=font),
            XObject=Dictionary(FmL=bad_length, FmE=bad_eol),
        ),
        Contents=Stream(
            pdf,
            b"BT /F1 12 Tf 50 700 Td (stream syntax and actions) Tj ET\n"
            b"q 1 0 0 1 60 400 cm /FmL Do Q\n"
            b"q 1 0 0 1 120 400 cm /FmE Do Q\n",
        ),
        Annots=Array([field_with_action, field_without_action]),
    )
)
pdf.pages.append(pikepdf.Page(page))

pdf.Root.AcroForm = Dictionary(Fields=Array([field_with_action, field_without_action]))

# 6.6.1 t2: a named action the clause does not permit, beside a permitted one.
outline_bad = pdf.make_indirect(
    Dictionary(
        Title=String("forbidden named action"),
        A=Dictionary(S=Name.Named, N=Name("/GoForward")),
    )
)
outline_ok = pdf.make_indirect(
    Dictionary(
        Title=String("permitted named action"),
        A=Dictionary(S=Name.Named, N=Name.NextPage),
    )
)
outline_bad.Next = outline_ok
outline_ok.Prev = outline_bad
outlines = pdf.make_indirect(
    Dictionary(Type=Name.Outlines, First=outline_bad, Last=outline_ok, Count=2)
)
outline_bad.Parent = outlines
outline_ok.Parent = outlines
pdf.Root.Outlines = outlines

# A conforming XMP packet. It carries a dc:title so the file is not also short
# of one, keeping the verdict about stream syntax and actions.
XMP = """<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about="" xmlns:dc="http://purl.org/dc/elements/1.1/">
   <dc:title><rdf:Alt><rdf:li xml:lang="x-default">syntax</rdf:li></rdf:Alt></dc:title>
  </rdf:Description>
 </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>"""
metadata = Stream(pdf, XMP.encode("utf-8"))
metadata.Type = Name.Metadata
metadata.Subtype = Name.XML
pdf.Root.Metadata = pdf.make_indirect(metadata)

# Deliberately no /OutputIntents.
save(pdf, "syntax_1b.pdf", min_version="1.4")

path = out_path("syntax_1b.pdf")
with open(path, "rb") as fh:
    data = fh.read()


def patch(data, old, new, why):
    """Replace old with new exactly once, refusing any length change."""
    if len(old) != len(new):
        raise SystemExit(f"{why}: patch changes length, offsets would move")
    if data.count(old) != 1:
        raise SystemExit(f"{why}: expected exactly one match, found {data.count(old)}")
    return data.replace(old, new)


# 6.1.7 t1: /Length must equal the byte count that follows. The declared value
# sits immediately before the stream keyword that introduces the marked payload;
# rewriting its digits in place keeps every offset intact. Search from the
# payload rather than for the nearest "<<", which finds the empty /Resources
# dictionary nested inside the same object.
payload = data.index(BAD_LENGTH_MARK)
match = None
for m in re.finditer(rb"/Length (\d+)([^>]*)>>\s*stream\r?\n", data[:payload]):
    match = m
if match is None:
    raise SystemExit("6.1.7 t1: /Length before the marked stream not found")
digits = match.group(1)
wrong = str(int(digits) + 1).encode()
if len(wrong) != len(digits):
    raise SystemExit("6.1.7 t1: differing length would change the digit count")
data = data[: match.start(1)] + wrong + data[match.end(1) :]

# 6.1.7 t2: the stream keyword must be followed by CRLF or a single LINE FEED.
# A lone CARRIAGE RETURN satisfies neither and occupies the same one byte.
payload = data.index(BAD_EOL_MARK)
kw = data.rindex(b"stream\n", 0, payload)
data = data[:kw] + b"stream\r" + data[kw + len(b"stream\n") :]

with open(path, "wb") as fh:
    fh.write(data)
print("patched", path, len(data), "bytes")
