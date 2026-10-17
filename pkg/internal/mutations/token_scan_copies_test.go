package mutations

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// copiesDoc assembles a document from numbered object bodies, with a
// cross-reference table that lists each object at its offset. An object
// number in free is listed as free while its bytes stay in the file, and
// trailerExtra is spliced into the trailer.
func copiesDoc(t *testing.T, objs map[int]string, free map[int]bool, trailerExtra string) *document.Document {
	t.Helper()
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	nums := make([]int, 0, len(objs))
	for n := range objs {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	size := nums[len(nums)-1] + 1
	offsets := make(map[int]int, len(objs))
	for _, n := range nums {
		offsets[n] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", n, objs[n])
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", size)
	for n := 1; n < size; n++ {
		off, ok := offsets[n]
		if !ok || free[n] {
			buf.WriteString("0000000000 00001 f\r\n")
			continue
		}
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R %s >>\nstartxref\n%d\n%%%%EOF\n", size, trailerExtra, xref)

	data := buf.Bytes()
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return doc
}

// copiesObjects returns a one-page document's objects with extra spliced into
// its catalog: the page draws nothing a scan for SECRET could find.
func copiesObjects(catalogExtra string) map[int]string {
	body := "BT /F1 12 Tf 100 600 Td (KEEP) Tj ET"
	return map[int]string{
		1: "<< /Type /Catalog /Pages 2 0 R " + catalogExtra + " >>",
		2: "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		3: "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>",
		4: streamObject("", []byte(body)),
	}
}

// streamObject writes a stream object body with dict entries and data.
func streamObject(dict string, data []byte) string {
	return fmt.Sprintf("<< %s /Length %d >>\nstream\n%s\nendstream", dict, len(data), data)
}

func scanFor(t *testing.T, doc *document.Document, token string) ScanResult {
	t.Helper()
	res, err := ScanOutputForTokens(context.Background(), nil, doc, []string{token})
	if err != nil {
		t.Fatalf("ScanOutputForTokens: %v", err)
	}
	return res
}

// TestScanOutputForTokens_ReadsTheBytesPastAFiltersEnd pins the stored-tail
// check. Each stream decodes to "hello" and stores SECRET after its filter's
// end, where a rewrite copies it and a reader of the file's bytes finds it. A
// decoded comparison alone reported each one clean.
func TestScanOutputForTokens_ReadsTheBytesPastAFiltersEnd(t *testing.T) {
	t.Parallel()
	flate, err := flateCompress([]byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"flate":     streamObject("/Filter /FlateDecode", append(flate, []byte("SECRET")...)),
		"asciihex":  streamObject("/Filter /ASCIIHexDecode", []byte("68656c6c6f> SECRET")),
		"ascii85":   streamObject("/Filter /ASCII85Decode", []byte("BOu!rDZ~> SECRET")),
		"hexdamage": streamObject("/Filter /ASCIIHexDecode", []byte("68656c6c6f SECRET")),
	}
	for name, stream := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			objs := copiesObjects("/PieceInfo << /App << /Private 5 0 R >> >>")
			objs[5] = stream
			if res := scanFor(t, copiesDoc(t, objs, nil, ""), "SECRET"); !hasHit(res, 5, "stream") {
				t.Errorf("the stored copy past the filter's end was not reported: %+v", res)
			}
		})
	}

	// The control: the same filter with nothing after its end is clean, so the
	// hits above come from the tail rather than from the stream at large.
	objs := copiesObjects("/PieceInfo << /App << /Private 5 0 R >> >>")
	objs[5] = streamObject("/Filter /FlateDecode", flate)
	if res := scanFor(t, copiesDoc(t, objs, nil, ""), "SECRET"); !res.Clean() {
		t.Errorf("a stream with no tail reported %+v", res)
	}
}

// TestScanOutputForTokens_ReadsContentStringsAsTheyDraw pins the content
// operand check: a hexadecimal string, an escaped one, a TJ array split by
// kerning, and an inline /ActualText in UTF-16 all draw or stand for SECRET
// without holding its bytes, in an appearance stream the rewrite copies.
func TestScanOutputForTokens_ReadsContentStringsAsTheyDraw(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"hex":        "BT /F1 12 Tf <534543524554> Tj ET",
		"escaped":    `BT /F1 12 Tf (\123\105\103\122\105\124) Tj ET`,
		"kerned":     "BT /F1 12 Tf [(SEC) -20 (RET)] TJ ET",
		"actualtext": "/Span << /ActualText <FEFF005300450043005200450054> >> BDC EMC",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			objs := copiesObjects("")
			objs[3] = "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Annots [5 0 R] >>"
			objs[5] = "<< /Type /Annot /Subtype /FreeText /Rect [0 0 100 20] /AP << /N 6 0 R >> >>"
			objs[6] = streamObject("/Type /XObject /Subtype /Form /BBox [0 0 100 20]", []byte(body))
			if res := scanFor(t, copiesDoc(t, objs, nil, ""), "SECRET"); !hasHit(res, 6, "stream") {
				t.Errorf("the appearance stream drawing the word was not reported: %+v", res)
			}
		})
	}

	// A page's own content stream is read the same way, though its dictionary
	// says nothing of what it holds.
	objs := copiesObjects("")
	objs[4] = streamObject("", []byte("BT /F1 12 Tf [(SEC) 5 (RET)] TJ ET"))
	if res := scanFor(t, copiesDoc(t, objs, nil, ""), "SECRET"); !hasHit(res, 4, "stream") {
		t.Errorf("the page content stream drawing the word was not reported: %+v", res)
	}
}

// TestScanOutputForTokens_ReadsDestinationNames pins the /Dests check: the
// PDF 1.1 named destinations are the keys of a dictionary (ISO 32000-1,
// 12.3.2.3), names the walk otherwise never compares.
func TestScanOutputForTokens_ReadsDestinationNames(t *testing.T) {
	t.Parallel()
	objs := copiesObjects("/Dests 5 0 R")
	objs[5] = "<< /Chapter#20SECRET [3 0 R /Fit] >>"
	if res := scanFor(t, copiesDoc(t, objs, nil, ""), "SECRET"); !hasHit(res, 5, "name") {
		t.Errorf("the destination name was not reported: %+v", res)
	}

	// The control: a structural name never counts, so a word such as Page is
	// not reported from /Type /Page.
	if res := scanFor(t, copiesDoc(t, copiesObjects(""), nil, ""), "Page"); !res.Clean() {
		t.Errorf("a structural name was reported as a copy: %+v", res)
	}
}

// TestScanOutputForTokens_ReadsTheFileIdentifier pins the /ID check: the
// rewrite copies the trailer's /ID, and its bytes are compared as they are, so
// a digest byte read as a PDFDocEncoding bullet does not count.
func TestScanOutputForTokens_ReadsTheFileIdentifier(t *testing.T) {
	t.Parallel()
	doc := copiesDoc(t, copiesObjects(""), nil, "/ID [(SECRET) (SECRET)]")
	if res := scanFor(t, doc, "SECRET"); !hasHit(res, 0, "string") {
		t.Errorf("the /ID copy was not reported: %+v", res)
	}

	doc = copiesDoc(t, copiesObjects(""), nil, "/ID [<80A1> <80A1>]")
	if res := scanFor(t, doc, "•"); !res.Clean() {
		t.Errorf("a digest byte was read as text: %+v", res)
	}
}

// TestScanOutputForTokens_ReadsAnUndefinedObjectAsNull pins the reading of a
// reference to an object the file does not define: the null object (ISO
// 32000-1, 7.3.10), which the rewrite writes in its place, so the scan has
// nothing to report and nothing it failed to read. An object the table lists
// but that does not parse stays unreadable.
func TestScanOutputForTokens_ReadsAnUndefinedObjectAsNull(t *testing.T) {
	t.Parallel()
	if res := scanFor(t, copiesDoc(t, copiesObjects("/PieceInfo 9 0 R"), nil, ""), "SECRET"); !res.Clean() {
		t.Errorf("a dangling reference was reported: %+v", res)
	}

	objs := copiesObjects("/PieceInfo 5 0 R")
	objs[5] = "10P1 garbage"
	if res := scanFor(t, copiesDoc(t, objs, nil, ""), "SECRET"); len(res.Unreadable) != 1 || res.Unreadable[0] != 5 {
		t.Errorf("Unreadable = %v, want [5]: an object that does not parse is not proved clean", res.Unreadable)
	}
}

// TestScanOutputForTokens_FreeEntryIsNotCopied pins that an object the table
// lists as free reads as null even while its bytes stay in the file: neither
// the scan nor the rewrite reaches them, so the copy they hold is not in the
// output.
func TestScanOutputForTokens_FreeEntryIsNotCopied(t *testing.T) {
	t.Parallel()
	objs := copiesObjects("/PieceInfo 5 0 R")
	objs[5] = "<< /App << /Private (SECRET) >> >>"
	if res := scanFor(t, copiesDoc(t, objs, map[int]bool{5: true}, ""), "SECRET"); !res.Clean() {
		t.Errorf("a free object was reported: %+v", res)
	}
	if res := scanFor(t, copiesDoc(t, objs, nil, ""), "SECRET"); !hasHit(res, 5, "string") {
		t.Errorf("the control, the same object in use, was not reported: %+v", res)
	}
}
