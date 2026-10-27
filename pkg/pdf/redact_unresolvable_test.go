package pdf

import (
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"regexp"
	"sort"
	"testing"
)

// unresolvableDoc assembles a one-page document drawing SECRET at y=700 and
// KEEP at y=600 from numbered object bodies, the page's being objects 1 to 6,
// with a cross-reference table listing each object at its offset. An object
// number in free is listed as free while its bytes stay in the file.
func unresolvableDoc(extra map[int]string, catalogExtra, pageExtra, body string, free map[int]bool, trailerExtra string) []byte {
	if body == "" {
		body = "BT /F1 12 Tf 100 700 Td (SECRET) Tj ET BT /F1 12 Tf 100 600 Td (KEEP) Tj ET"
	}
	objs := map[int]string{
		1: "<< /Type /Catalog /Pages 2 0 R " + catalogExtra + " >>",
		2: "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		3: "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 5 0 R /Contents 6 0 R " + pageExtra + " >>",
		4: "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		5: "<< /Font << /F1 4 0 R >> >>",
		6: rawStream("", []byte(body)),
	}
	maps.Copy(objs, extra)
	nums := make([]int, 0, len(objs))
	for n := range objs {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	size := nums[len(nums)-1] + 1

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
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
	return buf.Bytes()
}

// rawStream writes a stream object body.
func rawStream(dict string, data []byte) string {
	return fmt.Sprintf("<< %s /Length %d >>\nstream\n%s\nendstream", dict, len(data), data)
}

// streamBodies matches the data between the stream and endstream keywords.
var streamBodies = regexp.MustCompile(`(?s)stream\r?\n(.*?)\r?\nendstream`)

// holdsSecret reports whether the file's bytes, or the data of any of its
// FlateDecode streams decoded with compress/zlib rather than litho's own
// decoder, contain SECRET. It reads the output as another reader of the file
// would, independent of the scan under test.
func holdsSecret(t *testing.T, data []byte) bool {
	t.Helper()
	if bytes.Contains(data, []byte("SECRET")) {
		return true
	}
	for _, m := range streamBodies.FindAllSubmatch(data, -1) {
		zr, err := zlib.NewReader(bytes.NewReader(m[1]))
		if err != nil {
			continue
		}
		decoded, _ := io.ReadAll(zr) //nolint:errcheck // A damaged stream yields what it holds, which is what is searched.
		if bytes.Contains(decoded, []byte("SECRET")) {
			return true
		}
	}
	return false
}

// redactSecret opens data, redacts the SECRET run and writes the result.
func redactSecret(t *testing.T, data []byte) ([]byte, error) {
	t.Helper()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	w := NewWriter(r)
	if _, err := w.Redact(ctx, []RedactRegion{coversSecret}); err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	return out.Bytes(), nil
}

const (
	taggedBody  = "/P <</MCID 0>> BDC BT /F1 12 Tf 100 700 Td (SECRET) Tj ET EMC BT /F1 12 Tf 100 600 Td (KEEP) Tj ET"
	taggedRoot  = "/MarkInfo << /Marked true >> /StructTreeRoot 7 0 R"
	secretElem8 = "<< /Type /StructElem /S /P /P 7 0 R /Pg 3 0 R /K 0 /ActualText (SECRET) >>"
)

// TestWriter_Redact_ClearsCopiesBehindReferencesItCannotResolve pins the
// redactions that go through although an object on the way to a copy does not
// resolve. In each, the copy is reachable another way, or not at all, and the
// output holds none of it, read with an independent decoder.
//
//   - A /ParentTree entry that dangles, or that the table lists as free while
//     its bytes stay in the file: the element is still reached from the
//     root's /K, which is where it is cleared.
//   - The element itself listed free with its bytes in the file: it reads as
//     null, and the rewrite writes null, so the bytes never reach the output.
//   - An outline whose first item is free: the titled item past it is cleared
//     through /Last and /Prev.
//   - A catalog entry naming a free object holding a copy: never copied.
func TestWriter_Redact_ClearsCopiesBehindReferencesItCannotResolve(t *testing.T) {
	t.Parallel()
	cases := map[string][]byte{
		"parent tree entry dangles": unresolvableDoc(map[int]string{
			7: "<< /Type /StructTreeRoot /K 8 0 R /ParentTree << /Nums [0 20 0 R] >> >>",
			8: secretElem8,
		}, taggedRoot, "/StructParents 0", taggedBody, nil, ""),
		"parent tree entry is free": unresolvableDoc(map[int]string{
			7: "<< /Type /StructTreeRoot /K 8 0 R /ParentTree << /Nums [0 9 0 R] >> >>",
			8: secretElem8,
			9: "[8 0 R]",
		}, taggedRoot, "/StructParents 0", taggedBody, map[int]bool{9: true}, ""),
		"element is free": unresolvableDoc(map[int]string{
			7: "<< /Type /StructTreeRoot /K 8 0 R /ParentTree << /Nums [0 9 0 R] >> >>",
			8: secretElem8,
			9: "[8 0 R]",
		}, taggedRoot, "/StructParents 0", taggedBody, map[int]bool{8: true}, ""),
		"outline item is free": unresolvableDoc(map[int]string{
			7: "<< /Type /Outlines /First 8 0 R /Last 9 0 R /Count 2 >>",
			8: "<< /Title (Intro) /Parent 7 0 R /Next 9 0 R /Dest [3 0 R /Fit] >>",
			9: "<< /Title (SECRET) /Parent 7 0 R /Prev 8 0 R /Dest [3 0 R /Fit] >>",
		}, "/Outlines 7 0 R", "", "", map[int]bool{8: true}, ""),
		"catalog names a free object": unresolvableDoc(map[int]string{
			7: "<< /App << /Private (SECRET) >> >>",
		}, "/PieceInfo 7 0 R", "", "", map[int]bool{7: true}, ""),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if !holdsSecret(t, data) {
				t.Fatal("the input holds no copy, so the test proves nothing")
			}
			out, err := redactSecret(t, data)
			if err != nil {
				t.Fatalf("Redact: %v", err)
			}
			if holdsSecret(t, out) {
				t.Error("the output still holds the redacted word")
			}
			if !bytes.Contains(out, []byte("KEEP")) {
				t.Error("text outside the region was removed too")
			}
		})
	}
}

// TestWriter_Redact_RefusesCopiesNoDecodedValueSpells pins the refusals for a
// copy the scan used to miss because no decoded value spells it: the rewrite
// copies each one, and before each redaction succeeded and wrote it out.
//
//   - Bytes after a FlateDecode stream's checksum, and after an
//     ASCIIHexDecode stream's EOD marker.
//   - A PDF 1.1 named destination whose name is the word.
//   - The word as the trailer's /ID.
//   - An appearance stream drawing the word as a hexadecimal string, on an
//     annotation whose /Contents names an object the file does not define.
func TestWriter_Redact_RefusesCopiesNoDecodedValueSpells(t *testing.T) {
	t.Parallel()
	var flate bytes.Buffer
	zw := zlib.NewWriter(&flate)
	if _, err := zw.Write([]byte("hello world")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	piece := func(stream string) []byte {
		return unresolvableDoc(map[int]string{7: "<< /Type /Filespec /F (a) /EF << /F 8 0 R >> >>", 8: stream},
			"/Names << /EmbeddedFiles << /Names [(a) 7 0 R] >> >>", "", "", nil, "")
	}
	cases := map[string][]byte{
		"flate tail":    piece(rawStream("/Filter /FlateDecode", append(flate.Bytes(), []byte("SECRET")...))),
		"asciihex tail": piece(rawStream("/Filter /ASCIIHexDecode", []byte("68656c6c6f> SECRET"))),
		"destination":   unresolvableDoc(map[int]string{7: "<< /SECRET [3 0 R /Fit] >>"}, "/Dests 7 0 R", "", "", nil, ""),
		"file id":       unresolvableDoc(nil, "", "", "", nil, "/ID [(SECRET) (SECRET)]"),
		"hex appearance": unresolvableDoc(map[int]string{
			7: "<< /Type /Annot /Subtype /FreeText /Rect [300 300 400 320] /Contents 30 0 R /DA (/F1 12 Tf) /AP << /N 8 0 R >> >>",
			8: rawStream("/Type /XObject /Subtype /Form /BBox [0 0 100 20] /Resources 5 0 R", []byte("BT /F1 12 Tf 0 5 Td <534543524554> Tj ET")),
		}, "", "/Annots [7 0 R]", "", nil, ""),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			w := NewWriter(r)
			report, err := w.Redact(ctx, []RedactRegion{coversSecret})
			if !errors.Is(err, ErrRedactionIncomplete) {
				t.Fatalf("err = %v, want ErrRedactionIncomplete", err)
			}
			if report == nil || len(report.SurvivingCopies) == 0 {
				t.Fatalf("the report names no survivor: %+v", report)
			}
		})
	}
}
