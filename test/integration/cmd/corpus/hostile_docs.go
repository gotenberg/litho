package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"math/rand/v2"
	"path"
	"slices"
	"strings"
	"unicode/utf16"
)

// hostileBase is the unpacked document of a build corpus the hostile
// documents are derived from.
const hostileBase = "minimal"

// hostileDoc is one hostile document a build corpus generates: a package
// built to exhaust, confuse or crash a reader.
type hostileDoc struct {
	name string
	// build returns the document's bytes from the parts of [hostileBase],
	// which it must not change.
	build func(base map[string][]byte) ([]byte, error)
}

// Sizes of the hostile payloads. Each sits past the litho cap it probes, or
// just under it where the probe is what litho does with many of them.
const (
	// bombBytes inflates past what any reader lets one part inflate to:
	// litho stops at 128 MiB.
	bombBytes = 300 << 20
	// partBombBytes stays under that, so each part is read, and five of
	// them pass the 512 MiB litho reads from one package in all.
	partBombBytes = 120 << 20
	// overlapBytes is what each overlapping picture entry inflates to.
	overlapBytes = 100 << 20
	// overlapCount is how many pictures share one stored entry.
	overlapCount = 64
	// entryFlood passes the entries a package may have: 10,000 for litho,
	// 65,535 before ZIP64.
	entryFlood = 70_000
	// pageFlood passes the 10,000 pages a conversion may have, with as
	// many slides or sheets naming one part: as many parts would pass the
	// entry cap first.
	pageFlood = 10_001
	// deepNesting is how deep the nested elements go.
	deepNesting = 1_000_000
	// structureNesting is how deep the nested tables, groups or fields go.
	structureNesting = 5_000
	// longWord is the length of a run of text with no break opportunity.
	longWord = 50 << 20
	// fuzzVariants is how many byte-mutated copies of the base are made.
	fuzzVariants = 8
)

// hostileMarker is the text the hostile documents that must still convert
// in full add to their body, which the robustness expectations require.
const hostileMarker = "Hostile marker text"

// hostileDocs returns the hostile documents of a format, by name: the ones
// every format shares, then the format's own.
func hostileDocs(format string) []hostileDoc {
	docs := genericHostile(format)
	switch format {
	case "docx":
		docs = append(docs, docxHostile()...)
	case "xlsx":
		docs = append(docs, xlsxHostile()...)
	case "pptx":
		docs = append(docs, pptxHostile()...)
	}
	for i := range docs {
		docs[i].name = "hostile-" + docs[i].name
	}
	return docs
}

// edited builds a document from a copy of the base parts f changes.
func edited(f func(parts map[string][]byte) error) func(map[string][]byte) ([]byte, error) {
	return func(base map[string][]byte) ([]byte, error) {
		parts := cloneParts(base)
		if err := f(parts); err != nil {
			return nil, err
		}
		return zipPackage(parts)
	}
}

// withBodyContent builds a document whose main part's body gets content.
func withBodyContent(format string, content func() string) func(map[string][]byte) ([]byte, error) {
	return edited(func(parts map[string][]byte) error { return setBody(parts, format, content()) })
}

// rawEdited builds a document from the base entries f changes, written as
// they declare themselves.
func rawEdited(prefix []byte, f func(entries []rawEntry) ([]rawEntry, error)) func(map[string][]byte) ([]byte, error) {
	return func(base map[string][]byte) ([]byte, error) {
		entries, err := rawPackage(base)
		if err != nil {
			return nil, err
		}
		if entries, err = f(entries); err != nil {
			return nil, err
		}
		return rawZip(prefix, entries)
	}
}

// entryNamed returns the index of the entry named name.
func entryNamed(entries []rawEntry, name string) (int, error) {
	for i, e := range entries {
		if e.name == name {
			return i, nil
		}
	}
	return 0, fmt.Errorf("no entry %s", name)
}

// paddedXML writes part with n bytes of white space after its XML
// declaration, or before its root element when it has none: still the same
// well-formed document (XML 1.0, 2.8, Misc).
func paddedXML(part []byte, n int64) func(w io.Writer) error {
	return func(w io.Writer) error {
		head, rest := []byte(nil), part
		if bytes.HasPrefix(part, []byte("<?xml")) {
			if i := bytes.Index(part, []byte("?>")); i >= 0 {
				head, rest = part[:i+2], part[i+2:]
			}
		}
		if _, err := w.Write(head); err != nil {
			return err
		}
		if err := repeatTo(w, ' ', n); err != nil {
			return err
		}
		_, err := w.Write(rest)
		return err
	}
}

// xmlParts returns the names of the XML parts of parts but the content
// types and the relationships, in package order.
func xmlParts(parts map[string][]byte) []string {
	var out []string
	for _, n := range packageOrder(parts) {
		if n != contentTypesPart && path.Ext(n) == ".xml" {
			out = append(out, n)
		}
	}
	return out
}

// withDoctype puts a document type declaration into the main part, after its
// XML declaration.
func withDoctype(parts map[string][]byte, format, doctype string) error {
	main := mainPart(format)
	xml := string(parts[main])
	i := 0
	if strings.HasPrefix(xml, "<?xml") {
		i = strings.Index(xml, "?>") + 2
	}
	parts[main] = []byte(xml[:i] + doctype + xml[i:])
	return nil
}

// strictNamespaces maps the Transitional namespaces and relationship types
// to their Strict ones (ISO/IEC 29500-1, Annex A; 29500-4 keeps the
// Transitional ones).
var strictNamespaces = strings.NewReplacer( //nolint:gochecknoglobals // static table
	"http://schemas.openxmlformats.org/wordprocessingml/2006/main", "http://purl.oclc.org/ooxml/wordprocessingml/main",
	"http://schemas.openxmlformats.org/spreadsheetml/2006/main", "http://purl.oclc.org/ooxml/spreadsheetml/main",
	"http://schemas.openxmlformats.org/presentationml/2006/main", "http://purl.oclc.org/ooxml/presentationml/main",
	"http://schemas.openxmlformats.org/drawingml/2006/main", "http://purl.oclc.org/ooxml/drawingml/main",
	"http://schemas.openxmlformats.org/officeDocument/2006/relationships", "http://purl.oclc.org/ooxml/officeDocument/relationships",
)

// genericHostile returns the hostile documents every format shares.
func genericHostile(format string) []hostileDoc {
	main := mainPart(format)
	media := mediaDir(format)
	return append([]hostileDoc{
		{"zip-empty", func(map[string][]byte) ([]byte, error) { return nil, nil }},
		{"zip-not-zip", func(map[string][]byte) ([]byte, error) {
			return bytes.Repeat([]byte("This is not a zip archive.\n"), 64), nil
		}},
		{"zip-truncated", func(base map[string][]byte) ([]byte, error) {
			data, err := zipPackage(base)
			return data[:len(data)/2], err
		}},
		{"zip-bad-crc", rawEdited(nil, func(e []rawEntry) ([]rawEntry, error) {
			i, err := entryNamed(e, main)
			e[i].d.crc ^= 1
			return e, err
		})},
		{"zip-unsupported-method", rawEdited(nil, func(e []rawEntry) ([]rawEntry, error) {
			i, err := entryNamed(e, main)
			e[i].method = 12 // bzip2 (APPNOTE.TXT, 4.4.5), which no reader of the format needs
			return e, err
		})},
		{"zip-encrypted-entry", rawEdited(nil, func(e []rawEntry) ([]rawEntry, error) {
			i, err := entryNamed(e, main)
			e[i].flags |= 1 // traditional PKWARE encryption (APPNOTE.TXT, 4.4.4)
			return e, err
		})},
		{"zip-lying-size", rawEdited(nil, func(e []rawEntry) ([]rawEntry, error) {
			i, err := entryNamed(e, main)
			e[i].declared = 64
			return e, err
		})},
		{"zip-duplicate-entry", rawEdited(nil, func(e []rawEntry) ([]rawEntry, error) {
			if _, err := entryNamed(e, main); err != nil {
				return nil, err
			}
			d, err := deflateBytes([]byte("<broken"))
			return append(e, rawEntry{name: main, method: zip.Deflate, d: d}), err
		})},
		{"zip-prepended", rawEdited([]byte("%PDF-1.7\n1 0 obj << >> endobj\n%%EOF\n"), func(e []rawEntry) ([]rawEntry, error) { return e, nil })},
		{"zip-path-names", rawEdited(nil, func(e []rawEntry) ([]rawEntry, error) {
			d, err := deflateBytes([]byte("<x/>"))
			for _, n := range []string{"../escape.xml", "/absolute.xml", `..\windows.xml`, "C:/drive.xml", "word/../../up.xml", "a//b.xml", "nul\x00byte.xml"} {
				e = append(e, rawEntry{name: n, method: zip.Deflate, d: d})
			}
			return e, err
		})},
		{"zip-entry-flood", func(base map[string][]byte) ([]byte, error) {
			parts := cloneParts(base)
			for i := range entryFlood {
				parts[fmt.Sprintf("customXml/f%d.xml", i)] = nil
			}
			return zipPackage(parts)
		}},
		{"no-content-types", edited(func(parts map[string][]byte) error {
			delete(parts, contentTypesPart)
			return nil
		})},
		{"no-main-part", edited(func(parts map[string][]byte) error {
			delete(parts, main)
			return nil
		})},
		{"deflate-bomb", func(base map[string][]byte) ([]byte, error) { return bomb(base, main, 0) }},
		{"deflate-bomb-lying", func(base map[string][]byte) ([]byte, error) { return bomb(base, main, 1024) }},
		{"deflate-bomb-parts", func(base map[string][]byte) ([]byte, error) {
			entries, err := rawPackage(base)
			if err != nil {
				return nil, err
			}
			for _, n := range xmlParts(base) {
				i, err := entryNamed(entries, n)
				if err != nil {
					return nil, err
				}
				if entries[i].d, err = deflate(paddedXML(base[n], partBombBytes)); err != nil {
					return nil, err
				}
			}
			return rawZip(nil, entries)
		}},
		{"overlap-bomb", func(base map[string][]byte) ([]byte, error) {
			parts := cloneParts(base)
			pics := make([]picture, overlapCount)
			names := make([]string, overlapCount)
			for i := range pics {
				names[i] = fmt.Sprintf("%s/overlap%d.png", media, i)
				pics[i] = picture{name: names[i]}
			}
			if err := addPictures(parts, format, pics); err != nil {
				return nil, err
			}
			shared, err := deflate(func(w io.Writer) error {
				if _, err := w.Write(pixelPNG()); err != nil {
					return err
				}
				return repeatTo(w, 0, overlapBytes)
			})
			if err != nil {
				return nil, err
			}
			return overlapZip(parts, shared, names)
		}},
		{"image-pixel-bombs", edited(func(parts map[string][]byte) error {
			return addPictures(parts, format, []picture{
				{name: media + "/huge.png", data: pngImage(100_000, 100_000, 4)},
				{name: media + "/huge.jpeg", data: jpegImage(65_500, 65_500)},
				{name: media + "/huge.gif", data: gifImage(65_535, 65_535)},
				{name: media + "/tall.png", data: pngImage(1, 1<<31-1, 4)},
			})
		})},
		{"image-corrupt", edited(func(parts map[string][]byte) error {
			png := pixelPNG()
			badCRC := bytes.Clone(png)
			badCRC[len(badCRC)-1] ^= 0xFF
			return addPictures(parts, format, []picture{
				{name: media + "/truncated.png", data: png[:len(png)/2]},
				{name: media + "/badcrc.png", data: badCRC},
				{name: media + "/garbage.jpeg", data: bytes.Repeat([]byte{0xFF, 0xD8, 0x00}, 100)},
				{name: media + "/empty.png", data: []byte{}},
				{name: media + "/wrongext.png", data: jpegImage(16, 16)},
				{name: media + "/missing.png"},
			})
		})},
		{"rels-external", edited(func(parts map[string][]byte) error {
			return addPictures(parts, format, []picture{
				{name: "file:///etc/passwd", external: true},
				{name: "http://127.0.0.1:9/hostile.png", external: true},
				{name: `\\127.0.0.1\share\hostile.png`, external: true},
				{name: "../../../../../../etc/passwd"},
			})
		})},
		{"rels-cycle", edited(func(parts map[string][]byte) error {
			from := main
			if format == "xlsx" {
				from = "xl/drawings/drawingH1.xml"
			}
			return addPictures(parts, format, []picture{{name: from}, {name: relsOf(from)}, {name: contentTypesPart}})
		})},
		{"xml-billion-laughs", edited(func(parts map[string][]byte) error {
			var d strings.Builder
			d.WriteString(`<!DOCTYPE lolz [<!ENTITY l0 "lol">`)
			for i := 1; i <= 9; i++ {
				fmt.Fprintf(&d, `<!ENTITY l%d "%s">`, i, strings.Repeat(fmt.Sprintf("&l%d;", i-1), 10))
			}
			d.WriteString(`]>`)
			if err := withDoctype(parts, format, d.String()); err != nil {
				return err
			}
			return setBody(parts, format, textBlock(format, "&l9;"))
		})},
		{"xml-external-entity", edited(func(parts map[string][]byte) error {
			if err := withDoctype(parts, format, `<!DOCTYPE x [<!ENTITY xxe SYSTEM "file:///etc/passwd">]>`); err != nil {
				return err
			}
			return setBody(parts, format, textBlock(format, "&xxe;"))
		})},
		{"xml-deep-nesting", withBodyContent(format, func() string {
			return `<hx:n xmlns:hx="urn:litho:hostile">` + strings.Repeat("<hx:n>", deepNesting) + strings.Repeat("</hx:n>", deepNesting+1)
		})},
		{"xml-unclosed", edited(func(parts map[string][]byte) error {
			parts[main] = parts[main][:len(parts[main])*2/3]
			return nil
		})},
		{"xml-garbage", edited(func(parts map[string][]byte) error {
			r := rand.New(rand.NewPCG(1, 2)) //nolint:gosec // deterministic noise, not a secret
			junk := make([]byte, 4096)
			for i := range junk {
				junk[i] = byte(r.UintN(256)) //nolint:gosec // below 256
			}
			parts[main] = junk
			return nil
		})},
		{"xml-utf16", edited(func(parts map[string][]byte) error {
			if err := setBody(parts, format, textBlock(format, hostileMarker)); err != nil {
				return err
			}
			xml := strings.Replace(string(parts[main]), `encoding="UTF-8"`, `encoding="UTF-16"`, 1)
			u := utf16.Encode([]rune(xml))
			out := []byte{0xFF, 0xFE}
			for _, c := range u {
				out = append(out, byte(c), byte(c>>8)) //nolint:gosec // the low and high bytes of a UTF-16 code unit
			}
			parts[main] = out
			return nil
		})},
		{"long-word", withBodyContent(format, func() string { return textBlock(format, strings.Repeat("a", longWord)) })},
		{"strict-namespaces", edited(func(parts map[string][]byte) error {
			if err := setBody(parts, format, textBlock(format, hostileMarker)); err != nil {
				return err
			}
			for n, data := range parts {
				if n != contentTypesPart {
					parts[n] = []byte(strictNamespaces.Replace(string(data)))
				}
			}
			return nil
		})},
	}, fuzzed(main)...)
}

// bomb builds the base with its main part padded with [bombBytes] of white
// space, its headers stating declared bytes when set.
func bomb(base map[string][]byte, main string, declared uint64) ([]byte, error) {
	entries, err := rawPackage(base)
	if err != nil {
		return nil, err
	}
	i, err := entryNamed(entries, main)
	if err != nil {
		return nil, err
	}
	if entries[i].d, err = deflate(paddedXML(base[main], bombBytes)); err != nil {
		return nil, err
	}
	entries[i].declared = declared
	return rawZip(nil, entries)
}

// fuzzed returns [fuzzVariants] copies of the base whose main part has bytes
// flipped, from a fixed seed: the package stays sound, the XML need not.
func fuzzed(main string) []hostileDoc {
	out := make([]hostileDoc, 0, fuzzVariants)
	for v := range fuzzVariants {
		out = append(out, hostileDoc{fmt.Sprintf("fuzz-%d", v+1), edited(func(parts map[string][]byte) error {
			data := bytes.Clone(parts[main])
			r := rand.New(rand.NewPCG(uint64(v+1), 0x4c6974686f)) //nolint:gosec // deterministic noise, not a secret
			for range 1 + len(data)/200 {
				i := r.IntN(len(data))
				switch r.IntN(4) {
				case 0:
					data[i] = byte(r.UintN(256)) //nolint:gosec // below 256
				case 1:
					data[i] = "<>&\"'/=\x00"[r.IntN(8)]
				case 2:
					j := min(len(data), i+1+r.IntN(64))
					data = append(data[:i], data[j:]...)
				default:
					j := min(len(data), i+1+r.IntN(64))
					data = slices.Insert(data, j, bytes.Clone(data[i:j])...)
				}
			}
			parts[main] = data
			return nil
		})})
	}
	return out
}
