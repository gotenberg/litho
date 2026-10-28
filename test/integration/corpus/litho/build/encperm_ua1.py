#!/usr/bin/env python3
"""Build encperm_ua1.pdf: an encrypted document that forbids accessible extraction.

| Construct                                          | Rule       |
| -------------------------------------------------- | ---------- |
| `/P` with bit 10 clear in an encrypted document    | `7.16/1`   |

ISO 14289-1, 7.16 test 1 reads `P != null && (P & 512) == 512`, so it judges the
permissions an encrypted document grants: bit 10 is "extract for accessibility",
and a document withholding it denies assistive technology the text.

This is the one fixture written byte by byte rather than through pikepdf. qpdf
never clears bit 10, whatever `pikepdf.Permissions(accessibility=False)` asks
for: measured at revision 4 and revision 6, `/P` comes out -1028 either way, and
`extract=False` clears bit 5 instead. The construct is therefore unreachable
through pikepdf's encryption, so the standard security handler is written here
directly (ISO 32000-1, Algorithms 2, 3 and 5) with V=4, R=4 and a V2 crypt
filter.

Writing it by hand buys back determinism the qpdf path cannot offer: qpdf
refuses `deterministic_id` together with encryption, and `trailer_2b.pdf`
documents what that costs. Here the `/ID` is fixed, so the whole file, keys
included, is reproducible byte for byte.

The metadata stream is encrypted like every other stream. Leaving it in the
clear made the packet unreadable to BOTH engines, which cost the file its title
and its `pdfuaid:part` and buried the permissions rule under three unrelated
ones. That was measured, not assumed.

The document is otherwise conformant apart from its unembedded font, so at
PDF/UA-1 exactly two rules answer: the permissions and `7.21.4.1`. A checker
reading the permissions wrongly shows up immediately rather than behind a
neighbour.

See ../FIXTURES.md for the authoritative expected firing set.
"""

import hashlib
import os
import struct

PDFS_DIR = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "pdfs")

# The 32-byte padding string of ISO 32000-1, 7.6.3.3.
PAD = bytes([
    0x28, 0xBF, 0x4E, 0x5E, 0x4E, 0x75, 0x8A, 0x41, 0x64, 0x00, 0x4E, 0x56,
    0xFF, 0xFA, 0x01, 0x08, 0x2E, 0x2E, 0x00, 0xB6, 0xD0, 0x68, 0x3E, 0x80,
    0x2F, 0x0C, 0xA9, 0xFE, 0x64, 0x53, 0x69, 0x7A,
])

# A fixed file identifier keeps every derived key, and so the whole file,
# byte for byte reproducible.
DOC_ID = bytes.fromhex("6c6974686f636f727075736964656e74")

KEY_LENGTH = 16

# -3904 grants every permission except bit 10 (512), extract for accessibility.
PERMISSIONS = -3904


def rc4(key, data):
    """RC4, the cipher the V2 crypt filter names."""
    state = list(range(256))
    j = 0
    for i in range(256):
        j = (j + state[i] + key[i % len(key)]) & 0xFF
        state[i], state[j] = state[j], state[i]
    out = bytearray()
    i = j = 0
    for byte in data:
        i = (i + 1) & 0xFF
        j = (j + state[i]) & 0xFF
        state[i], state[j] = state[j], state[i]
        out.append(byte ^ state[(state[i] + state[j]) & 0xFF])
    return bytes(out)


def pad_password(password):
    return (password + PAD)[:32]


def owner_value(owner_password, user_password):
    """ISO 32000-1, Algorithm 3."""
    digest = hashlib.md5(pad_password(owner_password or user_password)).digest()
    for _ in range(50):
        digest = hashlib.md5(digest).digest()
    key = digest[:KEY_LENGTH]
    out = rc4(key, pad_password(user_password))
    for i in range(1, 20):
        out = rc4(bytes(b ^ i for b in key), out)
    return out


def file_key(user_password, o_value, permissions):
    """ISO 32000-1, Algorithm 2."""
    digest = hashlib.md5()
    digest.update(pad_password(user_password))
    digest.update(o_value)
    digest.update(struct.pack("<i", permissions))
    digest.update(DOC_ID)
    out = digest.digest()
    for _ in range(50):
        out = hashlib.md5(out[:KEY_LENGTH]).digest()
    return out[:KEY_LENGTH]


def user_value(key):
    """ISO 32000-1, Algorithm 5, for revision 3 and above."""
    out = rc4(key, hashlib.md5(PAD + DOC_ID).digest())
    for i in range(1, 20):
        out = rc4(bytes(b ^ i for b in key), out)
    return out + b"\x00" * 16


def object_key(key, number):
    """The per-object key of ISO 32000-1, Algorithm 1, at generation zero."""
    digest = hashlib.md5()
    digest.update(key)
    digest.update(bytes([number & 0xFF, (number >> 8) & 0xFF, (number >> 16) & 0xFF]))
    digest.update(b"\x00\x00")
    return digest.digest()[:min(len(key) + 5, 16)]


XMP = b"""<?xpacket begin="\xef\xbb\xbf" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about=""
    xmlns:dc="http://purl.org/dc/elements/1.1/"
    xmlns:pdfuaid="http://www.aiim.org/pdfua/ns/id/">
   <dc:title><rdf:Alt><rdf:li xml:lang="x-default">encrypted permissions</rdf:li></rdf:Alt></dc:title>
   <pdfuaid:part>1</pdfuaid:part>
  </rdf:Description>
 </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>"""

CONTENT = b"/P <</MCID 0>> BDC\nBT /F1 12 Tf 50 700 Td (encrypted permissions) Tj ET\nEMC\n"


def build(path, permissions):
    o_value = owner_value(b"owner", b"")
    key = file_key(b"", o_value, permissions)
    u_value = user_value(key)

    def encrypt(number, data):
        return rc4(object_key(key, number), data)

    def string(number, raw):
        """A hex string, so an encrypted byte never needs escaping."""
        return b"<" + encrypt(number, raw).hex().encode() + b">"

    def stream(number, data, extra=b""):
        body = encrypt(number, data)
        return b"<< " + extra + b"/Length %d >>\nstream\n" % len(body) + body + b"\nendstream"

    objects = {
        1: (b"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 5 0 R "
            b"/MarkInfo << /Marked true >> /Lang " + string(1, b"en-US") +
            b" /ViewerPreferences << /DisplayDocTitle true >> /Metadata 9 0 R >>"),
        2: b"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
        3: (b"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] "
            b"/Resources << /Font << /F1 8 0 R >> >> /Contents 4 0 R "
            b"/StructParents 0 /Tabs /S >>"),
        4: stream(4, CONTENT),
        5: b"<< /Type /StructTreeRoot /K [6 0 R] /ParentTree 7 0 R >>",
        6: (b"<< /Type /StructElem /S /Document /P 5 0 R /K [10 0 R] /Lang " +
            string(6, b"en-US") + b" >>"),
        7: b"<< /Nums [0 [10 0 R]] >>",
        8: (b"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica "
            b"/Encoding /WinAnsiEncoding >>"),
        9: stream(9, XMP, b"/Type /Metadata /Subtype /XML "),
        10: b"<< /Type /StructElem /S /P /P 6 0 R /Pg 3 0 R /K 0 >>",
        11: (b"<< /Filter /Standard /V 4 /R 4 /Length 128 "
             b"/CF << /StdCF << /CFM /V2 /AuthEvent /DocOpen /Length 16 >> >> "
             b"/StmF /StdCF /StrF /StdCF "
             b"/O <" + o_value.hex().encode() + b"> "
             b"/U <" + u_value.hex().encode() + b"> "
             b"/P %d >>" % permissions),
    }

    out = bytearray(b"%PDF-1.7\n%\xe2\xe3\xcf\xd3\n")
    offsets = {}
    for number in sorted(objects):
        offsets[number] = len(out)
        out += b"%d 0 obj\n" % number + objects[number] + b"\nendobj\n"

    startxref = len(out)
    # Each entry is exactly 20 bytes, its two-byte end-of-line marker
    # included (ISO 32000-1, 7.5.4).
    out += b"xref\n0 %d\n0000000000 65535 f\r\n" % (len(objects) + 1)
    for number in sorted(objects):
        out += b"%010d 00000 n\r\n" % offsets[number]
    out += (b"trailer\n<< /Size %d /Root 1 0 R /Encrypt 11 0 R /ID [<%s> <%s>] >>\n"
            b"startxref\n%d\n%%%%EOF\n"
            % (len(objects) + 1, DOC_ID.hex().encode(), DOC_ID.hex().encode(), startxref))

    with open(path, "wb") as fh:
        fh.write(bytes(out))
    print("wrote", path, len(out), "bytes, /P", permissions)


build(os.path.normpath(os.path.join(PDFS_DIR, "encperm_ua1.pdf")), PERMISSIONS)
