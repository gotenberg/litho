package mutations

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"fmt"
	"testing"
)

// nestedWord is long and varied enough that compressing a text repeating it
// leaves no literal copy in the compressed bytes.
const nestedWord = "SECRET"

// nestedText is a text holding nestedWord that compresses well.
func nestedText(word string) []byte {
	var b bytes.Buffer
	for i := range 64 {
		fmt.Fprintf(&b, "line %d of the draft: %s, reviewed.\n", i, word)
	}
	return b.Bytes()
}

// zipOf returns a zip archive of one deflated member.
func zipOf(t *testing.T, name string, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// gzipOf returns one gzip member holding data.
func gzipOf(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// pdfOf returns a one-page PDF, with a cross-reference table, whose content
// stream is Flate-compressed and draws word.
func pdfOf(t *testing.T, word string) []byte {
	t.Helper()
	content, err := flateCompress(nestedText(word))
	if err != nil {
		t.Fatal(err)
	}
	objs := map[int]string{
		1: "<< /Type /Catalog /Pages 2 0 R >>",
		2: "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		3: "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>",
		4: streamObject("/Filter /FlateDecode", content),
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offsets := make([]int, 5)
	for n := 1; n <= 4; n++ {
		offsets[n] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", n, objs[n])
	}
	xref := buf.Len()
	buf.WriteString("xref\n0 5\n0000000000 65535 f\r\n")
	for n := 1; n <= 4; n++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offsets[n])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size 5 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xref)
	return buf.Bytes()
}

// attachedDoc returns a document attaching data as object 6.
func attachedDoc(t *testing.T, data []byte) map[int]string {
	t.Helper()
	objs := copiesObjects("/Names << /EmbeddedFiles << /Names [(a) 5 0 R] >> >>")
	objs[5] = "<< /Type /Filespec /F (a) /EF << /F 6 0 R >> >>"
	objs[6] = streamObject("/Type /EmbeddedFile", data)
	return objs
}

// TestScanOutputForTokens_ReadsUTF16Copies pins that a copy spelled in UTF-16,
// in either byte order and with or without a byte order mark, is found in a
// stream and in a byte string. A text stream is UTF-16BE (ISO 32000-1, 7.9.3)
// and XMP may be UTF-16 in either order, and a one-byte comparison never
// matched either.
func TestScanOutputForTokens_ReadsUTF16Copies(t *testing.T) {
	t.Parallel()
	for _, le := range []bool{false, true} {
		wide := utf16Bytes("draft: "+nestedWord, le)
		if bytes.Contains(wide, []byte(nestedWord)) {
			t.Fatal("fixture holds the one-byte spelling")
		}
		if res := scanFor(t, copiesDoc(t, attachedDoc(t, wide), nil, ""), nestedWord); !hasHit(res, 6, "stream") {
			t.Errorf("little-endian %v: the UTF-16 copy in a stream was not reported: %+v", le, res)
		}
		objs := copiesObjects(fmt.Sprintf("/Perms << /X << /Contents <%x> >> >>", wide))
		if res := scanFor(t, copiesDoc(t, objs, nil, ""), nestedWord); !hasHit(res, 1, "string") {
			t.Errorf("little-endian %v: the UTF-16 copy in a byte string was not reported: %+v", le, res)
		}
	}
}

// TestScanOutputForTokens_ShortTokensSkipUTF16 pins minWideTokenUnits: a
// one-character needle's two UTF-16 bytes turn up by chance in binary data,
// so they are not looked for, while a three-character needle's are.
func TestScanOutputForTokens_ShortTokensSkipUTF16(t *testing.T) {
	t.Parallel()
	data := append([]byte{0x00, 0x01}, utf16Bytes("下", false)...)
	data = append(data, utf16Bytes("下午好", false)...)
	doc := copiesDoc(t, attachedDoc(t, data), nil, "")
	if res := scanFor(t, doc, "上"); !res.Clean() {
		t.Errorf("control: an absent needle was reported: %+v", res)
	}
	if res := scanFor(t, doc, "下"); !res.Clean() {
		t.Errorf("a one-character needle was compared in UTF-16: %+v", res)
	}
	if res := scanFor(t, doc, "下午好"); !hasHit(res, 6, "stream") {
		t.Errorf("a three-character needle was not compared in UTF-16: %+v", res)
	}
}

// TestScanOutputForTokens_ContentIsNoContainer pins that a content stream,
// a form XObject and an image are never opened as a container, whatever their
// leading bytes: a page about the file format shows "%PDF-1.4" as text, and
// reading that as an attached PDF that does not open refused a redaction over
// an unreadable object.
func TestScanOutputForTokens_ContentIsNoContainer(t *testing.T) {
	t.Parallel()
	objs := copiesObjects("")
	objs[3] = "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R " +
		"/Resources << /XObject << /Fm1 5 0 R /Im1 6 0 R >> >> >>"
	objs[4] = streamObject("", []byte("BT /F1 12 Tf 72 700 Td (%PDF-1.4) Tj ET /Fm1 Do"))
	objs[5] = streamObject("/Type /XObject /Subtype /Form /BBox [0 0 10 10]", []byte("BT (%PDF-1.7) Tj ET"))
	objs[6] = streamObject("/Type /XObject /Subtype /Image /Width 2 /Height 1 /ColorSpace /DeviceRGB /BitsPerComponent 8",
		[]byte{0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00})
	if res := scanFor(t, copiesDoc(t, objs, nil, ""), nestedWord); !res.Clean() {
		t.Errorf("content or samples were read as a container: %+v", res)
	}
	// The control: the same bytes as an attachment are opened, and refused.
	if res := scanFor(t, copiesDoc(t, attachedDoc(t, []byte("%PDF-1.4 not a file")), nil, ""), nestedWord); len(res.Unreadable) != 1 {
		t.Errorf("control: an attachment that does not open as the PDF it claims was not unreadable: %+v", res)
	}
}

// TestScanOutputForTokens_ReadsIntoContainers pins that the scan reads into an
// attachment that compresses its members: a PDF whose content stream is
// Flate-compressed, a zip, a gzip member, and a PDF inside a zip. Each hides
// the copy from a comparison of the attachment's bytes, and the control, the
// same container without the word, stays clean.
func TestScanOutputForTokens_ReadsIntoContainers(t *testing.T) {
	t.Parallel()
	builds := map[string]func(word string) []byte{
		"pdf":        func(word string) []byte { return pdfOf(t, word) },
		"zip":        func(word string) []byte { return zipOf(t, "notes.txt", nestedText(word)) },
		"gzip":       func(word string) []byte { return gzipOf(t, nestedText(word)) },
		"pdf in zip": func(word string) []byte { return zipOf(t, "draft.pdf", pdfOf(t, word)) },
	}
	for name, build := range builds {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			data := build(nestedWord)
			if bytes.Contains(data, []byte(nestedWord)) {
				t.Fatal("fixture holds an uncompressed copy")
			}
			if res := scanFor(t, copiesDoc(t, attachedDoc(t, data), nil, ""), nestedWord); !hasHit(res, 6, "stream") {
				t.Errorf("the copy inside the container was not reported: %+v", res)
			}
			if res := scanFor(t, copiesDoc(t, attachedDoc(t, build("PUBLIC")), nil, ""), nestedWord); !res.Clean() {
				t.Errorf("a container without the word was not clean: %+v", res)
			}
		})
	}
}

// TestScanOutputForTokens_FailsClosedOnUnreadableContainers pins that a
// container the scan cannot read through is reported unreadable rather than
// clean: an encrypted zip member, and archives nested past
// maxNestedScanDepth.
func TestScanOutputForTokens_FailsClosedOnUnreadableContainers(t *testing.T) {
	t.Parallel()

	encrypted := zipOf(t, "notes.txt", nestedText("PUBLIC"))
	// APPNOTE.TXT 4.3.12: the central directory header's general purpose
	// flags sit 8 bytes in; bit 0 marks the member encrypted.
	central := bytes.Index(encrypted, []byte("PK\x01\x02"))
	if central < 0 {
		t.Fatal("no central directory header")
	}
	encrypted[central+8] |= 0x1

	deep := nestedText("PUBLIC")
	for range maxNestedScanDepth + 1 {
		deep = zipOf(t, "inner.zip", deep)
	}

	for name, data := range map[string][]byte{"encrypted member": encrypted, "nested too deep": deep} {
		res := scanFor(t, copiesDoc(t, attachedDoc(t, data), nil, ""), nestedWord)
		if len(res.Unreadable) != 1 || res.Unreadable[0] != 6 {
			t.Errorf("%s: want object 6 unreadable, got %+v", name, res)
		}
	}
}
