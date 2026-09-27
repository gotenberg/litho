#!/usr/bin/env python3
"""Build sigtagged_ua1.pdf: a certified, tagged form under the mutations.

ISO 32000-1, 12.8.1: a signature covers the bytes its /ByteRange names. An
incremental update appends after them and keeps it; a full rewrite moves every
one of them, so litho draws each signed widget's appearance into its page and
drops the signature, its field and the /Perms entry naming it. No fixture was
both tagged and signed, so the gate never saw a signature drawn under the Form
element that enclosed its widget, nor a certification dropped from /Perms.

One tagged page shows "AB" as a paragraph and presents three fields, each merged
with its widget, the only kid of its own Form element and carrying a /TU:

  * "approval", a signature field signed by a genuine CMS signature whose
    /ByteRange covers the file, which /Perms /DocMDP names (P 2: filling in
    forms and signing stay permitted), drawing a box and a stroke;
  * "name", a text field whose appearance paints a box;
  * "cosign", an unsigned signature field whose appearance paints a box.

No appearance draws text, so drawing one into the page changes no page text.
The page's text shows only A and B in the conforming embedded TrueType of
ttcmap_2b.pdf. The XMP packet carries dc:title and pdfuaid:part 1.

The signature is made here, deterministically: an RSA PKCS #1 v1.5 signature
over the SHA-256 of the covered bytes, in a detached CMS SignedData with no
signed attributes (RFC 5652, 5.4), so no signing time varies between builds.
The key and its self-signed certificate are throwaway test material generated
once for this fixture. poppler's pdfsig reports the signature valid, its
certificate untrusted.

See ../README.md for the authoritative expected firing set.
"""

import base64
import hashlib
import os
import re

import _truetype
from _common import (
    Array,
    Dictionary,
    Name,
    Stream,
    String,
    attach_xmp,
    out_path,
    pikepdf,
    xmp_packet,
)

# A throwaway RSA-2048 key and its self-signed certificate, CN "litho corpus
# signer", serial 7. Test material only: it signs this fixture and nothing else.
RSA_N = int(
    "eedf10fc2d7718a2f2c17c918e50dc01eb34480ddde2e37c25d976bf11c7e572"
    "6804fe3421a2190a4334e031652ec1914506d0a7dbd96170a18c26e0ed7e27a8"
    "9e4288ca1e0098d30d8c52d9e4b8e24a2fa52e2a3c9186780c363bec11133b2c"
    "e143df2718ae75a6f11eec097afade2389b958aeca09b2e71b3cdca2da86adb2"
    "e2b89f6a06a57b21af9d8985642c76391ce4d4fe3570d65c9b0591b9360a588d"
    "d3c8f051948931a290dd9dff6cebf87de73f876188694efafd51ef54c9382c38"
    "d1d9947a5c6854bc21c30596ee77f2e21a8227d1ac82c6e1cbc6521d66021ade"
    "06807268d57e9fe36dae8e05dfe6015a0bfe5db85c5d643e35e7b270766310d7",
    16,
)
RSA_D = int(
    "2d87108aaff3b21b649f6229aff1536d7e31d1c5ead392be17ccb1e84af04998"
    "1ba14ffe09a70694c12873ce96556a095119b26d3be6dbbe86dbda912652d426"
    "3c3bbc2ca1b945677c7e914ffc5b0cae4d14738f134a3d51e629f413d3515207"
    "04cc55dcf2c455a242f4bcd235c158d3772d7f6c990f6e2cc4313e703fac49b0"
    "9f6615dfbb99f5c16427cd4db48fbe4d47f87cb795e85e59d6eece025a67f94d"
    "4ddfd6f28e80426798a3b7910842665d9998e09fe8f80fe55e37669a63bb5b95"
    "99fba74daa1a9493c93b176cc221dbfe0b004dd35a8dfd11f34c609b91e50123"
    "344f8ee7d82f0b559a483a1c839251a54310d9344506ee4185bc0e76a39e8581",
    16,
)
CERT_B64 = """
MIIDCjCCAfKgAwIBAgIBBzANBgkqhkiG9w0BAQsFADAeMRwwGgYDVQQDDBNsaXRobyBjb3JwdXMg
c2lnbmVyMB4XDTI2MDkyNzA5NTc1NFoXDTM2MDkyNDA5NTc1NFowHjEcMBoGA1UEAwwTbGl0aG8g
Y29ycHVzIHNpZ25lcjCCASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBAO7fEPwtdxii8sF8
kY5Q3AHrNEgN3eLjfCXZdr8Rx+VyaAT+NCGiGQpDNOAxZS7BkUUG0Kfb2WFwoYwm4O1+J6ieQojK
HgCY0w2MUtnkuOJKL6UuKjyRhngMNjvsERM7LOFD3ycYrnWm8R7sCXr63iOJuViuygmy5xs83KLa
hq2y4rifagaleyGvnYmFZCx2ORzk1P41cNZcmwWRuTYKWI3TyPBRlIkxopDdnf9s6/h95z+HYYhp
Tvr9Ue9UyTgsONHZlHpcaFS8IcMFlu538uIagifRrILG4cvGUh1mAhreBoByaNV+n+Ntro4F3+YB
Wgv+XbhcXWQ+NeeycHZjENcCAwEAAaNTMFEwHQYDVR0OBBYEFAt9nfgKsgLU8+94cjIEJuVTkYr5
MB8GA1UdIwQYMBaAFAt9nfgKsgLU8+94cjIEJuVTkYr5MA8GA1UdEwEB/wQFMAMBAf8wDQYJKoZI
hvcNAQELBQADggEBAOxAN5reF10xTWxk2SviBZdCx+CWckajjrnHIq5Gt/g/ZyXGXjQTzjUQi3WB
uMzKTvht8WKvuK4IjrRrOOetjnZz7Pm8uDrf1tpeLl2N8Gx5nOkAwaLNJd3tpVP7bsnBgGiDHZHq
XAhbWuK6CZzjIO3VIQFJJmUUHtT2S4oRHOdOHoxgRfdRupZ8DTWcHOYOBH9vbKcUvQkM+joc8lj4
AhOMVXg/I+AR3nf963OmmsbtENk1QKaLK+Tr3nqT7a+UVXVK7rYWDARkIB7V2XbYFBdPt3lji/ne
YiiYtNyf4jaR19gNs5XDnuwZFDFlbtb2jSuHEGGElLYfXPydwTeEYmc=
"""
ISSUER_B64 = "MB4xHDAaBgNVBAMME2xpdGhvIGNvcnB1cyBzaWduZXI="
SERIAL = 7

# The bytes reserved for the CMS blob in /Contents. The blob is about 1.3 KB.
SIGNATURE_BYTES = 4096

# A ten-digit placeholder leaves the patched value the same width as the written
# one, so no xref offset moves.
PLACEHOLDER = 1000000000


def der(tag, body):
    """One DER element: tag, definite length, body (X.690, 8.1)."""
    n = len(body)
    if n < 0x80:
        length = bytes([n])
    else:
        octets = n.to_bytes((n.bit_length() + 7) // 8, "big")
        length = bytes([0x80 | len(octets)]) + octets
    return bytes([tag]) + length + body


def der_int(value):
    """A DER INTEGER holding a non-negative value."""
    body = value.to_bytes(value.bit_length() // 8 + 1, "big")
    return der(0x02, body)


def der_oid(dotted):
    """A DER OBJECT IDENTIFIER."""
    arcs = [int(a) for a in dotted.split(".")]
    body = bytes([40 * arcs[0] + arcs[1]])
    for arc in arcs[2:]:
        chunk = [arc & 0x7F]
        arc >>= 7
        while arc:
            chunk.append(0x80 | (arc & 0x7F))
            arc >>= 7
        body += bytes(reversed(chunk))
    return der(0x06, body)


def seq(*items):
    return der(0x30, b"".join(items))


def set_of(*items):
    return der(0x31, b"".join(items))


SHA256 = seq(der_oid("2.16.840.1.101.3.4.2.1"))
RSA_ENCRYPTION = seq(der_oid("1.2.840.113549.1.1.1"), b"\x05\x00")


def rsa_sign(digest):
    """RSASSA-PKCS1-v1_5 over a SHA-256 digest (RFC 8017, 8.2.1 and 9.2)."""
    k = (RSA_N.bit_length() + 7) // 8
    # DigestInfo, whose algorithm carries NULL parameters (RFC 8017, 9.2 note 1).
    t = seq(seq(der_oid("2.16.840.1.101.3.4.2.1"), b"\x05\x00"), der(0x04, digest))
    em = b"\x00\x01" + b"\xff" * (k - len(t) - 3) + b"\x00" + t
    return pow(int.from_bytes(em, "big"), RSA_D, RSA_N).to_bytes(k, "big")


def cms_detached(digest):
    """A detached CMS SignedData with one signer and its certificate, no signed
    attributes (RFC 5652, 5.1 and 5.3)."""
    cert = base64.b64decode("".join(CERT_B64.split()))
    issuer = base64.b64decode(ISSUER_B64)
    signer = seq(
        der_int(1),
        seq(issuer, der_int(SERIAL)),
        SHA256,
        RSA_ENCRYPTION,
        der(0x04, rsa_sign(digest)),
    )
    signed_data = seq(
        der_int(1),
        set_of(SHA256),
        seq(der_oid("1.2.840.113549.1.7.1")),
        der(0xA0, cert),
        set_of(signer),
    )
    return seq(der_oid("1.2.840.113549.1.7.2"), der(0xA0, signed_data))


pdf = pikepdf.new()

LATIN = {0x41: "A", 0x42: "B"}
program = _truetype.build([(3, 1, LATIN), (1, 0, LATIN)])
font_file = Stream(pdf, program)
font_file.Length1 = len(program)
descriptor = Dictionary(
    Type=Name.FontDescriptor,
    FontName=Name("/LithoSignedSans"),
    Flags=32,  # non-symbolic, ISO 32000-1, Table 123
    ItalicAngle=0,
    Ascent=800,
    Descent=-200,
    CapHeight=700,
    StemV=80,
    FontBBox=Array([0, -200, 550, 700]),
    FontFile2=pdf.make_indirect(font_file),
)
FIRST, LAST = 0x41, 0x42
font = pdf.make_indirect(
    Dictionary(
        Type=Name.Font,
        Subtype=Name.TrueType,
        BaseFont=Name("/LithoSignedSans"),
        FirstChar=FIRST,
        LastChar=LAST,
        Widths=Array([_truetype.ADVANCE_WIDTH] * (LAST - FIRST + 1)),
        FontDescriptor=pdf.make_indirect(descriptor),
        Encoding=Name.WinAnsiEncoding,
    )
)


def appearance(width, height, content):
    """A Form XObject appearance of the given size painting content."""
    s = Stream(pdf, content)
    s.Type = Name.XObject
    s.Subtype = Name.Form
    s.BBox = Array([0, 0, width, height])
    return pdf.make_indirect(s)


page = pdf.make_indirect(
    Dictionary(
        Type=Name.Page,
        MediaBox=Array([0, 0, 612, 792]),
        Resources=Dictionary(Font=Dictionary(F1=font)),
        Contents=Stream(pdf, b"/P <</MCID 0>> BDC\nBT /F1 12 Tf 50 740 Td (AB) Tj ET\nEMC\n"),
        StructParents=0,
        Tabs=Name.S,
    )
)
pdf.pages.append(pikepdf.Page(page))

signature = pdf.make_indirect(
    Dictionary(
        Type=Name.Sig,
        Filter=Name("/Adobe.PPKLite"),
        SubFilter=Name("/adbe.pkcs7.detached"),
        Name=String("litho corpus signer"),
        Reference=Array(
            [
                Dictionary(
                    Type=Name.SigRef,
                    TransformMethod=Name.DocMDP,
                    TransformParams=Dictionary(Type=Name.TransformParams, P=2, V=Name("/1.2")),
                )
            ]
        ),
        ByteRange=Array([0, PLACEHOLDER, PLACEHOLDER, PLACEHOLDER]),
        Contents=pikepdf.Object.parse(b"<" + b"00" * SIGNATURE_BYTES + b">"),
    )
)


def field(name, ft, rect, ap, struct_parent, value=None):
    """A field merged with its widget, printed, carrying a /TU."""
    d = Dictionary(
        Type=Name.Annot,
        Subtype=Name.Widget,
        Rect=Array(rect),
        F=4,
        P=page,
        FT=Name(ft),
        T=String(name),
        TU=String(name.capitalize()),
        AP=Dictionary(N=ap),
        StructParent=struct_parent,
    )
    if value is not None:
        d.V = value
    return pdf.make_indirect(d)


approval = field(
    "approval",
    "/Sig",
    [300, 700, 500, 750],
    appearance(
        200,
        50,
        b"0.95 g 0 0 200 50 re f\n0 0 0.5 RG 1.5 w 10 15 m 40 45 60 5 90 30 c 120 50 140 10 180 25 c S\n",
    ),
    1,
    signature,
)
name = field("name", "/Tx", [50, 650, 150, 670], appearance(100, 20, b"0.9 g 0 0 100 20 re f\n"), 2, String("Ada"))
cosign = field("cosign", "/Sig", [300, 600, 500, 650], appearance(200, 50, b"0.8 g 0 0 200 50 re f\n"), 3)
widgets = [approval, name, cosign]
page.Annots = Array(widgets)
pdf.Root.AcroForm = Dictionary(Fields=Array(widgets), SigFlags=3)
pdf.Root.Perms = Dictionary(DocMDP=signature)

struct_root = pdf.make_indirect(Dictionary(Type=Name.StructTreeRoot))
document = pdf.make_indirect(Dictionary(Type=Name.StructElem, S=Name.Document, P=struct_root))
paragraph = pdf.make_indirect(Dictionary(Type=Name.StructElem, S=Name.P, P=document, Pg=page, K=0))


def form(widget):
    """A Form element whose only kid is an object reference to widget."""
    return pdf.make_indirect(
        Dictionary(
            Type=Name.StructElem,
            S=Name.Form,
            P=document,
            Pg=page,
            K=Dictionary(Type=Name.OBJR, Obj=widget),
        )
    )


form_approval, form_name, form_cosign = form(approval), form(name), form(cosign)
document.K = Array([paragraph, form_approval, form_name, form_cosign])
struct_root.K = document
struct_root.ParentTree = pdf.make_indirect(
    Dictionary(Nums=Array([0, Array([paragraph]), 1, form_approval, 2, form_name, 3, form_cosign]))
)
struct_root.ParentTreeNextKey = 4

pdf.Root.StructTreeRoot = struct_root
pdf.Root.MarkInfo = Dictionary(Marked=True)
pdf.Root.Lang = String("en-US")
pdf.Root.ViewerPreferences = Dictionary(DisplayDocTitle=True)
attach_xmp(pdf, xmp_packet(ua_part=1))

path = out_path("sigtagged_ua1.pdf")
pdf.save(
    path,
    min_version="1.7",
    deterministic_id=True,
    compress_streams=False,
    stream_decode_level=pikepdf.StreamDecodeLevel.none,
)

# The signature covers the file but its own /Contents, so it is made last: the
# /ByteRange is patched against where /Contents landed, then the covered bytes
# are hashed and signed, and the blob fills the hole. Padding both to a fixed
# width keeps every xref offset where the save put it.
data = bytearray(open(path, "rb").read())
match = None
for obj in re.finditer(rb"\d+ 0 obj(.*?)endobj", bytes(data), re.S):
    if b"/Type /Sig" in obj.group(1):
        if match is not None:
            raise SystemExit("more than one signature dictionary")
        match = obj
if match is None:
    raise SystemExit("no signature dictionary")
body, base = match.group(1), match.start(1)
br = re.search(rb"/ByteRange \[ 0 %d %d %d \]" % (PLACEHOLDER, PLACEHOLDER, PLACEHOLDER), body)
contents = re.search(rb"/Contents <0+>", body)
if not (br and contents):
    raise SystemExit("the signature dictionary is not in the expected shape")
start = base + contents.start() + len(b"/Contents ")
end = base + contents.end()
replacement = b"/ByteRange [ 0 %010d %010d %010d ]" % (start, end, len(data) - end)
if len(replacement) != br.end() - br.start():
    raise SystemExit("the patched /ByteRange changes width, which would move an xref offset")
data[base + br.start() : base + br.end()] = replacement

digest = hashlib.sha256(bytes(data[:start]) + bytes(data[end:])).digest()
blob = cms_detached(digest).hex().encode()
if len(blob) > 2 * SIGNATURE_BYTES:
    raise SystemExit("the CMS blob does not fit its placeholder")
data[start + 1 : end - 1] = blob + b"0" * (2 * SIGNATURE_BYTES - len(blob))

with open(path, "wb") as fh:
    fh.write(bytes(data))
print("wrote", path, os.path.getsize(path), "bytes")
