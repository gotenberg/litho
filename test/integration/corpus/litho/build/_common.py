"""Shared helpers for the litho corpus fixture builders.

Each builder in this directory emits one deliberately complex PDF into
../pdfs/. The builders are committed so every fixture is reproducible:
`python3 build/complex_2b.py` rewrites `pdfs/complex_2b.pdf` byte for
byte.

pikepdf gotchas encoded here:
  * `pdf.pages.append` needs a `pikepdf.Page(pdf.make_indirect(d))`.
  * `pdf.pdf_version` is read-only; pass `min_version=` to `save`.
  * `Name.None` is a Python keyword clash; write `Name("/None")`.
"""

import os

import pikepdf
from pikepdf import Array, Dictionary, Name, Stream, String

PDFS_DIR = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "pdfs")


def out_path(basename):
    """Return the absolute path of a fixture under ../pdfs/."""
    return os.path.normpath(os.path.join(PDFS_DIR, basename))


def save(pdf, basename, min_version="1.7", **extra):
    """Write pdf to ../pdfs/<basename> with a deterministic /ID.

    Deterministic IDs keep the committed bytes stable across rebuilds so a
    regenerated fixture shows an empty diff when nothing changed.

    Stream data is written through untouched. qpdf's default generalized
    decode level silently rewrites an LZWDecode stream as Flate, which would
    delete a planted violation; `none` keeps every declared filter intact.

    extra passes further pikepdf save options through. Two matter to fixtures
    that plant a defect in the XMP: pikepdf defaults `preserve_pdfa` and
    `fix_metadata_version` to True, and both REGENERATE the packet, silently
    repairing whatever the builder planted. Pass them as False to keep the
    bytes the builder wrote.
    """
    path = out_path(basename)
    pdf.save(
        path,
        min_version=min_version,
        deterministic_id=True,
        compress_streams=False,
        stream_decode_level=pikepdf.StreamDecodeLevel.none,
        **extra,
    )
    print("wrote", path, os.path.getsize(path), "bytes")


def lzw_encode(data, early_change=1):
    """LZW-compress data the way PDF's LZWDecode filter expects.

    Used to plant a genuinely decodable LZWDecode stream: ISO 19005-1
    6.1.10 and ISO 19005-2 6.1.7 both forbid the filter, and a validator
    must reject it on the filter name alone rather than on a decode
    failure.
    """
    out = bytearray()
    bitbuf = 0
    bitcnt = 0

    def emit(code, width):
        nonlocal bitbuf, bitcnt
        bitbuf = (bitbuf << width) | code
        bitcnt += width
        while bitcnt >= 8:
            bitcnt -= 8
            out.append((bitbuf >> bitcnt) & 0xFF)

    def reset():
        return {bytes([i]): i for i in range(256)}, 258, 9

    table, nxt, width = reset()
    emit(256, width)
    prev = b""
    for byte in data:
        cur = prev + bytes([byte])
        if cur in table:
            prev = cur
            continue
        emit(table[prev], width)
        table[cur] = nxt
        nxt += 1
        if nxt + early_change > (1 << width):
            if width == 12:
                emit(256, width)
                table, nxt, width = reset()
            else:
                width += 1
        prev = bytes([byte])
    if prev:
        emit(table[prev], width)
    emit(257, width)
    if bitcnt:
        out.append((bitbuf << (8 - bitcnt)) & 0xFF)
    return bytes(out)


def xmp_packet(
    part=None, conformance=None, rev=None, ua_part=None, ua_rev=None, extra=""
):
    """Build a minimal XMP packet, optionally carrying a conformance claim.

    Passing no identifiers yields a well-formed packet with no claim,
    which is what the "wrong claim" fixtures need as a starting point.
    ua_rev feeds pdfuaid:rev, which ISO 14289-2, 5 requires to be 2024.
    """
    ids = []
    if part is not None:
        ids.append('    <pdfaid:part>%s</pdfaid:part>' % part)
    if conformance is not None:
        ids.append('    <pdfaid:conformance>%s</pdfaid:conformance>' % conformance)
    if rev is not None:
        ids.append('    <pdfaid:rev>%s</pdfaid:rev>' % rev)
    if ua_part is not None:
        ids.append('    <pdfuaid:part>%s</pdfuaid:part>' % ua_part)
    if ua_rev is not None:
        ids.append('    <pdfuaid:rev>%s</pdfuaid:rev>' % ua_rev)
    return (
        '<?xpacket begin="﻿" id="W5M0MpCehiHzreSzNTczkc9d"?>\n'
        '<x:xmpmeta xmlns:x="adobe:ns:meta/">\n'
        ' <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">\n'
        '  <rdf:Description rdf:about=""\n'
        '    xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/"\n'
        '    xmlns:pdfuaid="http://www.aiim.org/pdfua/ns/id/"\n'
        '    xmlns:dc="http://purl.org/dc/elements/1.1/">\n'
        '   <dc:title><rdf:Alt><rdf:li xml:lang="x-default">litho corpus fixture'
        '</rdf:li></rdf:Alt></dc:title>\n'
        '%s\n'
        '%s'
        '  </rdf:Description>\n'
        ' </rdf:RDF>\n'
        '</x:xmpmeta>\n'
        '<?xpacket end="w"?>\n' % ("\n".join(ids), extra)
    ).encode("utf-8")


def attach_xmp(pdf, packet):
    """Attach an XMP packet to the catalog as an uncompressed stream."""
    meta = Stream(pdf, packet)
    meta.Type = Name.Metadata
    meta.Subtype = Name.XML
    pdf.Root.Metadata = pdf.make_indirect(meta)
    return meta


def type3_font(pdf, procs, widths, first=65):
    """Build a Type3 font whose CharProcs carry explicit d0 advances.

    procs is a list of (glyph name, d0 advance, painting operators).
    widths is the /Widths array, which the caller deliberately skews to
    plant a width mismatch on a chosen code.
    """
    charprocs = Dictionary()
    diffs = [first]
    for gname, adv, ops in procs:
        s = Stream(pdf, ("%d 0 d0\n%s\n" % (adv, ops)).encode("ascii"))
        setattr(charprocs, gname, s)
        diffs.append(Name("/" + gname))
    return Dictionary(
        Type=Name.Font,
        Subtype=Name.Type3,
        FontBBox=Array([0, 0, 1000, 1000]),
        FontMatrix=Array([0.001, 0, 0, 0.001, 0, 0]),
        CharProcs=charprocs,
        Encoding=Dictionary(Type=Name.Encoding, Differences=Array(diffs)),
        FirstChar=first,
        LastChar=first + len(procs) - 1,
        Widths=Array(widths),
        Resources=Dictionary(),
    )


def tounicode_cmap(pdf, mapping):
    """Build a ToUnicode CMap stream mapping single-byte codes to code points.

    mapping is a dict of {code: codepoint}. Used to steer a glyph onto a
    Unicode Private Use Area value (ISO 14289-2, 8.4.3) or to leave a code
    unmapped (ISO 19005-2, 6.2.11.7.2).
    """
    lines = [
        "/CIDInit /ProcSet findresource begin",
        "12 dict begin",
        "begincmap",
        "/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def",
        "/CMapName /Adobe-Identity-UCS def",
        "/CMapType 2 def",
        "1 begincodespacerange",
        "<00> <FF>",
        "endcodespacerange",
        "%d beginbfchar" % len(mapping),
    ]
    for code in sorted(mapping):
        lines.append("<%02X> <%04X>" % (code, mapping[code]))
    lines += [
        "endbfchar",
        "endcmap",
        "CMapName currentdict /CMap defineresource pop",
        "end",
        "end",
    ]
    return pdf.make_indirect(Stream(pdf, ("\n".join(lines) + "\n").encode("ascii")))


def unembedded_truetype(base="/Helvetica"):
    """Build a simple TrueType font dict with no font program."""
    return Dictionary(
        Type=Name.Font,
        Subtype=Name.TrueType,
        BaseFont=Name(base),
        Encoding=Name.WinAnsiEncoding,
        FirstChar=65,
        LastChar=66,
        Widths=Array([722, 722]),
        FontDescriptor=Dictionary(
            Type=Name.FontDescriptor,
            FontName=Name(base),
            Flags=32,
            FontBBox=Array([-166, -225, 1000, 931]),
            ItalicAngle=0,
            Ascent=718,
            Descent=-207,
            CapHeight=718,
            StemV=88,
        ),
    )


def rgb_image(pdf, interpolate=False):
    """Build a 2x2 DeviceRGB image XObject."""
    import zlib

    raw = bytes([255, 0, 0, 0, 255, 0, 0, 0, 255, 255, 255, 0])
    img = Stream(pdf, zlib.compress(raw))
    img.Type = Name.XObject
    img.Subtype = Name.Image
    img.Width = 2
    img.Height = 2
    img.ColorSpace = Name.DeviceRGB
    img.BitsPerComponent = 8
    img.Filter = Name.FlateDecode
    if interpolate:
        img.Interpolate = True
    return img


def filespec(pdf, name, payload, relationship=None, subtype=None):
    """Build an embedded-file specification dictionary."""
    ef = Stream(pdf, payload)
    ef.Type = Name.EmbeddedFile
    if subtype is not None:
        # A MIME type contains '/', which a PDF name can only carry as the
        # escape #2F. Name("/text#2Fxml") does NOT do that: pikepdf escapes the
        # '#' itself and writes /text#232Fxml, whose one correct decoding is the
        # literal "text#2Fxml", not a MIME type at all. Parsing the token
        # instead honours the escape, so this really means "text/xml".
        ef.Subtype = pikepdf.Object.parse(subtype.encode("ascii"))
    ef.Params = Dictionary(Size=len(payload))
    spec = Dictionary(
        Type=Name.Filespec,
        F=String(name),
        UF=String(name),
        EF=Dictionary(F=pdf.make_indirect(ef)),
        Desc=String("litho corpus attachment"),
    )
    if relationship is not None:
        spec.AFRelationship = Name(relationship)
    return pdf.make_indirect(spec)


__all__ = [
    "Array",
    "Dictionary",
    "Name",
    "Stream",
    "String",
    "attach_xmp",
    "filespec",
    "lzw_encode",
    "out_path",
    "pikepdf",
    "rgb_image",
    "save",
    "tounicode_cmap",
    "type3_font",
    "unembedded_truetype",
    "xmp_packet",
]
