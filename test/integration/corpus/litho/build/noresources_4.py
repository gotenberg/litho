#!/usr/bin/env python3
"""Build noresources_4.pdf: page content with no resource dictionary at all.

ISO 32000-2, 7.7.3.3, Table 31 makes /Resources a required, inheritable entry
of a page: a page that needs no resources states an empty dictionary. A page
that states none, under a page tree that states none either, leaves its content
stream with no resource dictionary to associate, which ISO 19005-4, 6.2.2
requires of every content stream. veraPDF fails 6.2.2 t3 once for each such
stream, whatever it paints, and names DeviceGray, the initial colour space it
resolves through the missing dictionary. litho stated nothing for a stream
that names no resource.

| Page | /Contents                          | /Resources        | 6.2.2 t3 |
| ---- | ---------------------------------- | ----------------- | -------- |
| 1    | paints a rectangle in DeviceGray   | none              | once     |
| 2    | an empty stream                    | none              | once     |
| 3    | `q Q`                              | `null`            | once     |
| 4    | `q Q`, one stream shared with 5    | none              | once     |
| 5    | the stream page 4 shows            | none              | none     |
| 6    | `q Q`                              | an empty dict     | none     |
| 7    | none                               | none              | none     |

Page 1 also fails 6.2.4.3 t4 (DeviceGray with no OutputIntent), which veraPDF
still judges on a page with no resources. Page 2 carries a Square annotation
whose appearance stream states no /Resources either: veraPDF gives a form
XObject resources of its own even when it states none, so the appearance fails
nothing. Pages 5 to 7 are the controls: a content stream is judged once however
many pages show it, and a page with an empty dictionary or no content has
nothing to fail.

No stream names a resource, so 6.2.2 t3 reaches the rule set through the
missing dictionaries alone, and a check that misses them drops the rule.

See ../README.md for the authoritative expected firing set.
"""

from _common import (
    Array,
    Dictionary,
    Name,
    Stream,
    String,
    attach_xmp,
    out_path,
    pikepdf,
    save,
    xmp_packet,
)

pdf = pikepdf.new()


def add_page(contents=None, resources=None):
    d = Dictionary(Type=Name.Page, MediaBox=Array([0, 0, 612, 792]))
    if contents is not None:
        d.Contents = contents
    page = pdf.make_indirect(d)
    pdf.pages.append(pikepdf.Page(page))
    # pikepdf adds a /Resources to a page on append; the pages that must state
    # none have it removed again.
    if "/Resources" in page.keys():
        del page["/Resources"]
    if resources is not None:
        page.Resources = resources
    return page


add_page(Stream(pdf, b"0 g 50 700 200 50 re f\n"))

page2 = add_page(Stream(pdf, b""))
appearance = Stream(pdf, b"q Q\n")
appearance.Type = Name.XObject
appearance.Subtype = Name.Form
appearance.BBox = Array([0, 0, 100, 50])
page2.Annots = Array(
    [
        pdf.make_indirect(
            Dictionary(
                Type=Name.Annot,
                Subtype=Name.Square,
                Rect=Array([100, 600, 200, 650]),
                F=4,
                Contents=String("an annotation whose appearance states no resources"),
                AP=Dictionary(N=pdf.make_indirect(appearance)),
            )
        )
    ]
)

# /Resources null is patched in after the save: pikepdf drops a null value.
add_page(Stream(pdf, b"q Q\n"), resources=Name("/LithoNullRes"))

shared = pdf.make_indirect(Stream(pdf, b"q Q\n"))
add_page(shared)
add_page(shared)

add_page(Stream(pdf, b"q Q\n"), resources=Dictionary())

add_page()

attach_xmp(pdf, xmp_packet(part=4, rev=2020))
save(pdf, "noresources_4.pdf", min_version="2.0")

# Same length, so no offset moves.
path = out_path("noresources_4.pdf")
data = open(path, "rb").read()
old, new = b"/Resources /LithoNullRes", b"/Resources null         "
assert data.count(old) == 1
with open(path, "wb") as f:
    f.write(data.replace(old, new))
