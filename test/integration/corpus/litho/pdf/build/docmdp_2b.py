#!/usr/bin/env python3
"""Build docmdp_2b.pdf: a certifying signature beside a denormal real.

A coverage scan showed two clusters no fixture reached, neither of them blocked
by anything but authorship:

| Construct                                                            | A-2b spelling |
| -------------------------------------------------------------------- | ------------- |
| a `/Reference` entry carrying `/DigestMethod` under `/Perms /DocMDP` | `6.1.12/2`    |
| a real number too small to be represented                            | `6.1.13/5`    |

The two are independent, which is the masking probe: one lives in the signature
dictionary reached from the catalog `/Perms`, the other in the page content
stream, so a checker that abandons a document after its first structural
verdict reports one rule where two are due.

`6.1.12/2` reads `permsContainDocMDP == false || entries.split('&').filter(elem
=> elem == 'DigestLocation' || elem == 'DigestMethod' || elem == 'DigestValue')
.length == 0`, so it only applies once the catalog carries `/Perms /DocMDP`.
Two `/Reference` entries sit under that DocMDP signature: the first names a
`/DigestMethod`, the second is the control and carries only `/TransformMethod`
and `/TransformParams`. A check that condemns the array rather than the entry
fires on both.

The signature itself is conformant, which is the point: its `/Contents` is the
same genuine `openssl cms -sign` blob `sigcms_2b.pdf` uses, one signer with its
certificate, and its `/ByteRange` is patched after the save to cover the file
exactly. All three tests of ISO 19005-2, 6.4.3 therefore stay silent and the
`6.1.12` verdict is cleanly attributable to the `/Reference` entry.

`6.1.13/5` reads `realValue == 0.0 || (realValue <= -1.175e-38) || (realValue >=
1.175e-38)`, the smallest normal single-precision magnitude. The page paints a
rectangle whose width is `0.000000000000000000000000000000000000000001`, a
denormal, beside one of `1` which is the control. Writing it as a literal
rather than in exponent form matters: ISO 32000-1, 7.3.3 gives PDF reals no
exponent syntax at all.

A third cluster, `6.2.11.2/2` (a font `/Subtype` naming no font type), was
dropped after measurement: veraPDF builds no font object for an unrecognised
subtype, so it reaches neither that rule nor the embedding rules behind it, and
carrying the construct here would bake a permanent rule-set divergence into the
corpus.

The blob is embedded rather than generated: openssl stamps a signingTime, so
shelling out would cost the fixture its byte-for-byte rebuild.

See ../FIXTURES.md for the authoritative expected firing set.
"""

import base64
import os
import re

from _common import Array, Dictionary, Name, Stream, String, out_path, pikepdf

# A ten-digit placeholder leaves the patched value the same width as the written
# one, so no xref offset moves.
PLACEHOLDER = 1000000000

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


def blob():
    """Decode the embedded CMS SignedData blob."""
    return base64.b64decode("".join(CMS_REAL_B64.split()))


pdf = pikepdf.new()

font = pdf.make_indirect(Dictionary(
    Type=Name.Font, Subtype=Name.Type1,
    BaseFont=Name("/Helvetica"), Encoding=Name.WinAnsiEncoding))

# The denormal beside its control. PDF reals have no exponent syntax
# (ISO 32000-1, 7.3.3), so the literal carries every digit.
DENORMAL = b"0.000000000000000000000000000000000000000001"
content = (b"BT /F1 12 Tf 50 700 Td (certifying signature) Tj ET\n"
           b"0 0 0 rg 50 600 " + DENORMAL + b" 20 re f\n"
           b"50 560 1 20 re f\n")

page = pdf.make_indirect(Dictionary(
    Type=Name.Page,
    MediaBox=Array([0, 0, 612, 792]),
    Resources=Dictionary(Font=Dictionary(F1=font)),
    Contents=Stream(pdf, content)))
pdf.pages.append(pikepdf.Page(page))

# The offending /Reference entry beside its control. Only the first carries one
# of the three forbidden digest keys.
transform_params = Dictionary(Type=Name.TransformParams, P=2, V=String("1.2"))
bad_ref = Dictionary(
    Type=Name.SigRef,
    TransformMethod=Name.DocMDP,
    TransformParams=transform_params,
    DigestMethod=Name("/SHA256"))
good_ref = Dictionary(
    Type=Name.SigRef,
    TransformMethod=Name.DocMDP,
    TransformParams=transform_params)

sig = pdf.make_indirect(Dictionary(
    Type=Name.Sig,
    Filter=Name("/Adobe.PPKLite"),
    SubFilter=Name("/adbe.pkcs7.detached"),
    Name=String("docmdp"),
    Reference=Array([bad_ref, good_ref]),
    ByteRange=Array([0, PLACEHOLDER, PLACEHOLDER, PLACEHOLDER]),
    Contents=pikepdf.Object.parse(b"<" + blob().hex().upper().encode() + b">")))

widget = pdf.make_indirect(Dictionary(
    Type=Name.Annot, Subtype=Name.Widget, Rect=Array([50, 500, 200, 520]),
    F=132, T=String("docmdp"), FT=Name.Sig, V=sig,
    AP=Dictionary(N=pdf.make_indirect(Stream(pdf, b"")))))
page.Annots = Array([widget])
pdf.Root.AcroForm = Dictionary(Fields=Array([widget]), SigFlags=3)

# The clause only bites once the catalog certifies the document.
pdf.Root.Perms = Dictionary(DocMDP=sig)

# Deliberately no /Metadata.
path = out_path("docmdp_2b.pdf")
pdf.save(path, min_version="1.7", deterministic_id=True, compress_streams=False,
         stream_decode_level=pikepdf.StreamDecodeLevel.none)

# Patch /ByteRange against where /Contents landed: a signature cannot know its
# own offset before the file is laid out.
data = bytearray(open(path, "rb").read())
total = len(data)
patched = 0
for obj in re.finditer(rb"\d+ 0 obj(.*?)endobj", bytes(data), re.S):
    body = obj.group(1)
    if b"/Type /Sig" not in body:
        continue
    base = obj.start(1)
    br = re.search(rb"/ByteRange \[ 0 %d %d %d \]" % (PLACEHOLDER, PLACEHOLDER, PLACEHOLDER), body)
    contents = re.search(rb"/Contents <[0-9a-fA-F]*>", body)
    if not (br and contents):
        raise SystemExit("signature object is not in the expected shape")
    start = base + contents.start() + len(b"/Contents ")
    end = base + contents.end()
    replacement = b"/ByteRange [ 0 %010d %010d %010d ]" % (start, end, total - end)
    if len(replacement) != br.end() - br.start():
        raise SystemExit("patched /ByteRange changes width, which would move an xref offset")
    data[base + br.start():base + br.end()] = replacement
    patched += 1

if patched != 1:
    raise SystemExit("patched %d signatures, expected 1" % patched)

with open(path, "wb") as fh:
    fh.write(bytes(data))
print("wrote", path, os.path.getsize(path), "bytes")
