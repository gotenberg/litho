#!/usr/bin/env python3
"""Build resinherit_2b.pdf: the page attributes a page inherits, at PDF/A-2b.

ISO 32000-1, 7.7.3.4 and Table 30 make /Resources and /MediaBox inheritable: a
page that does not state one takes it from its nearest ancestor in the page
tree that does, whole. A census of every corpus found no page inheriting from
an intermediate /Pages node, only from the root, so a validator that fell back
to the root's dictionary passed the gate while missing every font and colour
space an intermediate node lends.

Each page sits under an intermediate node of its own, and every font and colour
space is a distinct object, so each verdict names the page it comes from:

| Page     | Its /Resources        | Ancestor lends               | Answers                                   |
| -------- | --------------------- | ---------------------------- | ----------------------------------------- |
| `Inter`  | none                  | `F1`, `CS0`, two levels up   | `6.2.11.4.1/1`, `6.2.11.6/2`, `6.2.4.3/2`, `6.2.2/2` |
| `Own`    | `F2` alone            | `F6`, `CS4`                  | `6.2.11.4.1/1` for `F2`                   |
| `Null`   | `null`                | `F3`, `CS2`                  | `6.2.11.4.1/1`, `6.2.4.3/3`, `6.2.2/2`    |
| `Dangle` | `9999 0 R`, no object | `F4`, `CS3`                  | nothing from the ancestor                 |
| `Parent` | none, `/Parent` wrong | `F5`, on the node listing it | `6.2.11.4.1/1`, `6.2.2/2`                 |
| `Box1`, `Box2` | none            | `/MediaBox [0 0 2 2]`        | `6.1.13/11`, once per page                |

Every object a page must not reach carries a defect no reached object does,
so reaching it adds a rule class rather than an instance. `FR`, the root's
font, `F6` and `F4` are TrueType fonts outside the standard 14 with no
/FirstChar (`6.2.11.2/4`). `CS4` and `CS3` are Separations naming `SpotRGB`,
the colorant `CS0` names, over another alternate (`6.2.4.4/2`). Every page
draws with the names its ancestors define: `Inter` with `FR`, which its nearest
ancestor does not define, since that ancestor's dictionary replaces the root's
rather than merging with it; `Own` with `F6` and `CS4`, past the dictionary it
states; `Dangle` with `F4` and `CS3`.

A null entry is absent (ISO 32000-1, 7.3.7), and a reference to a missing
object is a value, which is how veraPDF reads both. `Parent` names the root as
its /Parent while the intermediate node lists it in /Kids: veraPDF inherits
through the node the tree walk came from. `F1` is the one TrueType font with no
/Encoding, which makes the inheritance from an intermediate node a rule class
of its own (`6.2.11.6/2`), not only an instance of the embedding rule the other
pages fire.

qpdf cannot write a null dictionary value, which it drops, nor a reference to a
missing object, so both are planted as placeholder names of the same length and
patched as bytes after the save, which moves no offset. The tree is built by
hand: pikepdf's page list flattens it and pushes inherited attributes onto each
page, which would delete the constructs the fixture exists for.

See ../README.md for the authoritative expected firing set.
"""

import pathlib

from _common import Array, Dictionary, Name, Stream, out_path, pikepdf, save

pdf = pikepdf.new()

TINT_RGB = Dictionary(FunctionType=2, Domain=Array([0, 1]), C0=Array([0, 0, 0]), C1=Array([1, 0, 0]), N=1)
TINT_CMYK = Dictionary(FunctionType=2, Domain=Array([0, 1]), C0=Array([0, 0, 0, 0]), C1=Array([1, 0, 0, 0]), N=1)


def standard_font(base):
    """An unembedded standard font: it fails 6.2.11.4.1 t1 and nothing else."""
    return pdf.make_indirect(Dictionary(Type=Name.Font, Subtype=Name.Type1, BaseFont=Name(base)))


def truetype(name, encoding=True, first=True):
    """An unembedded non-symbolic TrueType font outside the standard 14."""
    d = Dictionary(
        Type=Name.Font,
        Subtype=Name.TrueType,
        BaseFont=Name("/" + name),
        LastChar=65,
        Widths=Array([722]),
        FontDescriptor=pdf.make_indirect(
            Dictionary(
                Type=Name.FontDescriptor,
                FontName=Name("/" + name),
                Flags=32,
                FontBBox=Array([-166, -225, 1000, 931]),
                ItalicAngle=0,
                Ascent=718,
                Descent=-207,
                CapHeight=718,
                StemV=88,
            )
        ),
    )
    if encoding:
        d.Encoding = Name.WinAnsiEncoding
    if first:
        d.FirstChar = 65
    return pdf.make_indirect(d)


def unreached_font(name):
    """A font no page may reach: no /FirstChar (6.2.11.2 t4) if one does."""
    return truetype(name, first=False)


def separation(colorant, alternate, tint):
    """A Separation over a device space, which no OutputIntent covers."""
    return pdf.make_indirect(Array([Name.Separation, Name(colorant), alternate, tint]))


def unreached_space():
    """A colour space no page may reach: SpotRGB over DeviceCMYK, which
    contradicts CS0's SpotRGB over DeviceRGB (6.2.4.4 t2) if one does."""
    return separation("/SpotRGB", Name.DeviceCMYK, TINT_CMYK)


def resources(fonts=None, spaces=None):
    res = Dictionary()
    if fonts:
        res.Font = Dictionary(fonts)
    if spaces:
        res.ColorSpace = Dictionary(spaces)
    return res


def text(name, y):
    return "BT /%s 12 Tf 50 %d Td (A) Tj ET\n" % (name, y)


def fill(name, x):
    return "/%s cs 1 scn %d 0 10 10 re f\n" % (name, x)


def node(parent, extra=None):
    d = Dictionary(Type=Name.Pages, Kids=Array([]), Count=0, Parent=parent)
    for k, v in (extra or {}).items():
        d[k] = v
    return pdf.make_indirect(d)


def page(parent, ops=None, extra=None):
    d = Dictionary(Type=Name.Page, Parent=parent)
    if ops is not None:
        d.Contents = pdf.make_indirect(Stream(pdf, ops.encode("ascii")))
    for k, v in (extra or {}).items():
        d[k] = v
    return pdf.make_indirect(d)


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
root.MediaBox = Array([0, 0, 612, 792])
root.Resources = resources({"/FR": unreached_font("LithoRoot")})

# Inter: the node two levels up lends F1 and CS0; the node between states none.
inter_top = node(root, {"/Resources": resources(
    {"/F1": truetype("LithoInter", encoding=False)},
    {"/CS0": separation("/SpotRGB", Name.DeviceRGB, TINT_RGB)},
)})
inter_mid = node(inter_top)
attach(root, inter_top, 0)
attach(inter_top, inter_mid, 0)
attach(inter_mid, page(inter_mid, text("F1", 700) + text("FR", 680) + fill("CS0", 0)))

# Own: the page states F2 alone, so its ancestor's F6 and CS4 stay out of reach.
own_node = node(root, {"/Resources": resources({"/F6": unreached_font("LithoOwn")}, {"/CS4": unreached_space()})})
attach(root, own_node, 0)
attach(own_node, page(
    own_node,
    text("F2", 700) + text("F6", 680) + fill("CS4", 0),
    {"/Resources": resources({"/F2": standard_font("/Courier")})},
))

# Null: a null /Resources is absent, so the ancestor's F3 and CS2 are inherited.
null_node = node(root, {"/Resources": resources(
    {"/F3": standard_font("/Helvetica-Bold")},
    {"/CS2": separation("/SpotNull", Name.DeviceCMYK, TINT_CMYK)},
)})
attach(root, null_node, 0)
attach(null_node, page(null_node, text("F3", 700) + fill("CS2", 0), {"/Resources": Name("/LithoNullResources")}))

# Dangle: a reference to a missing object is the page's own value, so F4 and
# CS3 are not inherited.
dangle_node = node(root, {"/Resources": resources({"/F4": unreached_font("LithoDangle")}, {"/CS3": unreached_space()})})
attach(root, dangle_node, 0)
attach(dangle_node, page(dangle_node, text("F4", 700) + fill("CS3", 0), {"/Resources": Name("/LithoDangleRes")}))

# Parent: listed by parent_node, while its /Parent names the root.
parent_node = node(root, {"/Resources": resources({"/F5": standard_font("/Helvetica-Oblique")})})
attach(root, parent_node, 0)
attach(parent_node, page(root, text("F5", 700)))

# Box1, Box2: a 2 by 2 media box on their node, below the 3-unit minimum.
box_node = node(root, {"/MediaBox": Array([0, 0, 2, 2])})
attach(root, box_node, 0)
attach(box_node, page(box_node))
attach(box_node, page(box_node))

# Deliberately no /Metadata.
save(pdf, "resinherit_2b.pdf")

path = pathlib.Path(out_path("resinherit_2b.pdf"))
raw = path.read_bytes()
for placeholder, value in ((b"/LithoNullResources", b"null"), (b"/LithoDangleRes", b"9999 0 R")):
    if raw.count(placeholder) != 1:
        raise SystemExit("resinherit_2b: expected exactly one %r" % placeholder)
    raw = raw.replace(placeholder, value.ljust(len(placeholder)))
path.write_bytes(raw)
print("patched the null and dangling /Resources of", path)
