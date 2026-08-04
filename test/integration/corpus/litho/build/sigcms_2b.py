#!/usr/bin/env python3
"""Build sigcms_2b.pdf: one signature per PDF/A-2b digital-signature verdict.

| Field      | `/Contents`                   | `/ByteRange`    | Rule      |
| ---------- | ----------------------------- | --------------- | --------- |
| `sig0`     | one signer, certificate       | covers the file | silent    |
| `sig1`     | one signer, certificate       | 64 bytes short  | `6.4.3/1` |
| `sig2`     | one signer, no certificate    | covers the file | `6.4.3/2` |
| `sig3`     | two signers, certificate      | covers the file | `6.4.3/3` |
| `sigempty` | no `/V` at all                | n/a             | `6.3.3/1` |
| `text`     | a text field, not a signature | n/a             | `6.3.3/1` |

ISO 19005-2, 6.4.3 splits into three tests over two objects. Test 1 asks the
PDSignature whether its `/ByteRange` covers the whole document. Tests 2 and 3
ask the CMS SignedData in `/Contents` whether a signing certificate rides along
and whether there is exactly one SignerInfo. Nothing about the first is visible
from the second, so a checker that reads only `/ByteRange` reports `sig1` and
misses `sig2` and `sig3` entirely.

Each of the three defects sits on its own field with the other two conformant,
and `sig0` is conformant on all three. A verdict attached to the wrong field, or
one derived from the document rather than the signature it belongs to, therefore
answers on `sig0` too and shows up immediately.

The blobs are genuine `openssl cms -sign` output, embedded here rather than
generated, because openssl stamps a signingTime and its output is not
reproducible across runs. They are the same bytes as
`pkg/internal/compliance/testdata/cms_{real,nocert,two}.der`, which is what
veraPDF 1.30.2 was measured against.

`/ByteRange` is written as a ten-digit zero-padded placeholder and patched after
the save against the offset the signature's own `/Contents` landed at. Padding
to a fixed width is what keeps the patch from moving an xref offset. An
unparseable blob would answer tests 2 and 3 together and mask the split, so
every blob here is real CMS.

`sigempty` and `text` carry no `/AP`, which answers 6.3.3 test 1 twice. They are
the fixture's second purpose: a widget annotation is normally merged with its
form field (ISO 32000-1, 12.7.4.1), so it is reached by the page `/Annots` walk
and the `/AcroForm` `/Fields` walk alike, and a checker running both states the
same missing appearance twice. The four signature widgets carry an `/AP` so this
lands on two objects rather than six.

Beyond the forms the file has no `/Metadata` and paints text in an unembedded
Helvetica with no OutputIntent, three further independent defects that keep the
signature verdicts visibly separate from the rest of the document.

See ../README.md for the authoritative expected firing set.
"""

import base64
import os
import re

from _common import Array, Dictionary, Name, Stream, String, out_path, pikepdf

# A ten-digit placeholder leaves the patched value the same width as the written
# one, so no xref offset moves.
PLACEHOLDER = 1000000000

# How far sig1's covered range falls short of the end of the file.
SHORTFALL = 64

# openssl cms -sign -binary -in data -signer c1.pem -inkey k1.pem \
#     -outform DER -nodetach -md sha256
CMS_REAL_B64 = """
MIIFtwYJKoZIhvcNAQcCoIIFqDCCBaQCAQExDTALBglghkgBZQMEAgEwKgYJKoZIhvcNAQcBoB0E
G2xpdGhvIGNvcnB1cyBzaWduZWQgY29udGVudKCCAxswggMXMIIB/6ADAgECAhRo6ToXVQ9CV4YY
y8g/o1LKj79q0DANBgkqhkiG9w0BAQsFADAbMRkwFwYDVQQDDBBsaXRobyBjb3JwdXMgb25lMB4X
DTI2MDgwMzE3NTIxOFoXDTM2MDczMTE3NTIxOFowGzEZMBcGA1UEAwwQbGl0aG8gY29ycHVzIG9u
ZTCCASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBANanVSvizJ/dUXKF+I6Bszg144PFfvpz
fz7QW2T0aJhp9Lc/YpOGn2ZfqYixDk46B9Qgb9hMWe5vDw1SHXbtxp59SVPElIT63PnBc4aGAZcs
dL66iN7Wajj7dc5j37/6gSb8wkJCL0/UWgA12kQcBLDvYhvjbTxZpbVoiuQ7jn83v0YNa2e1V37c
bcRLIci5rhmbCqy0ostvI4tFW8cIggtkOXjDwUwZL/QaNyhCQNkdgOi//APwkGx8YqvdpzSHyLH3
JlTK4WSMiwdKyfpmBnUnCJZKx8X6jmGjMZkv0VuwM497g0O5xw7DoOWVJmi5cpJJbudHFANebdqA
JnmhOHUCAwEAAaNTMFEwHQYDVR0OBBYEFJPDSf1ExwFLMh92cOdgd/2pgdnlMB8GA1UdIwQYMBaA
FJPDSf1ExwFLMh92cOdgd/2pgdnlMA8GA1UdEwEB/wQFMAMBAf8wDQYJKoZIhvcNAQELBQADggEB
AEqC87rbfgXJ6m3xGnK3jHbEGwVEqr4zpBVMbVVbhZA9O1uAyzTzL4hP5CTSUj8N+3Qoi+OMjkhQ
ZKr9evMcUgCt4zQVP35DqrdfQKCcZr094gMONegEIdMc8mJvTgywZVjgxAicV9G02BEHazlHPp3n
Feb+MjdzshQp7uaGpxL+uH6+rkc5YIBFIy7VCjQMtKScV6tMhCnyHLz3Jt+gc3i7YwMPn/5cQDrO
nE/0YuE4eF8LmQgySuSSXf5wFdHm79ayBQl4D4r7kPGLAPCg61gYPZbWntp12ENCaR/PdW6RNGVL
wBs13xaZDqFCSkFzKGF97G7jWxbh38toEhmMEC4xggJDMIICPwIBATAzMBsxGTAXBgNVBAMMEGxp
dGhvIGNvcnB1cyBvbmUCFGjpOhdVD0JXhhjLyD+jUsqPv2rQMAsGCWCGSAFlAwQCAaCB5DAYBgkq
hkiG9w0BCQMxCwYJKoZIhvcNAQcBMBwGCSqGSIb3DQEJBTEPFw0yNjA4MDMxNzUyMThaMC8GCSqG
SIb3DQEJBDEiBCBgc0WS1bme80WXqfhcwsqTnNYz6CuxvfCaFWZy5u7xGDB5BgkqhkiG9w0BCQ8x
bDBqMAsGCWCGSAFlAwQBKjALBglghkgBZQMEARYwCwYJYIZIAWUDBAECMAoGCCqGSIb3DQMHMA4G
CCqGSIb3DQMCAgIAgDANBggqhkiG9w0DAgIBQDAHBgUrDgMCBzANBggqhkiG9w0DAgIBKDANBgkq
hkiG9w0BAQEFAASCAQCtXPtE2WbW6RElaOjv0xxpjVPkx7RNiKl9r2vn9KqHXOrsWXiEZi8uoUon
X69Glg0SDtfAijPKd8PHnhh6BHjIhc43hq+PhrXZlWOPgiLGooaPc1vXaI4bto5KvlPlpzXsR6bS
PGxnjUpXnaWcGRjeFWkHTFUJXAQL1by3xEDE9odSUrAou3eF6Q501HbN4gCvjgd+hERviWu/ReP5
EQd05+c+Sho67c2eCeU+Ru5zG0+jg9lxbZMHoELFTt8clHhJdCmEiwKRqn5vyPQ9ELexN3cyjxnY
cOSc915k1Halr7E6dsoslFMImJc9W7/aT1DL3kkSXgMzVZmOX70ozg1A
"""

# The same, with -nocerts.
CMS_NOCERT_B64 = """
MIICmAYJKoZIhvcNAQcCoIICiTCCAoUCAQExDTALBglghkgBZQMEAgEwKgYJKoZIhvcNAQcBoB0E
G2xpdGhvIGNvcnB1cyBzaWduZWQgY29udGVudDGCAkMwggI/AgEBMDMwGzEZMBcGA1UEAwwQbGl0
aG8gY29ycHVzIG9uZQIUaOk6F1UPQleGGMvIP6NSyo+/atAwCwYJYIZIAWUDBAIBoIHkMBgGCSqG
SIb3DQEJAzELBgkqhkiG9w0BBwEwHAYJKoZIhvcNAQkFMQ8XDTI2MDgwMzE3NTIxOFowLwYJKoZI
hvcNAQkEMSIEIGBzRZLVuZ7zRZep+FzCypOc1jPoK7G98JoVZnLm7vEYMHkGCSqGSIb3DQEJDzFs
MGowCwYJYIZIAWUDBAEqMAsGCWCGSAFlAwQBFjALBglghkgBZQMEAQIwCgYIKoZIhvcNAwcwDgYI
KoZIhvcNAwICAgCAMA0GCCqGSIb3DQMCAgFAMAcGBSsOAwIHMA0GCCqGSIb3DQMCAgEoMA0GCSqG
SIb3DQEBAQUABIIBAK1c+0TZZtbpESVo6O/THGmNU+THtE2IqX2va+f0qodc6uxZeIRmLy6hSidf
r0aWDRIO18CKM8p3w8eeGHoEeMiFzjeGr4+GtdmVY4+CIsaiho9zW9dojhu2jkq+U+WnNexHptI8
bGeNSledpZwZGN4VaQdMVQlcBAvVvLfEQMT2h1JSsCi7d4XpDnTUds3iAK+OB36ERG+Ja79F4/kR
B3Tn5z5KGjrtzZ4J5T5G7nMbT6OD2XFtkwegQsVO3xyUeEl0KYSLApGqfm/I9D0Qt7E3dzKPGdhw
5Jz3XmTUdqWvsTp2yiyUUwiYlz1bv9pPUMveSRJeAzNVmY5fvSjODUA=
"""

# The same, with a second -signer/-inkey pair.
CMS_TWO_B64 = """
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
"""


def blob(text):
    """Decode one of the embedded CMS SignedData blobs."""
    return base64.b64decode("".join(text.split()))


pdf = pikepdf.new()

font = pdf.make_indirect(Dictionary(
    Type=Name.Font, Subtype=Name.Type1,
    BaseFont=Name("/Helvetica"), Encoding=Name.WinAnsiEncoding))


def appearance():
    """An empty /AP /N form, so 6.3.3 answers only for the two fields without one."""
    stream = Stream(pdf, b"")
    stream.Type = Name.XObject
    stream.Subtype = Name.Form
    stream.BBox = Array([0, 0, 150, 20])
    stream.Resources = Dictionary()
    return Dictionary(N=pdf.make_indirect(stream))


def widget(rect, label):
    """One merged field/widget dictionary, per ISO 32000-1, 12.7.4.1."""
    return Dictionary(Type=Name.Annot, Subtype=Name.Widget,
                      Rect=Array(rect), F=132, T=String(label))


def signature(label, der):
    """A signature value carrying a real CMS blob and a patchable /ByteRange."""
    return pdf.make_indirect(Dictionary(
        Type=Name.Sig,
        Filter=Name("/Adobe.PPKLite"),
        SubFilter=Name("/adbe.pkcs7.detached"),
        Name=String(label),
        ByteRange=Array([0, PLACEHOLDER, PLACEHOLDER, PLACEHOLDER]),
        Contents=pikepdf.Object.parse(b"<" + der.hex().upper().encode() + b">"),
    ))


# short marks the field whose covered range stops before the end of the file.
SIGNED = [
    ("sig0", CMS_REAL_B64, False),
    ("sig1", CMS_REAL_B64, True),
    ("sig2", CMS_NOCERT_B64, False),
    ("sig3", CMS_TWO_B64, False),
]

fields = []
y = 700
for label, der_b64, _ in SIGNED:
    w = widget([50, y, 200, y + 20], label)
    w[Name("/FT")] = Name.Sig
    w[Name("/V")] = signature(label, blob(der_b64))
    w[Name("/AP")] = appearance()
    fields.append(pdf.make_indirect(w))
    y -= 40

# A signature field with no /V: there is no CMS blob to read, so neither
# 6.4.3 test 2 nor test 3 has an object to answer for.
w = widget([50, y, 200, y + 20], "sigempty")
w[Name("/FT")] = Name.Sig
fields.append(pdf.make_indirect(w))
y -= 40

# A text field, so a checker reading every /AcroForm field as a signature
# reports a fourth signature verdict that has no signature behind it.
w = widget([50, y, 200, y + 20], "text")
w[Name("/FT")] = Name.Tx
w[Name("/V")] = String("not a signature")
fields.append(pdf.make_indirect(w))

page = pdf.make_indirect(Dictionary(
    Type=Name.Page,
    MediaBox=Array([0, 0, 612, 792]),
    Resources=Dictionary(Font=Dictionary(F1=font)),
    Contents=Stream(pdf, b"BT /F1 12 Tf 50 750 Td (four signatures) Tj ET\n"),
    Annots=Array(fields)))
pdf.pages.append(pikepdf.Page(page))

pdf.Root.AcroForm = Dictionary(
    Fields=Array(fields), SigFlags=3,
    DA=String("/Helv 0 Tf 0 g"), DR=Dictionary(Font=Dictionary(Helv=font)))

# Deliberately no /Metadata.
path = out_path("sigcms_2b.pdf")
pdf.save(path, min_version="1.7", deterministic_id=True, compress_streams=False,
         stream_decode_level=pikepdf.StreamDecodeLevel.none)

# Patch each /ByteRange against where its own /Contents landed. A signature
# cannot know its offset before the file is laid out, so this is the only
# order that yields exact arithmetic.
data = bytearray(open(path, "rb").read())
total = len(data)
short = {label: is_short for label, _, is_short in SIGNED}
patched_count = 0

for obj in re.finditer(rb"\d+ 0 obj(.*?)endobj", bytes(data), re.S):
    body = obj.group(1)
    if b"/Type /Sig" not in body:
        continue
    base = obj.start(1)
    br = re.search(rb"/ByteRange \[ 0 %d %d %d \]" % (PLACEHOLDER, PLACEHOLDER, PLACEHOLDER), body)
    contents = re.search(rb"/Contents <[0-9a-fA-F]*>", body)
    name = re.search(rb"/Name \(([^)]*)\)", body)
    if not (br and contents and name):
        raise SystemExit("signature object %s is not in the expected shape" % obj.group(0)[:20])
    label = name.group(1).decode()
    start = base + contents.start() + len(b"/Contents ")
    end = base + contents.end()
    tail = total - end - (SHORTFALL if short[label] else 0)
    replacement = b"/ByteRange [ 0 %010d %010d %010d ]" % (start, end, tail)
    if len(replacement) != br.end() - br.start():
        raise SystemExit("patched /ByteRange changes width, which would move an xref offset")
    data[base + br.start():base + br.end()] = replacement
    patched_count += 1

if patched_count != len(SIGNED):
    raise SystemExit("patched %d signatures, expected %d" % (patched_count, len(SIGNED)))

with open(path, "wb") as fh:
    fh.write(bytes(data))
print("wrote", path, os.path.getsize(path), "bytes")
