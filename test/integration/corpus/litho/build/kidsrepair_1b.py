#!/usr/bin/env python3
"""Build kidsrepair_1b.pdf: defects in the /Resources a page inherits through
the page tree, and in pages written directly in their node's /Kids, at
PDF/A-1b.

ISO 32000-1, 7.7.3.4 hands a page stating no /Resources the dictionary of its
nearest ancestor in the page tree, whole, and veraPDF reads the tree through
/Kids. ISO 32000-1, Table 29 makes each element of /Kids an indirect
reference, yet a page written directly in the array is still a page both tools
judge. The transparency repair wrote an inline graphics state repair through
the page's own /Resources entry, so it dropped every one a page inherits, and
the repairs writing a page, or the /Resources a page states, had no object
number to write a page written directly in /Kids at.

Four pages, each drawing one graphics state with a constant alpha of 0.5:

| Page  | Where it sits                     | Its /Resources | Draws | Lent by |
| ----- | --------------------------------- | -------------- | ----- | ------- |
| `Rt`  | the root's /Kids                  | none           | `GSR` | root    |
| `Mis` | node M's /Kids, /Parent the root  | none           | `GSM` | node M  |
| `Kid` | written directly in node K's /Kids| none           | `GSK` | node K  |
| `Own` | written directly in node K's /Kids| its own        | `GSO` | itself  |

`GSO` also carries a transfer function, and `Own` a transparency page group.
Every graphics state is written directly in its /ExtGState, and every
/ExtGState directly in its /Resources, so a repair can only be written through
the page tree node holding them. The tree is built by hand: pikepdf's page list
flattens it and pushes inherited attributes onto each page, which would delete
the construct the fixture exists for.

See ../FIXTURES.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, attach_xmp, pikepdf, save, xmp_packet

pdf = pikepdf.new()


def alpha(**extra):
    """A graphics state with a constant stroke alpha of 0.5."""
    return Dictionary(Type=Name.ExtGState, CA=0.5, **extra)


def content(ops):
    return pdf.make_indirect(Stream(pdf, ops.encode("ascii")))


def node(parent, resources):
    return pdf.make_indirect(Dictionary(Type=Name.Pages, Kids=Array([]), Count=0, Parent=parent, Resources=resources))


def attach(parent, kid, pages=1):
    """Append kid to parent's /Kids and add its pages to every /Count above."""
    parent.Kids.append(kid)
    n = parent
    while True:
        n.Count = int(n.Count) + pages
        if "/Parent" not in n:
            break
        n = n.Parent


root = pdf.Root.Pages
root.MediaBox = Array([0, 0, 200, 200])
root.Resources = Dictionary(ExtGState=Dictionary(GSR=alpha()))

# Rt: states no /Resources, so it draws with the root's.
attach(root, pdf.make_indirect(Dictionary(Type=Name.Page, Parent=root, Contents=content("q /GSR gs Q\n"))))

# Mis: listed by node M while its /Parent names the root.
m = node(root, Dictionary(ExtGState=Dictionary(GSM=alpha())))
attach(root, m, 0)
attach(m, pdf.make_indirect(Dictionary(Type=Name.Page, Parent=root, Contents=content("q /GSM gs Q\n"))))

# Kid and Own: written directly in node K's /Kids. Kid states no /Resources
# and draws with K's; Own states its own, with a transfer function (ISO
# 19005-1, 6.2.8) beside the alpha, and a transparency group (ISO 19005-1,
# 6.4).
k = node(root, Dictionary(ExtGState=Dictionary(GSK=alpha())))
attach(root, k, 0)
attach(k, Dictionary(Type=Name.Page, Parent=k, Contents=content("q /GSK gs Q\n")))
attach(
    k,
    Dictionary(
        Type=Name.Page,
        Parent=k,
        Resources=Dictionary(ExtGState=Dictionary(GSO=alpha(TR=Name.Identity))),
        Group=Dictionary(Type=Name.Group, S=Name.Transparency),
        Contents=content("q /GSO gs Q\n"),
    ),
)

attach_xmp(pdf, xmp_packet(part=1, conformance="B"))
save(pdf, "kidsrepair_1b.pdf", min_version="1.4")
