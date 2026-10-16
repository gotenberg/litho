#!/usr/bin/env python3
"""Build siglock_2b.pdf: unsigned signature fields carrying a /Lock or seed
values, at PDF/A-2b.

ISO 32000-1, 12.7.4.5 lets an unsigned signature field state what signing it
does and asks. Its /Lock (Table 233) names the fields the signature turns
read-only, and its seed values (/SV, Table 234) choose or require the
subfilter, the reason and the digest. No corpus file holds an unsigned
signature field with either, so the mutate-sign cell, which signs each
unsigned signature field in a write of its own, reaches every path here:

| Field         | Kind                   | Entry                                                     |
| ------------- | ---------------------- | --------------------------------------------------------- |
| name          | text                   | none                                                      |
| group         | non-terminal           | none                                                      |
| group.first   | text                   | none                                                      |
| group.second  | text                   | none                                                      |
| lockinclude   | signature, unsigned    | /Lock /Include [(name) (group)]                           |
| lockexclude   | signature, unsigned    | /Lock /Exclude [(group.first)]                            |
| lockall       | signature, unsigned    | /Lock /All                                                |
| seedmet       | signature, unsigned    | /SV choosing ETSI.CAdES.detached, requiring a reason and  |
|               |                        | a SHA-256 or SHA-512 digest                               |
| seedunmet     | signature, unsigned    | /SV requiring adbe.x509.rsa_sha1, which litho cannot sign |

seedmet lists adbe.x509.rsa_sha1 first, which litho does not write, so the
subfilter Table 234 picks is the first one the signer supports. Every field is
merged with its widget, printed, on the one page; the signature widgets are of
zero size, the text widgets' appearances paint nothing, and the page shows
nothing. The XMP packet claims PDF/A-2b.

See ../README.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, String, attach_xmp, pikepdf, save, xmp_packet

pdf = pikepdf.new()

page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        Parent=pdf.Root.Pages,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(),
        Contents=pdf.make_indirect(Stream(pdf, b"")),
    )
)
pdf.Root.Pages.Kids = Array([page])
pdf.Root.Pages.Count = 1


def appearance():
    """A 100 by 20 Form XObject appearance that paints nothing."""
    s = Stream(pdf, b"0 0 100 20 re n\n")
    s.Type = Name.XObject
    s.Subtype = Name.Form
    s.BBox = Array([0, 0, 100, 20])
    return pdf.make_indirect(s)


def widget(partial, y=None, **entries):
    """A field merged with its widget: a text widget at y with an appearance,
    or a signature widget of zero size when y is None."""
    d = Dictionary(Type=Name.Annot, Subtype=Name.Widget, T=String(partial), F=4, P=page, **entries)
    if y is None:
        d.Rect = Array([0, 0, 0, 0])
    else:
        d.Rect = Array([72, y, 172, y + 20])
        d.AP = Dictionary(N=appearance())
    return pdf.make_indirect(d)


name = widget("name", 700, FT=Name.Tx, V=String("Ada"))
group = pdf.make_indirect(Dictionary(T=String("group"), FT=Name.Tx))
first = widget("first", 650, Parent=group, V=String("one"))
second = widget("second", 600, Parent=group, V=String("two"))
group.Kids = Array([first, second])


def lock(action, *fields):
    """A signature field lock dictionary (ISO 32000-1, Table 233)."""
    d = Dictionary(Type=Name.SigFieldLock, Action=Name("/" + action))
    if fields:
        d.Fields = Array([String(f) for f in fields])
    return pdf.make_indirect(d)


def seed(ff, **entries):
    """A seed value dictionary (ISO 32000-1, Table 234) of /Ff ff."""
    return pdf.make_indirect(Dictionary(Type=Name.SV, Ff=ff, **entries))


# Table 234, /Ff: bit 2 SubFilter, bit 4 Reasons, bit 7 DigestMethod.
SEED_SUBFILTER, SEED_REASONS, SEED_DIGEST = 1 << 1, 1 << 3, 1 << 6

signatures = [
    widget("lockinclude", FT=Name.Sig, Lock=lock("Include", "name", "group")),
    widget("lockexclude", FT=Name.Sig, Lock=lock("Exclude", "group.first")),
    widget("lockall", FT=Name.Sig, Lock=lock("All")),
    widget(
        "seedmet",
        FT=Name.Sig,
        SV=seed(
            SEED_SUBFILTER | SEED_REASONS | SEED_DIGEST,
            SubFilter=Array(
                [Name("/adbe.x509.rsa_sha1"), Name("/ETSI.CAdES.detached"), Name("/adbe.pkcs7.detached")]
            ),
            Reasons=Array([String("I approve this document")]),
            DigestMethod=Array([Name.SHA256, Name.SHA512]),
        ),
    ),
    widget("seedunmet", FT=Name.Sig, SV=seed(SEED_SUBFILTER, SubFilter=Array([Name("/adbe.x509.rsa_sha1")]))),
]

page.Annots = Array([name, first, second] + signatures)
pdf.Root.AcroForm = Dictionary(Fields=Array([name, group] + signatures))
attach_xmp(pdf, xmp_packet(part=2, conformance="B"))
save(pdf, "siglock_2b.pdf")
