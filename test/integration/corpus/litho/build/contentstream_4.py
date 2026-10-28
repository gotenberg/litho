#!/usr/bin/env python3
"""Build contentstream_4.pdf: content stream operators and resources at PDF/A-4.

The coverage scan showed ISO 19005-4, 6.2.2 unreached. It is the only clause
that judges a content stream as a whole rather than the objects it references:
the operators it uses must be defined, and the resources it names must be
enumerated by a resource dictionary the stream itself carries.

Three defects sit in one page and are independent, which is the masking probe.
Two of them concern resources and would be easy to conflate, so they are
planted so that each is attributable: the page carries no resource dictionary
of its own and inherits one from the page tree, which is test 2, and the stream
also names a font that appears in no dictionary at all, which is test 3. A
check that reports the first resource problem and stops reports one rule where
two are due.

The undefined operator sits inside a `BX`/`EX` pair on purpose. Those operators
bracket a compatibility section, and the clause says explicitly that an
undefined operator is forbidden "even if such operators are bracketed by the
BX/EX compatibility operators", so a check that honours the bracket as an
escape misses it.

The inherited dictionary defines `/F1`, and the stream draws with it as well.
That is the control for test 3: the verdict lands on the name that resolves
nowhere, not on every name the stream uses.

See ../FIXTURES.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, pikepdf, save

pdf = pikepdf.new()

font = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.Type1,
        BaseFont=Name("/Helvetica"),
        Encoding=Name.WinAnsiEncoding,
    )
)

content = (
    # Draws with a font the inherited dictionary defines: the control for t3.
    b"BT /F1 12 Tf 50 720 Td (a defined resource) Tj ET\n"
    # Names a font no dictionary defines (t3).
    b"BT /F99 12 Tf 50 700 Td (an undefined resource) Tj ET\n"
    # An operator ISO 32000-2 does not define, inside a compatibility section,
    # which the clause says is no excuse (t1).
    b"BX /Litho lithoOp EX\n"
)

page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Contents=Stream(pdf, content),
    )
)
pdf.pages.append(pikepdf.Page(page))

# The page carries no /Resources of its own, so it inherits the dictionary from
# the page tree node (t2). pikepdf adds a /Resources to the page on append, so
# it is removed again here.
if "/Resources" in page.keys():
    del page["/Resources"]
pdf.Root.Pages.Resources = Dictionary(Font=Dictionary(F1=font))

# Deliberately no /Metadata.
save(pdf, "contentstream_4.pdf", min_version="2.0")
