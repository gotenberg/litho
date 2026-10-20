package pdf

import (
	"archive/zip"
	"bytes"
	"compress/zlib"
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// leakBody draws SECRET, which coversSecret takes, and KEEP, which stays.
const leakBody = "BT /F1 12 Tf 100 700 Td (SECRET) Tj ET BT /F1 12 Tf 100 600 Td (KEEP) Tj ET"

// splitScript builds SECRET at run time from two pieces, so no comparison of
// the file's bytes finds the word.
const splitScript = "var s = 'SEC' + 'RET';"

// utf16Of spells s in UTF-16, little- or big-endian, behind a byte order mark
// when bom is set.
func utf16Of(s string, littleEndian, bom bool) []byte {
	var out []byte
	if bom {
		if littleEndian {
			out = append(out, 0xFF, 0xFE)
		} else {
			out = append(out, 0xFE, 0xFF)
		}
	}
	for _, u := range utf16.Encode([]rune(s)) {
		if littleEndian {
			out = append(out, byte(u), byte(u>>8))
		} else {
			out = append(out, byte(u>>8), byte(u))
		}
	}
	return out
}

// compressedDraft returns a zlib stream of a text repeating word, long enough
// that no literal copy of it survives compression.
func compressedDraft(t *testing.T, word string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	for i := range 64 {
		if _, err := fmt.Fprintf(zw, "line %d of the draft: %s, reviewed.\n", i, word); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// zippedDraft returns a zip archive whose one member repeats word.
func zippedDraft(t *testing.T, word string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, err := zw.Create("notes.txt")
	if err != nil {
		t.Fatal(err)
	}
	for i := range 64 {
		if _, err := fmt.Fprintf(f, "line %d of the draft: %s, reviewed.\n", i, word); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// draftPDF returns a one-page PDF whose Flate-compressed content draws word.
// Its objects are numbered from 91 so they cannot stand in for the carrying
// document's own when a reader recovers objects from the raw bytes.
func draftPDF(t *testing.T, word string) []byte {
	t.Helper()
	content := compressedDraft(t, word)
	return []byte("%PDF-1.7\n91 0 obj\n<< /Type /Catalog /Pages 92 0 R >>\nendobj\n" +
		"92 0 obj\n<< /Type /Pages /Kids [93 0 R] /Count 1 >>\nendobj\n" +
		"93 0 obj\n<< /Type /Page /Parent 92 0 R /MediaBox [0 0 612 792] /Contents 94 0 R >>\nendobj\n" +
		"94 0 obj\n<< /Filter /FlateDecode /Length " + strconv.Itoa(len(content)) + " >>\nstream\n" +
		string(content) + "\nendstream\nendobj\ntrailer\n<< /Root 91 0 R /Size 95 >>\n%%EOF\n")
}

// attachment plants data as an embedded file named name.
func attachment(name string, data []byte) plant {
	return plant{
		catalog: "/Names << /EmbeddedFiles << /Names [(" + name + ") 7 0 R] >> >>",
		objects: "7 0 obj\n<< /Type /Filespec /F (" + name + ") /EF << /F 8 0 R >> >>\nendobj\n" +
			plantStream(8, "/Type /EmbeddedFile", string(data)),
	}
}

// signedField plants a signed signature field whose signature dictionary
// carries sigExtra, its widget on the page.
func signedField(sigExtra string) plant {
	return plant{
		catalog: "/AcroForm << /Fields [7 0 R] /SigFlags 3 >>",
		page:    "/Annots [7 0 R]",
		objects: "7 0 obj\n<< /Type /Annot /Subtype /Widget /FT /Sig /T (s) /Rect [0 0 0 0] /P 3 0 R /V 8 0 R >>\nendobj\n" +
			"8 0 obj\n<< /Type /Sig /Filter /Adobe.PPKLite /SubFilter /adbe.pkcs7.detached " +
			"/ByteRange [0 10 20 10] /Contents <3082> " + sigExtra + " >>\nendobj\n",
	}
}

// leakVector is one home of a copy of the removed word: the plant that puts
// it there, the bytes of the copy as planted, and what the redaction must do.
type leakVector struct {
	name    string
	plant   plant
	carrier []byte
	// refuse is true when the copy stays and the redaction must refuse with
	// ErrRedactionIncomplete, false when the redaction must clear it and
	// succeed.
	refuse bool
}

func leakVectors(t *testing.T) []leakVector {
	t.Helper()
	wideScript := string(utf16Of("app.alert('SECRET');", false, true))
	thumbnail := base64.StdEncoding.EncodeToString([]byte("THUMBNAIL PIXELS OF THE PAGE AS IT WAS"))
	private := compressedDraft(t, "SECRET")
	draft := draftPDF(t, "SECRET")
	zipped := zippedDraft(t, "SECRET")
	wideText := utf16Of("draft: SECRET again", true, true)
	js := func(where string) string { return "<< /S /JavaScript /JS (" + splitScript + ") >>" + where }

	return []leakVector{
		{name: "OpenAction script", plant: plant{catalog: "/OpenAction " + js("")}, carrier: []byte(splitScript)},
		{name: "catalog AA script", plant: plant{catalog: "/AA << /WC " + js(" >>")}, carrier: []byte(splitScript)},
		{name: "page AA script", plant: plant{page: "/AA << /O " + js(" >>")}, carrier: []byte(splitScript)},
		{
			name: "Names JavaScript UTF-16 stream",
			plant: plant{
				catalog: "/Names << /JavaScript << /Names [(init) 7 0 R] >> >>",
				objects: "7 0 obj\n<< /S /JavaScript /JS 8 0 R >>\nendobj\n" + plantStream(8, "", wideScript),
			},
			carrier: []byte(wideScript),
		},
		{
			name: "link annotation script",
			plant: plant{page: "/Annots [7 0 R]", objects: "7 0 obj\n<< /Type /Annot /Subtype /Link " +
				"/Rect [400 100 420 120] /A " + js(" >>") + "\nendobj\n"},
			carrier: []byte(splitScript),
		},
		{
			name: "field calculation script",
			plant: plant{
				catalog: "/AcroForm << /Fields [7 0 R] >>", page: "/Annots [7 0 R]",
				objects: "7 0 obj\n<< /Type /Annot /Subtype /Widget /FT /Tx /T (f) /Rect [400 100 500 120] " +
					"/P 3 0 R /AA << /C " + js(" >> >>") + "\nendobj\n",
			},
			carrier: []byte(splitScript),
		},
		{
			name:    "rendition script",
			plant:   plant{catalog: "/OpenAction << /S /Rendition /OP 4 /JS (" + splitScript + ") >>"},
			carrier: []byte(splitScript),
		},
		{
			name: "page metadata thumbnail",
			plant: plant{page: "/Metadata 7 0 R", objects: plantStream(7, "/Type /Metadata /Subtype /XML",
				"<x:xmpmeta><xmpGImg:image>"+thumbnail+"</xmpGImg:image></x:xmpmeta>")},
			carrier: []byte(thumbnail),
		},
		{
			name: "page metadata UTF-16",
			plant: plant{page: "/Metadata 7 0 R", objects: plantStream(7, "/Type /Metadata /Subtype /XML",
				string(utf16Of("<x:xmpmeta><dc:title>SECRET</dc:title></x:xmpmeta>", false, true)))},
			carrier: utf16Of("SECRET", false, false),
		},
		{
			name: "catalog private data",
			plant: plant{catalog: "/PieceInfo << /App << /LastModified (D:20200101) /Private 7 0 R >> >>",
				objects: plantStream(7, "", string(private))},
			carrier: private,
		},
		{name: "signature reason", plant: signedField("/Reason (SECRET)"), carrier: []byte("SECRET")},
		{
			name:    "signature contents UTF-16",
			plant:   signedField("/Name <" + hex.EncodeToString(utf16Of("SECRET", false, false)) + ">"),
			carrier: utf16Of("SECRET", false, false),
		},
		{
			name: "security store of a signed file",
			plant: func() plant {
				p := signedField("")
				p.catalog += " /DSS << /Certs [9 0 R] >>"
				p.objects += plantStream(9, "", "0\x82\x01\x13SECRET-CN")
				return p
			}(),
			carrier: []byte("SECRET-CN"),
		},
		{
			name: "certification signature",
			plant: plant{catalog: "/Perms << /DocMDP 8 0 R >>", objects: "8 0 obj\n<< /Type /Sig " +
				"/Filter /Adobe.PPKLite /ByteRange [0 10 20 10] /Contents <3082> /Name (SECRET) >>\nendobj\n"},
			carrier: []byte("SECRET"),
		},
		{name: "UTF-16 text attachment", plant: attachment("n.txt", wideText), carrier: wideText, refuse: true},
		{name: "PDF attachment", plant: attachment("n.pdf", draft), carrier: draft, refuse: true},
		{name: "zip attachment", plant: attachment("n.zip", zipped), carrier: zipped, refuse: true},
		{
			name:    "collection schema",
			plant:   plant{catalog: "/Collection << /Type /Collection /Schema << /F1 << /Subtype /S /N (SECRET) >> >> >>"},
			carrier: []byte("SECRET"), refuse: true,
		},
		{name: "Dests key", plant: plant{catalog: "/Dests << /SECRET [3 0 R /Fit] >>"}, carrier: []byte("SECRET"), refuse: true},
		{
			name:    "Dests name tree",
			plant:   plant{catalog: "/Names << /Dests << /Names [(SECRET) [3 0 R /Fit]] >> >>"},
			carrier: []byte("SECRET"), refuse: true,
		},
		{
			name:    "security store of an unsigned file",
			plant:   plant{catalog: "/DSS << /Certs [7 0 R] >>", objects: plantStream(7, "", "0\x82\x01\x13SECRET-CN")},
			carrier: []byte("SECRET-CN"), refuse: true,
		},
	}
}

// outputHolds reports whether out carries c in its bytes, in any decoded
// stream, or in any string, so a copy the writer compressed is found too.
func outputHolds(t *testing.T, out, c []byte) bool {
	t.Helper()
	if bytes.Contains(out, c) {
		return true
	}
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(out), int64(len(out)))
	if err != nil {
		t.Fatalf("reopening the output: %v", err)
	}
	doc := r.doc
	var holds func(obj ast.PdfObject, depth int) bool
	holds = func(obj ast.PdfObject, depth int) bool {
		if depth > 32 {
			return false
		}
		switch v := obj.(type) {
		case ast.PdfString:
			return bytes.Contains([]byte(v), c)
		case ast.PdfArray:
			for _, item := range v {
				if holds(item, depth+1) {
					return true
				}
			}
		case ast.PdfDict:
			for _, item := range v {
				if holds(item, depth+1) {
					return true
				}
			}
		case ast.PdfStream:
			if holds(v.Dict, depth+1) {
				return true
			}
			rc, err := doc.NewStreamReader(ctx, v)
			if err != nil {
				return false
			}
			defer func() { _ = rc.Close() }()
			data, err := io.ReadAll(rc)
			return err == nil && bytes.Contains(data, c)
		}
		return false
	}
	for objNum := range doc.XRefEntries() {
		obj, err := document.ResolveObject(ctx, doc, ast.PdfIndirectRef{ObjectNum: objNum})
		if err == nil && holds(obj, 0) {
			return true
		}
	}
	return false
}

// TestWriter_Redact_LeakVectors pins the redaction's policy for every home of
// a copy that a comparison of the file's bytes cannot see or that a reader
// keeps outside the page: each is either cleared, and the redaction succeeds
// with the copy gone from the output, or kept, and the redaction refuses.
// Before the vectors were composed, scripts that build the word at run time,
// UTF-16 copies, copies compressed inside an attachment, a page's XMP
// thumbnail and the catalog's private data were all written out while the
// redaction reported success.
func TestWriter_Redact_LeakVectors(t *testing.T) {
	t.Parallel()
	for _, v := range leakVectors(t) {
		t.Run(v.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			src := plantedPDF(leakBody, v.plant)
			if !outputHolds(t, src, v.carrier) {
				t.Fatal("the fixture holds no planted copy")
			}
			r, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			w := NewWriter(r)
			_, redactErr := w.Redact(ctx, []RedactRegion{coversSecret})
			if v.refuse {
				if !errors.Is(redactErr, ErrRedactionIncomplete) {
					t.Fatalf("Redact: want ErrRedactionIncomplete, got %v", redactErr)
				}
				return
			}
			if redactErr != nil {
				t.Fatalf("Redact: %v", redactErr)
			}
			var out bytes.Buffer
			if err := w.WriteTo(ctx, &out); err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			if outputHolds(t, out.Bytes(), v.carrier) {
				t.Error("the redaction succeeded and the output still holds the copy")
			}
			text := allPagesText(t, out.Bytes())
			if strings.Contains(text, "SECRET") || !strings.Contains(text, "KEEP") {
				t.Errorf("page text %q: want KEEP without SECRET", text)
			}
		})
	}
}

// TestWriter_Redact_ReportsScriptsAndPrivateData pins the report of the
// vectors a redaction clears, and that what holds no copy stays: a GoTo
// before a script keeps its place, and the private data's dictionary keeps
// its /LastModified.
func TestWriter_Redact_ReportsScriptsAndPrivateData(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src := plantedPDF(leakBody, plant{
		catalog: "/Outlines 7 0 R /Names << /JavaScript << /Names [(init) 9 0 R] >> >> " +
			"/PieceInfo << /App << /LastModified (D:20200101) /Private (opaque) >> >>",
		objects: "7 0 obj\n<< /Type /Outlines /First 8 0 R /Last 8 0 R /Count 1 >>\nendobj\n" +
			"8 0 obj\n<< /Title (Intro) /Parent 7 0 R /A << /S /GoTo /D [3 0 R /Fit] " +
			"/Next << /S /JavaScript /JS (" + splitScript + ") >> >> >>\nendobj\n" +
			"9 0 obj\n<< /S /JavaScript /JS (app.alert\\('hi'\\);) >>\nendobj\n",
	})
	r, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	w := NewWriter(r)
	report, err := w.Redact(ctx, []RedactRegion{coversSecret})
	if err != nil {
		t.Fatalf("Redact: %v", err)
	}
	if report.RemovedScripts != 2 {
		t.Errorf("RemovedScripts = %d, want 2: the outline's chained script and the name tree", report.RemovedScripts)
	}
	if len(report.ScrubbedPrivateData) != 1 || report.ScrubbedPrivateData[0] != "App" {
		t.Errorf("ScrubbedPrivateData = %v, want [App]", report.ScrubbedPrivateData)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	for _, kept := range []string{"/GoTo", "/LastModified"} {
		if !bytes.Contains(out.Bytes(), []byte(kept)) {
			t.Errorf("the output lost %s, which holds no copy", kept)
		}
	}
	if bytes.Contains(out.Bytes(), []byte("JavaScript")) {
		t.Error("the output still names a JavaScript action")
	}
}

// TestWriter_Redact_BlankedImageLosesItsAlternates pins that an image blanked
// in place keeps neither its /Alternates, alternate pictures of the samples it
// had (ISO 32000-1, 8.9.5.4), nor its own /Metadata: a viewer prints an
// alternate marked for printing in its place, so keeping one prints what the
// region cleared.
func TestWriter_Redact_BlankedImageLosesItsAlternates(t *testing.T) {
	t.Parallel()
	altSamples := strings.Repeat("ALTPIXEL", 2)
	src := imageDoc([]string{placeLeft}, "", "",
		plantStream(30, "/Type /XObject /Subtype /Image /Width 4 /Height 4 /ColorSpace /DeviceGray /BitsPerComponent 8", altSamples)+
			plantStream(31, "/Type /Metadata /Subtype /XML", "<x:xmpmeta><dc:title>IMAGEMETA</dc:title></x:xmpmeta>"))
	src = bytes.Replace(src, []byte("/Subtype /Image /Width 4 /Height 4 /ColorSpace /DeviceGray /BitsPerComponent 8 /Length 16"),
		[]byte("/Subtype /Image /Width 4 /Height 4 /ColorSpace /DeviceGray /BitsPerComponent 8 "+
			"/Alternates [<< /Image 30 0 R /DefaultForPrinting true >>] /Metadata 31 0 R /Length 16"), 1)
	if !bytes.Contains(src, []byte("/Alternates")) {
		t.Fatal("the fixture's image carries no /Alternates")
	}
	out, err := redactRegions(t, src, RedactRegion{Page: 1, Rect: wholeLeft})
	if err != nil {
		t.Fatalf("Redact: %v", err)
	}
	if !allSamples(imageSamples(t, out, 1), 0) {
		t.Fatal("the image was not blanked, so the test proves nothing")
	}
	for _, c := range []string{altSamples, "IMAGEMETA"} {
		if outputHolds(t, out, []byte(c)) {
			t.Errorf("the output still holds %q", c)
		}
	}
}

// TestWriter_Redact_KeepsAnAttachmentWithoutACopy pins that an attachment
// holding no copy of the removed text, compressed or not, stays in the output:
// the scan reads into it and finds nothing, and the redaction removes no
// structure that does not hold the text.
func TestWriter_Redact_KeepsAnAttachmentWithoutACopy(t *testing.T) {
	t.Parallel()
	for name, data := range map[string][]byte{
		"pdf": draftPDF(t, "PUBLIC"),
		"zip": zippedDraft(t, "PUBLIC"),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			out := redactAndWrite(t, plantedPDF(leakBody, attachment("n."+name, data)), []RedactRegion{coversSecret})
			if !outputHolds(t, out, data) {
				t.Error("the attachment was not written out")
			}
		})
	}
}
