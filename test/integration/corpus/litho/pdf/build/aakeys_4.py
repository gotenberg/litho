#!/usr/bin/env python3
"""Build aakeys_4.pdf: catalog and page additional-actions keys at PDF/A-4.

ISO 19005-4, 6.6.3 does not forbid an /AA entry on the document catalog or a
page, as parts 1 to 3 do. It restricts the keys of its additional-actions
dictionary to E, X, D, U, Fo and Bl, and veraPDF's test judges each such
dictionary by the keys it holds. A remediation that drops the /AA whole loses
the permitted entries with the forbidden ones, so each dictionary here holds
one of each:

  * the catalog's /AA holds /WC, a trigger 6.6.3 forbids, and /E;
  * page 1's /AA holds /O, a trigger 6.6.3 forbids, and /X;
  * page 2's /AA holds /D alone, the control: nothing to report or remove.

Every action is a GoTo to page 1, a type every PDF/A part permits, so the
action-type rules stay silent and the key restriction stands alone. The
catalog /AA is an object of its own, so a fix that edits it must write it back
rather than a copy of the document's version.

See ../FIXTURES.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, pikepdf, save

pdf = pikepdf.new()

for _ in range(2):
    page = pdf.make_indirect(
        Dictionary(
            Type=Name.Page,
            MediaBox=Array([0, 0, 612, 792]),
            Contents=Stream(pdf, b""),
            Resources=Dictionary(),
        )
    )
    pdf.pages.append(pikepdf.Page(page))

first = pdf.pages[0].obj


def goto():
    """A GoTo action opening page 1 in full."""
    return Dictionary(S=Name.GoTo, D=Array([first, Name.Fit]))


pdf.Root.AA = pdf.make_indirect(Dictionary(WC=goto(), E=goto()))
pdf.pages[0].obj.AA = Dictionary(O=goto(), X=goto())
pdf.pages[1].obj.AA = Dictionary(D=goto())

# Deliberately no /Metadata.
save(pdf, "aakeys_4.pdf", min_version="2.0")
