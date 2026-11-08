#!/usr/bin/env python3
"""Build fontembed_2b.pdf: unembedded fonts a caller-supplied program can
embed, in every place a font dictionary sits, and two a program cannot serve.

Every font shows text and embeds no program, so each fails ISO 19005-2,
6.2.11.4.1 t1 once. Remediated without a font provider, all nine stay in the
residual. pkg/pdf's TestFontEmbedFixture remediates the file with synthetic
programs built at test time (no font program is committed) and pins what each
font becomes:

  HV  Helvetica, a standard font with no /Widths, indirect, shows "Hello":
      embedded over a TrueType program, with the widths its metrics give.
  TT  LithoSans, a TrueType font under WinAnsiEncoding, indirect: embedded.
  CF  LithoSerif, a non-symbolic Type 1 font: embedded over a CFF program as
      /FontFile3 /Type1C.
  IN  Helvetica-Bold written inline in the page's direct /Font dictionary:
      embedded at a new object number the page's /Font names.
  FI  LithoSans again, written inline in the direct /Resources of form X1:
      embedded, the form's dictionary rewritten around its bytes.
  SH  Helvetica-Oblique written inline in a /Resources dictionary pages 2 and
      3 share as one object: embedded once, reached from both pages.
  JP  LithoMincho, a Type0 font over an unembedded Adobe-Japan1 CIDFontType0
      under UniJIS-UCS2-H, shows U+3042: its descendant becomes a
      CIDFontType2 with a /CIDToGIDMap.
  NO  LithoMissing, a TrueType font no program is supplied for: refused.
  WD  LithoWide, a TrueType font declaring 900 where the program advances
      667: refused, since embedding it would move its text.

See ../FIXTURES.md for the authoritative expected firing set.
"""

from _common import Array, Dictionary, Name, Stream, pikepdf, save

pdf = pikepdf.new()


def descriptor(name, flags):
    return Dictionary(
        Type=Name.FontDescriptor,
        FontName=Name(name),
        Flags=flags,
        FontBBox=Array([0, -200, 1000, 800]),
        ItalicAngle=0,
        Ascent=800,
        Descent=-200,
        CapHeight=700,
        StemV=80,
    )


def truetype(name, widths):
    return Dictionary(
        Type=Name.Font,
        Subtype=Name.TrueType,
        BaseFont=Name(name),
        Encoding=Name.WinAnsiEncoding,
        FirstChar=65,
        LastChar=65 + len(widths) - 1,
        Widths=Array(widths),
        FontDescriptor=pdf.make_indirect(descriptor(name, 32)),
    )


def standard(name):
    return Dictionary(Type=Name.Font, Subtype=Name.Type1, BaseFont=Name(name))


hv = pdf.make_indirect(standard("/Helvetica"))
tt = pdf.make_indirect(truetype("/LithoSans", [667, 667]))
cf = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.Type1,
        BaseFont=Name.LithoSerif,
        Encoding=Name.WinAnsiEncoding,
        FirstChar=65,
        LastChar=66,
        Widths=Array([722, 667]),
        FontDescriptor=pdf.make_indirect(descriptor("/LithoSerif", 34)),
    )
)
jp = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.Type0,
        BaseFont=Name.LithoMincho,
        Encoding=Name("/UniJIS-UCS2-H"),
        DescendantFonts=Array(
            [
                pdf.make_indirect(
                    Dictionary(
                        Type=Name.Font,
                        Subtype=Name.CIDFontType0,
                        BaseFont=Name.LithoMincho,
                        CIDSystemInfo=Dictionary(
                            Registry=pikepdf.String("Adobe"),
                            Ordering=pikepdf.String("Japan1"),
                            Supplement=2,
                        ),
                        DW=1000,
                        FontDescriptor=pdf.make_indirect(descriptor("/LithoMincho", 4)),
                    )
                )
            ]
        ),
    )
)
no = pdf.make_indirect(truetype("/LithoMissing", [667, 667]))
wd = pdf.make_indirect(truetype("/LithoWide", [900, 900]))

form_body = b"BT 72 400 Td /FI 12 Tf (AB) Tj ET\n"
form = Stream(pdf, form_body)
form.Type = Name.XObject
form.Subtype = Name.Form
form.BBox = Array([0, 0, 612, 792])
form.Resources = Dictionary(Font=Dictionary(FI=truetype("/LithoSans", [667, 667])))
form = pdf.make_indirect(form)

first = (
    b"BT 72 700 Td\n"
    b"/HV 12 Tf (Hello) Tj\n"
    b"0 -20 Td /TT 12 Tf (AB) Tj\n"
    b"0 -20 Td /CF 12 Tf (AB) Tj\n"
    b"0 -20 Td /IN 12 Tf (AB) Tj\n"
    b"0 -20 Td /JP 12 Tf <3042> Tj\n"
    b"0 -20 Td /NO 12 Tf (AB) Tj\n"
    b"0 -20 Td /WD 12 Tf (AB) Tj\n"
    b"ET\n"
    b"/X1 Do\n"
)
page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(
            Font=Dictionary(HV=hv, TT=tt, CF=cf, IN=standard("/Helvetica-Bold"), JP=jp, NO=no, WD=wd),
            XObject=Dictionary(X1=form),
        ),
        Contents=pdf.make_indirect(Stream(pdf, first)),
    )
)
pdf.pages.append(pikepdf.Page(page))

shared = pdf.make_indirect(Dictionary(Font=Dictionary(SH=standard("/Helvetica-Oblique"))))
for y in (b"700", b"600"):
    body = b"BT 72 " + y + b" Td /SH 12 Tf (AB) Tj ET\n"
    other = pdf.make_indirect(
        Dictionary(
            Type=Name.Page,
            MediaBox=Array([0, 0, 612, 792]),
            Resources=shared,
            Contents=pdf.make_indirect(Stream(pdf, body)),
        )
    )
    pdf.pages.append(pikepdf.Page(other))

# Deliberately no /OutputIntents and no /Metadata.
save(pdf, "fontembed_2b.pdf")
