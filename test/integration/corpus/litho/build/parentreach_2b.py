#!/usr/bin/env python3
"""Build parentreach_2b.pdf: defects in the /Resources a page inherits from the
node listing it, while its /Parent names the root, at PDF/A-2b.

ISO 32000-1, 7.7.3.4 hands a page stating no /Resources the dictionary of its
nearest ancestor in the page tree, and veraPDF reads the tree through /Kids, so
a page whose /Parent names another node inherits from the node listing it. The
repairs of the resources a page inherits climbed /Parent, so on such a page
they repaired the root's dictionary and left the one the page draws with as it
was. litho resinherit_2b.pdf has the construct, but its node lends a font,
which no repair touches.

Four pages sit under four intermediate nodes, each /Parent naming the root, and
each node lends the page a defect one repair owns:

| Page | Its node lends                                        | Repair              |
| ---- | ----------------------------------------------------- | ------------------- |
| `Gs` | `GS0`, a graphics state with a transfer function      | graphics            |
| `Dn` | `CSD`, a DeviceN of nine colorants and no /Colorants  | devicen, aggressive |
| `Fm` | `Fm0`, a form stating no /Resources, and `GS1`        | nested-resources    |
| `Lg` | `P1`, a property list whose /Lang is no language tag  | marked-content-lang |

The root lends `GSR`, a conforming graphics state no page draws with. A CMYK
OutputIntent covers the DeviceCMYK alternate. The tree is built by hand:
pikepdf's page list flattens it and pushes inherited attributes onto each page,
which would delete the construct the fixture exists for.

See ../README.md for the authoritative expected firing set.
"""

import struct

from _common import Array, Dictionary, Name, Stream, String, attach_xmp, pikepdf, save, xmp_packet

pdf = pikepdf.new()

ICC_HEADER_SIZE = 128
ICC_VERSION_2_1 = 0x02100000


def icc_profile(device_class, colour_space, tag):
    """A structurally minimal ICC profile, as iccbased_4.py builds it."""
    tag_table = struct.pack(">I", 0)
    header = bytearray(ICC_HEADER_SIZE)
    header[0:4] = struct.pack(">I", ICC_HEADER_SIZE + len(tag_table))
    header[4:8] = tag
    header[8:12] = struct.pack(">I", ICC_VERSION_2_1)
    header[12:16] = device_class
    header[16:20] = colour_space
    header[20:24] = b"XYZ "
    header[36:40] = b"acsp"
    header[68:80] = struct.pack(">III", 0x0000F6D6, 0x00010000, 0x0000D32D)
    return bytes(header) + tag_table


destination = Stream(pdf, icc_profile(b"prtr", b"CMYK", b"OUTI"))
destination.N = 4
pdf.Root.OutputIntents = Array(
    [
        pdf.make_indirect(
            Dictionary(
                Type=Name.OutputIntent,
                S=Name.GTS_PDFA1,
                OutputConditionIdentifier=String("litho destination"),
                DestOutputProfile=pdf.make_indirect(destination),
            )
        )
    ]
)


def node(parent, resources):
    return pdf.make_indirect(Dictionary(Type=Name.Pages, Kids=Array([]), Count=0, Parent=parent, Resources=resources))


def page(parent, ops):
    """A page stating no /Resources, whose /Parent is parent."""
    return pdf.make_indirect(
        Dictionary(Type=Name.Page, Parent=parent, Contents=pdf.make_indirect(Stream(pdf, ops.encode("ascii"))))
    )


def attach(parent, kid, pages=1):
    """Append kid to parent's /Kids and add its pages to every /Count above."""
    parent.Kids.append(kid)
    n = parent
    while True:
        n.Count = int(n.Count) + pages
        if "/Parent" not in n:
            break
        n = n.Parent


def misparented(resources, ops):
    """A node lending resources, listing a page whose /Parent names the root."""
    n = node(root, resources)
    attach(root, n, 0)
    attach(n, page(root, ops))


root = pdf.Root.Pages
root.MediaBox = Array([0, 0, 200, 200])
root.Resources = Dictionary(ExtGState=Dictionary(GSR=Dictionary(Type=Name.ExtGState, LW=1)))

# Gs: ISO 19005-2, 6.2.5 forbids /TR in a graphics state the page sets.
misparented(
    Dictionary(ExtGState=Dictionary(GS0=Dictionary(Type=Name.ExtGState, TR=Name.Identity))),
    "q /GS0 gs Q\n",
)

# Dn: ISO 19005-2, 6.2.4.4 t1 requires /Colorants of a DeviceN naming a spot,
# and ISO 19005-1, 6.1.12 caps it at eight colorants.
tint = pdf.make_indirect(
    Dictionary(FunctionType=2, Domain=Array([0, 1]), C0=Array([0, 0, 0, 0]), C1=Array([0, 0.5, 1, 0]), N=1)
)
spots = Array([Name("/Spot%d" % i) for i in range(1, 10)])
misparented(
    Dictionary(ColorSpace=Dictionary(CSD=Array([Name.DeviceN, spots, Name.DeviceCMYK, tint]))),
    "/CSD cs %s scn 0 0 5 5 re f\n" % " ".join(["1"] * 9),
)

# Fm: the form states no /Resources, so it names GS1 only through the page's
# (ISO 19005-2, 6.2.2 t2).
form = Stream(pdf, b"/GS1 gs")
form.Type = Name.XObject
form.Subtype = Name.Form
form.BBox = Array([0, 0, 20, 20])
misparented(
    Dictionary(
        XObject=Dictionary(Fm0=pdf.make_indirect(form)),
        ExtGState=Dictionary(GS1=Dictionary(Type=Name.ExtGState, LW=2)),
    ),
    "/Fm0 Do\n",
)

# Lg: a marked-content /Lang has to be a language tag (ISO 32000-1, 14.9.2).
misparented(
    Dictionary(Properties=Dictionary(P1=Dictionary(Lang=String("4w")))),
    "/Span /P1 BDC EMC\n",
)

attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "parentreach_2b.pdf", min_version="1.7")
