package build

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
	"github.com/gotenberg/litho/test/integration/fixture/pdfgen/zdeflate"
)

// Shared helpers, ported from common.go.

// obj is pdfgen.Object, short for the builders.
type obj = pdfgen.Object

var (
	d    = pdfgen.D
	a    = pdfgen.A
	n    = pdfgen.N
	text = pdfgen.Text
)

// str is a PDF string over the raw bytes of s.
func str(s string) pdfgen.String { return pdfgen.String(s) }

// zlibCompress is zlib at its default level, as the committed files were
// compressed.
func zlibCompress(data []byte) []byte { return zdeflate.Compress(data, -1) }

// save writes doc with a deterministic /ID and its streams as given, at
// least at minVersion. extra sets further save options. The committed files
// were first written by pikepdf over qpdf, and pdfgen reproduces that layout
// byte for byte.
func save(doc *pdfgen.Doc, minVersion string, extra ...func(*pdfgen.SaveOptions)) ([]byte, error) {
	out, err := saveOutput(doc, minVersion, extra...)
	if err != nil {
		return nil, err
	}
	return out.Bytes, nil
}

// saveOutput is save, returning the object numbers alongside the bytes for
// a builder that patches the file afterwards.
func saveOutput(doc *pdfgen.Doc, minVersion string, extra ...func(*pdfgen.SaveOptions)) (*pdfgen.Output, error) {
	opts := pdfgen.SaveOptions{MinVersion: minVersion}
	for _, f := range extra {
		f(&opts)
	}
	out, err := doc.Save(opts)
	if err != nil {
		return nil, fmt.Errorf("saving: %w", err)
	}
	return out, nil
}

// noPreservePDFA writes no newline before endstream (pikepdf's
// preserve_pdfa=False).
func noPreservePDFA(o *pdfgen.SaveOptions) { o.DisablePreservePDFA = true }

// noFixMetadataVersion keeps the catalog's XMP packet as given (pikepdf's
// fix_metadata_version=False).
func noFixMetadataVersion(o *pdfgen.SaveOptions) { o.DisableFixMetadataVersion = true }

// generateObjectStreams writes object streams and a cross-reference stream
// (pikepdf's object_stream_mode=generate).
func generateObjectStreams(o *pdfgen.SaveOptions) { o.GenerateObjectStreams = true }

// lzwEncode LZW-compresses data the way PDF's LZWDecode filter expects.
//
// Used to plant a genuinely decodable LZWDecode stream: ISO 19005-1
// 6.1.10 and ISO 19005-2 6.1.7 both forbid the filter, and a validator
// must reject it on the filter name alone rather than on a decode
// failure.
func lzwEncode(data []byte, earlyChange int) []byte {
	var out []byte
	bitbuf, bitcnt := uint64(0), 0
	emit := func(code, width int) {
		bitbuf = bitbuf<<uint(width) | uint64(code) //nolint:gosec // code < 4096.
		bitcnt += width
		for bitcnt >= 8 {
			bitcnt -= 8
			out = append(out, byte(bitbuf>>uint(bitcnt))) //nolint:gosec // keeping the low byte on purpose.
		}
		bitbuf &= 1<<uint(bitcnt) - 1
	}
	var table map[string]int
	var next, width int
	reset := func() {
		table = make(map[string]int, 4096)
		for i := range 256 {
			table[string([]byte{byte(i)})] = i
		}
		next, width = 258, 9
	}
	reset()
	emit(256, width)
	prev := ""
	for _, b := range data {
		cur := prev + string([]byte{b})
		if _, ok := table[cur]; ok {
			prev = cur
			continue
		}
		emit(table[prev], width)
		table[cur] = next
		next++
		if next+earlyChange > 1<<uint(width) {
			if width == 12 {
				emit(256, width)
				reset()
			} else {
				width++
			}
		}
		prev = string([]byte{b})
	}
	if prev != "" {
		emit(table[prev], width)
	}
	emit(257, width)
	if bitcnt > 0 {
		out = append(out, byte(bitbuf<<uint(8-bitcnt))) //nolint:gosec // keeping the low byte on purpose.
	}
	return out
}

// xmpOpts are xmpPacket's identifiers. A nil field writes no property.
type xmpOpts struct {
	Part, Conformance, Rev, UAPart, UARev any
	Extra                                 string
}

// xmpPacket builds a minimal XMP packet, optionally carrying a conformance
// claim.
//
// Passing no identifiers yields a well-formed packet with no claim,
// which is what the "wrong claim" fixtures need as a starting point.
// UARev feeds pdfuaid:rev, which ISO 14289-2, 5 requires to be 2024.
func xmpPacket(o xmpOpts) []byte {
	var ids []string
	add := func(tag string, v any) {
		if v != nil {
			ids = append(ids, fmt.Sprintf("    <%s>%v</%s>", tag, v, tag))
		}
	}
	add("pdfaid:part", o.Part)
	add("pdfaid:conformance", o.Conformance)
	add("pdfaid:rev", o.Rev)
	add("pdfuaid:part", o.UAPart)
	add("pdfuaid:rev", o.UARev)
	return []byte("<?xpacket begin=\"\ufeff\" id=\"W5M0MpCehiHzreSzNTczkc9d\"?>\n" +
		"<x:xmpmeta xmlns:x=\"adobe:ns:meta/\">\n" +
		" <rdf:RDF xmlns:rdf=\"http://www.w3.org/1999/02/22-rdf-syntax-ns#\">\n" +
		"  <rdf:Description rdf:about=\"\"\n" +
		"    xmlns:pdfaid=\"http://www.aiim.org/pdfa/ns/id/\"\n" +
		"    xmlns:pdfuaid=\"http://www.aiim.org/pdfua/ns/id/\"\n" +
		"    xmlns:dc=\"http://purl.org/dc/elements/1.1/\">\n" +
		"   <dc:title><rdf:Alt><rdf:li xml:lang=\"x-default\">litho corpus fixture" +
		"</rdf:li></rdf:Alt></dc:title>\n" +
		strings.Join(ids, "\n") + "\n" +
		o.Extra +
		"  </rdf:Description>\n" +
		" </rdf:RDF>\n" +
		"</x:xmpmeta>\n" +
		"<?xpacket end=\"w\"?>\n")
}

// attachXMP attaches an XMP packet to the catalog as an uncompressed
// stream.
func attachXMP(doc *pdfgen.Doc, packet []byte) {
	doc.Root.Set("Metadata", doc.Stream(packet, "Type", n("Metadata"), "Subtype", n("XML")))
}

// type3Proc is one (glyph name, d0 advance, painting operators) entry of
// type3_font's procs.
type type3Proc struct {
	Name    string
	Advance int
	Ops     string
}

// type3Font builds a Type3 font whose CharProcs carry explicit d0 advances.
//
// procs is a list of (glyph name, d0 advance, painting operators), encoded
// from code 65 ("A") on. widths is the /Widths array, which the caller
// deliberately skews to plant a width mismatch on a chosen code.
func type3Font(doc *pdfgen.Doc, procs []type3Proc, widths []any) *pdfgen.Dict {
	const first = 65
	charprocs := d()
	diffs := a(first)
	for _, p := range procs {
		s := doc.Stream([]byte(fmt.Sprintf("%d 0 d0\n%s\n", p.Advance, p.Ops)))
		charprocs.Set(p.Name, s)
		diffs.Append(n(p.Name))
	}
	return d(
		"Type", n("Font"),
		"Subtype", n("Type3"),
		"FontBBox", a(0, 0, 1000, 1000),
		"FontMatrix", a(0.001, 0, 0, 0.001, 0, 0),
		"CharProcs", charprocs,
		"Encoding", d("Type", n("Encoding"), "Differences", diffs),
		"FirstChar", first,
		"LastChar", first+len(procs)-1,
		"Widths", a(widths...),
		"Resources", d(),
	)
}

// toUnicodeCMap builds a ToUnicode CMap stream mapping single-byte codes to
// code points.
//
// mapping is a dict of {code: codepoint}. Used to steer a glyph onto a
// Unicode Private Use Area value (ISO 14289-2, 8.4.3) or to leave a code
// unmapped (ISO 19005-2, 6.2.11.7.2).
func toUnicodeCMap(doc *pdfgen.Doc, mapping map[int]int) *pdfgen.Stream {
	lines := []string{
		"/CIDInit /ProcSet findresource begin",
		"12 dict begin",
		"begincmap",
		"/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def",
		"/CMapName /Adobe-Identity-UCS def",
		"/CMapType 2 def",
		"1 begincodespacerange",
		"<00> <FF>",
		"endcodespacerange",
		fmt.Sprintf("%d beginbfchar", len(mapping)),
	}
	for _, code := range sortedKeys(mapping) {
		lines = append(lines, fmt.Sprintf("<%02X> <%04X>", code, mapping[code]))
	}
	lines = append(lines,
		"endbfchar",
		"endcmap",
		"CMapName currentdict /CMap defineresource pop",
		"end",
		"end",
	)
	return doc.Stream([]byte(strings.Join(lines, "\n") + "\n"))
}

// unembeddedTrueType builds a simple TrueType font dict named Helvetica with
// no font program.
func unembeddedTrueType() *pdfgen.Dict {
	const base = "/Helvetica"
	return d(
		"Type", n("Font"),
		"Subtype", n("TrueType"),
		"BaseFont", n(base),
		"Encoding", n("WinAnsiEncoding"),
		"FirstChar", 65,
		"LastChar", 66,
		"Widths", a(722, 722),
		"FontDescriptor", d(
			"Type", n("FontDescriptor"),
			"FontName", n(base),
			"Flags", 32,
			"FontBBox", a(-166, -225, 1000, 931),
			"ItalicAngle", 0,
			"Ascent", 718,
			"Descent", -207,
			"CapHeight", 718,
			"StemV", 88,
		),
	)
}

// rgbImage builds a 2x2 DeviceRGB image XObject.
func rgbImage(doc *pdfgen.Doc, interpolate bool) *pdfgen.Stream {
	raw := []byte{255, 0, 0, 0, 255, 0, 0, 0, 255, 255, 255, 0}
	img := doc.Stream(zlibCompress(raw),
		"Type", n("XObject"),
		"Subtype", n("Image"),
		"Width", 2,
		"Height", 2,
		"ColorSpace", n("DeviceRGB"),
		"BitsPerComponent", 8,
		"Filter", n("FlateDecode"),
	)
	if interpolate {
		img.Set("Interpolate", true)
	}
	return img
}

// filespec builds an embedded-file specification dictionary.
//
// subtype is the embedded file's MIME type, empty for none. A MIME type
// contains '/', which a PDF name can only carry as the escape #2F, so
// subtype is that name token, such as "/text#2Fxml", decoded by nameToken. relationship is
// the /AFRelationship name, empty for none.
func filespec(doc *pdfgen.Doc, fileName string, payload []byte, relationship, subtype string) *pdfgen.Dict {
	ef := doc.Stream(payload, "Type", n("EmbeddedFile"))
	if subtype != "" {
		ef.Set("Subtype", nameToken(subtype))
	}
	ef.Set("Params", d("Size", len(payload)))
	spec := d(
		"Type", n("Filespec"),
		"F", text(fileName),
		"UF", text(fileName),
		"EF", d("F", ef),
		"Desc", text("litho corpus attachment"),
	)
	if relationship != "" {
		spec.Set("AFRelationship", n(relationship))
	}
	return doc.Indirect(spec)
}

func sortedKeys[V any](m map[int]V) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

// nameToken decodes a name token as PDF syntax reads it, honouring #xx
// escapes, so "/text#2Fxml" is the name text/xml.
func nameToken(tok string) pdfgen.Name {
	tok = strings.TrimPrefix(tok, "/")
	var b strings.Builder
	for i := 0; i < len(tok); i++ {
		if tok[i] == '#' && i+2 < len(tok) {
			if v, err := hex.DecodeString(tok[i+1 : i+3]); err == nil {
				b.Write(v)
				i += 2
				continue
			}
		}
		b.WriteByte(tok[i])
	}
	return pdfgen.Name(b.String())
}

// patchOnce replaces old with new exactly once, refusing any length change:
// the patch helper several builders define to edit saved bytes without
// moving an offset.
func patchOnce(data, old, repl []byte, why string) ([]byte, error) {
	if len(old) != len(repl) {
		return nil, fmt.Errorf("%s: patch changes length, offsets would move", why)
	}
	if c := bytes.Count(data, old); c != 1 {
		return nil, fmt.Errorf("%s: expected exactly one %q, found %d", why, old, c)
	}
	return bytes.Replace(data, old, repl, 1), nil
}

// docinfo returns the trailer's /Info, created as an empty indirect
// dictionary when absent and made indirect when direct. Calling it changes
// the object numbering, as reading pikepdf's pdf.docinfo did when the
// committed files were first written.
func docinfo(pdf *pdfgen.Doc) *pdfgen.Dict {
	info, ok := pdf.Trailer.Get("Info").(*pdfgen.Dict)
	if !ok {
		info = pdf.Indirect(d())
		pdf.Trailer.Set("Info", info)
	}
	if !pdfgen.IsIndirect(info) {
		pdf.Indirect(info)
	}
	return info
}

// idOf returns the first /ID element of a file pdfgen saved, for a builder
// that reuses it.
func idOf(saved []byte) pdfgen.String {
	i := bytes.LastIndex(saved, []byte("/ID [<"))
	if i < 0 {
		return nil
	}
	rest := saved[i+len("/ID [<"):]
	before, _, ok := bytes.Cut(rest, []byte{'>'})
	if !ok {
		return nil
	}
	id, err := hex.DecodeString(string(before))
	if err != nil {
		return nil
	}
	return pdfgen.String(id)
}
