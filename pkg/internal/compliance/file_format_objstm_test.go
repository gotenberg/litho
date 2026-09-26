package compliance

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"fmt"
	"maps"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// objStmFileNum and objStmXRefNum are the object numbers [objStmDocPDF] gives
// its object stream and its cross-reference stream.
const (
	objStmFileNum = 20
	objStmXRefNum = 21
)

// objStmDocPDF builds and opens a file whose cross-reference stream lists the
// objects plain writes uncompressed and the objects packed stores in one
// object stream (ISO 32000-1, 7.5.7), keyed by object number. Object 1 is the
// catalog, and no key may be objStmFileNum or objStmXRefNum.
func objStmDocPDF(t *testing.T, plain, packed map[int]string) *document.Document {
	t.Helper()
	data := objStmFilePDF(t, plain, packed)
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	if doc.Rebuilt {
		t.Fatal("the test file's cross-reference stream did not parse")
	}
	for num := range packed {
		if entry, ok := doc.XRefEntry(num); !ok || entry.ObjStmNum != objStmFileNum {
			t.Fatalf("object %d's entry is %+v, want it in object stream %d", num, entry, objStmFileNum)
		}
	}
	return doc
}

// objStmFilePDF writes the file [objStmDocPDF] opens.
func objStmFilePDF(t *testing.T, plain, packed map[int]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n")
	offsets := make(map[int]int)
	for _, num := range slices.Sorted(maps.Keys(plain)) {
		offsets[num] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", num, plain[num])
	}

	members := slices.Sorted(maps.Keys(packed))
	var header, body bytes.Buffer
	index := make(map[int]int, len(members))
	for i, num := range members {
		fmt.Fprintf(&header, "%d %d ", num, body.Len())
		body.WriteString(packed[num] + "\n")
		index[num] = i
	}
	var zipped bytes.Buffer
	zw := zlib.NewWriter(&zipped)
	if _, err := zw.Write(append(header.Bytes(), body.Bytes()...)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	offsets[objStmFileNum] = buf.Len()
	fmt.Fprintf(&buf, "%d 0 obj\n<< /Type /ObjStm /N %d /First %d /Filter /FlateDecode /Length %d >>\nstream\n",
		objStmFileNum, len(members), header.Len(), zipped.Len())
	buf.Write(zipped.Bytes())
	buf.WriteString("\nendstream\nendobj\n")

	const size, width = objStmXRefNum + 1, 7 // /W [1 4 2]
	table := make([]byte, size*width)
	for num := range size {
		row := table[num*width : (num+1)*width]
		if off, ok := offsets[num]; ok {
			row[0] = 1
			binary.BigEndian.PutUint32(row[1:], uint32(off)) //nolint:gosec // test offsets are small and positive.
		} else if i, ok := index[num]; ok {
			row[0] = 2
			binary.BigEndian.PutUint32(row[1:], objStmFileNum)
			binary.BigEndian.PutUint16(row[5:], uint16(i)) //nolint:gosec // a test object stream holds a handful of members.
		}
	}
	xrefOff := buf.Len()
	binary.BigEndian.PutUint32(table[objStmXRefNum*width+1:], uint32(xrefOff)) //nolint:gosec // test offsets are small and positive.
	table[objStmXRefNum*width] = 1
	id := "<0123456789ABCDEF0123456789ABCDEF>"
	fmt.Fprintf(&buf, "%d 0 obj\n<< /Type /XRef /Size %d /Root 1 0 R /ID [%s %s] /W [1 4 2] /Length %d >>\nstream\n",
		objStmXRefNum, size, id, id, len(table))
	buf.Write(table)
	fmt.Fprintf(&buf, "\nendstream\nendobj\nstartxref\n%d\n%%%%EOF\n", xrefOff)
	return buf.Bytes()
}

// TestFormatInObjectStreams pins that the members of an object stream answer
// the format tests the parse of an object judges, as an uncompressed object
// does: the hex string tests, ISO 19005-1/2, 6.1.6 t1 and t2, the stream
// keyword test, ISO 19005-1, 6.1.7 t2 and ISO 19005-2, 6.1.7.1 t2, and the
// /Length test, t1 of the same clauses, for a stream with none. ISO 32000-1,
// 7.5.7 forbids a stream in an object stream, and veraPDF judges one all the
// same. Their parse was never format-checked, so every test answered nothing
// there. Measured with veraPDF 1.30 at PDF/A-1b and PDF/A-2b on the same
// files: each count below is veraPDF's, a member no object references
// included, and a property list held in a member fails as often as an
// uncompressed one (see [checkPropertyListHexStrings]).
//
// Each file is judged three ways, with the same counts: freshly opened, a
// second time on the same Document, and after every object was resolved before
// the validation began, as text extraction does.
func TestFormatInObjectStreams(t *testing.T) {
	t.Parallel()

	const (
		pages = "<< /Type /Pages /Kids [3 0 R] /Count 1 >>"
		bdc   = "/Span /P0 BDC EMC"
	)
	catalog := func(extra string) string { return "<< /Type /Catalog /Pages 2 0 R" + extra + " >>" }
	page := func(resources string) string {
		return "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources " + resources + " /Contents 4 0 R >>"
	}
	type counts struct{ odd, invalid, length, keyword int }
	cases := []struct {
		name   string
		plain  map[int]string
		packed map[int]string
		want   counts
	}{
		{
			"catalog odd",
			map[int]string{4: exponentStream("", "")},
			map[int]string{1: catalog(" /Foo <414>"), 2: pages, 3: page("<< >>")},
			counts{odd: 1},
		},
		{
			"catalog non-hex",
			map[int]string{4: exponentStream("", "")},
			map[int]string{1: catalog(" /Foo <41Z4>"), 2: pages, 3: page("<< >>")},
			counts{invalid: 1},
		},
		{
			"catalog both",
			map[int]string{4: exponentStream("", "")},
			map[int]string{1: catalog(" /Foo <414> /Bar <41Z4> /Baz [<41Z> <4142>]"), 2: pages, 3: page("<< >>")},
			counts{odd: 2, invalid: 2},
		},
		{
			"referenced member",
			map[int]string{4: exponentStream("", "")},
			map[int]string{1: catalog(" /Foo 6 0 R"), 2: pages, 3: page("<< >>"), 6: "[<4G> <41>]"},
			counts{invalid: 1},
		},
		{
			"member no object references",
			map[int]string{4: exponentStream("", "")},
			map[int]string{1: catalog(""), 2: pages, 3: page("<< >>"), 6: "<< /A <414> >>"},
			counts{odd: 1},
		},
		{
			"property list member named once",
			map[int]string{4: exponentStream("", bdc)},
			map[int]string{1: catalog(""), 2: pages, 3: page("<< /Properties << /P0 6 0 R >> >>"), 6: "<< /ActualText <414> >>"},
			counts{odd: 3},
		},
		{
			"property list member named twice",
			map[int]string{4: exponentStream("", bdc+" "+bdc)},
			map[int]string{1: catalog(""), 2: pages, 3: page("<< /Properties << /P0 6 0 R >> >>"), 6: "<< /E <41Z4> >>"},
			counts{invalid: 3},
		},
		{
			"property list in a resources member",
			map[int]string{4: exponentStream("", bdc)},
			map[int]string{1: catalog(""), 2: pages, 3: page("7 0 R"), 7: "<< /Properties << /P0 << /ActualText <414> >> >> >>"},
			counts{odd: 3},
		},
		{
			"stream keyword with CR alone",
			map[int]string{4: exponentStream("", "")},
			map[int]string{1: catalog(" /Foo 6 0 R"), 2: pages, 3: page("<< >>"), 6: "<< /Length 3 >>\nstream\rabc\nendstream"},
			counts{keyword: 1},
		},
		{
			"stream with no /Length",
			map[int]string{4: exponentStream("", "")},
			map[int]string{1: catalog(" /Foo 6 0 R"), 2: pages, 3: page("<< >>"), 6: "<< /Foo 1 >>\nstream\nabc\nendstream"},
			counts{length: 1},
		},
		{
			"hex string in a stream dictionary",
			map[int]string{4: exponentStream("", "")},
			map[int]string{1: catalog(" /Foo 6 0 R"), 2: pages, 3: page("<< >>"), 6: "<< /Length 3 /X <414> >>\nstream\nabc\nendstream"},
			counts{odd: 1},
		},
		{
			"clean members",
			map[int]string{4: exponentStream("", bdc)},
			map[int]string{1: catalog(" /Foo 6 0 R"), 2: pages, 3: page("<< /Properties << /P0 7 0 R >> >>"), 6: "<< /A <4142> /B [<41 42>] >>", 7: "<< /ActualText <FEFF0041> >>"},
			counts{},
		},
	}
	for _, tc := range cases {
		for _, p := range []struct {
			profile                          models.PDFAProfile
			odd, invalid, length, keywordEOL Rule
		}{
			{models.PDFA1b, Rule_19005_1_6_1_6_t01, Rule_19005_1_6_1_6_t02, Rule_19005_1_6_1_7_t01, Rule_19005_1_6_1_7_t02},
			{models.PDFA2b, Rule_19005_2_6_1_6_t01, Rule_19005_2_6_1_6_t02, Rule_19005_2_6_1_7_1_t01, Rule_19005_2_6_1_7_1_t02},
		} {
			t.Run(tc.name+" "+string(p.profile), func(t *testing.T) {
				t.Parallel()
				ctx := context.Background()
				check := func(when string, doc *document.Document) {
					t.Helper()
					viols, err := ValidatePDFA(ctx, doc, p.profile)
					if err != nil {
						t.Fatalf("%s: ValidatePDFA: %v", when, err)
					}
					for _, c := range []struct {
						rule Rule
						want int
					}{{p.odd, tc.want.odd}, {p.invalid, tc.want.invalid}, {p.length, tc.want.length}, {p.keywordEOL, tc.want.keyword}} {
						if got := countRuleErrs(t, viols, c.rule); got != c.want {
							t.Errorf("%s: %s test %d fired %d times, want %d: %v", when, c.rule.Clause, c.rule.TestNumber, got, c.want, viols)
						}
					}
				}

				doc := objStmDocPDF(t, tc.plain, tc.packed)
				check("opened", doc)
				check("validated again", doc)

				warmed := objStmDocPDF(t, tc.plain, tc.packed)
				for objNum := range warmed.XRefEntries() {
					if _, err := document.ResolveObject(ctx, warmed, ast.PdfIndirectRef{ObjectNum: objNum}); err != nil {
						t.Fatalf("resolve object %d: %v", objNum, err)
					}
				}
				check("every object resolved first", warmed)
			})
		}
	}
}
