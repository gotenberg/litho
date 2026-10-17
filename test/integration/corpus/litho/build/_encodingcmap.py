"""Type 0 fonts over embedded encoding CMaps, for the cmap*_2b fixtures.

Each font is a CIDFontType2 over the three-glyph TrueType program of
_truetype. Its /CIDToGIDMap stream sends CIDs 1 to 6 to glyph A and stops
after CID 6, so a CID past it selects no glyph, which veraPDF reads as absent
(CIDFontType2Program.containsCID) and as .notdef. CID 0 is absent too:
containsCID refuses it, and GFGlyph exempts only code 0. The /DW equals the
program's advance, so no width rule answers.

The encoding CMaps declare the two-byte code space <0000> <FFFF> and name
themselves with a /CMapName that is no predefined CMap. A font or CMap over a
predefined character collection passes its CIDSystemInfo, and a font whose
CIDs lie elsewhere passes its own CID-to-glyph map (gid_map).
"""

import zlib

import _truetype
from _common import Array, Dictionary, Name, Stream, String, pikepdf, save

IDENTITY_SYSTEM_INFO = Dictionary(Registry=String("Adobe"), Ordering=String("Identity"), Supplement=0)

BFCHAR = b"1 beginbfchar\n<0041> <0041>\nendbfchar\n"


def system_info(ordering, supplement):
    """The Adobe CIDSystemInfo dictionary of a character collection."""
    return Dictionary(Registry=String("Adobe"), Ordering=String(ordering), Supplement=supplement)


class Fixture:
    """One single-page fixture showing a string through each of its fonts."""

    def __init__(self):
        self.pdf = pikepdf.new()
        program = _truetype.build([(3, 1, {0x41: "A", 0x42: "B"})])
        self.font_file = self.pdf.make_indirect(Stream(self.pdf, program, Length1=len(program)))
        gid_map = Stream(self.pdf, zlib.compress(bytes([0, 0]) + bytes([0, 1]) * 6, 9))
        gid_map.Filter = Name.FlateDecode
        self.cid_gid_map = self.pdf.make_indirect(gid_map)
        self.fonts = []

    def gid_map(self, cids):
        """A /CIDToGIDMap stream sending each CID of cids to glyph A, and every
        other CID to no glyph."""
        table = bytearray(2 * (max(cids) + 1))
        for cid in cids:
            table[2 * cid + 1] = 1
        stream = Stream(self.pdf, zlib.compress(bytes(table), 9))
        stream.Filter = Name.FlateDecode
        return self.pdf.make_indirect(stream)

    def cmap(self, name, body, use_cmap=None, info=IDENTITY_SYSTEM_INFO):
        """An embedded encoding CMap stream carrying body after its code space."""
        program = (
            b"/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n"
            b"/CIDSystemInfo 3 dict dup begin /Registry (Adobe) def "
            b"/Ordering (" + str(info.Ordering).encode() + b") def /Supplement "
            + str(int(info.Supplement)).encode() + b" def end def\n"
            b"/CMapName /" + name.encode() + b" def\n/CMapType 1 def\n/WMode 0 def\n"
            b"1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n" + body
            + b"endcmap\nCMapName currentdict /CMap defineresource pop\nend\nend\n"
        )
        stream = Stream(self.pdf, program)
        stream.Type = Name.CMap
        stream.CMapName = Name("/" + name)
        stream.CIDSystemInfo = info
        stream.WMode = 0
        if use_cmap is not None:
            stream.UseCMap = use_cmap
        return self.pdf.make_indirect(stream)

    def font(self, key, name, encoding, shows, info=IDENTITY_SYSTEM_INFO, gid_map=None):
        """Add the Type 0 font key over encoding, showing the hex string shows."""
        pdf = self.pdf
        descendant = pdf.make_indirect(Dictionary(
            Type=Name.Font,
            Subtype=Name.CIDFontType2,
            BaseFont=Name("/" + name),
            CIDSystemInfo=info,
            DW=_truetype.ADVANCE_WIDTH,
            CIDToGIDMap=gid_map if gid_map is not None else self.cid_gid_map,
            FontDescriptor=pdf.make_indirect(Dictionary(
                Type=Name.FontDescriptor, FontName=Name("/" + name), Flags=32, ItalicAngle=0, Ascent=800,
                Descent=-200, CapHeight=700, StemV=80, FontBBox=Array([0, -200, 550, 700]),
                FontFile2=self.font_file)),
        ))
        font = pdf.make_indirect(Dictionary(Type=Name.Font, Subtype=Name.Type0, BaseFont=Name("/" + name),
                                            Encoding=encoding, DescendantFonts=Array([descendant])))
        self.fonts.append((key, font, shows))

    def save(self, basename):
        pdf = self.pdf
        content = b"".join(
            b"BT /%s 12 Tf 50 %d Td <%s> Tj ET\n" % (key.encode(), 720 - i * 20, shows)
            for i, (key, _, shows) in enumerate(self.fonts)
        )
        page = pdf.make_indirect(
            Dictionary(
                Type=Name.Page,
                MediaBox=Array([0, 0, 612, 792]),
                Resources=Dictionary(Font=Dictionary(**{key: font for key, font, _ in self.fonts})),
                Contents=Stream(pdf, content),
            )
        )
        pdf.pages.append(pikepdf.Page(page))
        # Deliberately no /Metadata and no output intent.
        save(pdf, basename)
