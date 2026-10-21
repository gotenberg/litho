package recolor

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"math"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/render/page"
)

// fixture is a PDF of pages: object 1 is the catalog, 2 the page tree, and
// each page takes two objects, the page then its content. objs follow them.
type fixture struct {
	// pages holds, per page, the page dictionary's extra entries and its
	// content.
	pages []fixturePage
	objs  []string
}

type fixturePage struct {
	dict, content string
	// contents, when set, is the page's /Contents in place of its own
	// content object.
	contents string
}

// onePage is a fixture of one page, objects 3 and 4, with objs from 5.
func onePage(dict, content string, objs ...string) fixture {
	return fixture{pages: []fixturePage{{dict: dict, content: content}}, objs: objs}
}

func (f fixture) bytes() []byte {
	kids := make([]string, len(f.pages))
	objs := []string{"<< /Type /Catalog /Pages 2 0 R >>", ""}
	for i, p := range f.pages {
		num := 3 + 2*i
		kids[i] = fmt.Sprintf("%d 0 R", num)
		dict := p.dict
		if !strings.Contains(dict, "/MediaBox") {
			dict += " /MediaBox [0 0 40 40]"
		}
		contents := fmt.Sprintf("%d 0 R", num+1)
		if p.contents != "" {
			contents = p.contents
		}
		objs = append(objs,
			fmt.Sprintf("<< /Type /Page /Parent 2 0 R /Contents %s %s >>", contents, dict),
			stream("", p.content))
	}
	objs[1] = fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(f.pages))
	return buildPDF(append(objs, f.objs...))
}

// stream writes a stream object with extra dictionary entries.
func stream(dict, data string) string {
	return fmt.Sprintf("<< %s /Length %d >>\nstream\n%s\nendstream", dict, len(data), data)
}

// buildPDF serialises objs as objects 1 to n with a classic xref table.
func buildPDF(objs []string) []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

// open parses data and returns its pages.
func open(t testing.TB, data []byte) (*document.Document, []*document.Page) {
	t.Helper()
	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil || len(pages) == 0 {
		t.Fatalf("ResolvePages: %d pages, %v", len(pages), err)
	}
	return doc, pages
}

// result is a recoloured fixture: its output reopened and the report.
type result struct {
	t      testing.TB
	data   []byte
	doc    *document.Document
	pages  []*document.Page
	alloc  *writer.ObjAllocator
	report *Report
}

// naive pins the conversions to the naive engine, so values are exact.
func naive(o Options) Options {
	o.Engine = naiveEngine{}
	return o
}

// recolor converts f to target and reopens the output.
func recolor(t testing.TB, f fixture, target Target, opts Options) result {
	t.Helper()
	return recolorBytes(t, f.bytes(), target, opts)
}

// recolorBytes converts the PDF src to target and reopens the output.
func recolorBytes(t testing.TB, src []byte, target Target, opts Options) result {
	t.Helper()
	ctx := context.Background()
	doc, pages := open(t, src)
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
	report, err := Content(ctx, doc, alloc, pages, target, opts)
	if err != nil {
		t.Fatalf("Content: %v", err)
	}
	startXRef, err := document.FindStartXRef(doc.RawReader(), doc.FileSize())
	if err != nil {
		t.Fatalf("FindStartXRef: %v", err)
	}
	trailer := ast.PdfDict{"Root": doc.Index.Trailer["Root"]}
	var out bytes.Buffer
	if err := writer.WriteIncremental(ctx, &out, writer.IncrementalConfig{
		Source:          doc.RawReader(),
		SourceSize:      doc.FileSize(),
		StartXRefOffset: startXRef,
		Allocator:       alloc,
		Trailer:         trailer,
		Generation:      doc.ObjectGeneration,
	}); err != nil {
		t.Fatalf("WriteIncremental: %v", err)
	}
	odoc, opages := open(t, out.Bytes())
	return result{t: t, data: out.Bytes(), doc: odoc, pages: opages, alloc: alloc, report: report}
}

// ops returns the operations of page i's content, as one stream.
func (r result) ops(i int) []content.Operation {
	r.t.Helper()
	ops, err := content.ParsePageContent(context.Background(), r.doc, r.pages[i].Dict, content.WholeStream())
	if err != nil {
		r.t.Fatalf("ParsePageContent: %v", err)
	}
	return ops
}

// text returns page i's content, spelled one operation per line.
func (r result) text(i int) string {
	r.t.Helper()
	var lines []string
	for _, op := range r.ops(i) {
		lines = append(lines, spell(op))
	}
	return strings.Join(lines, "\n")
}

func spell(op content.Operation) string {
	parts := make([]string, 0, len(op.Operands)+1)
	for _, o := range op.Operands {
		switch v := o.(type) {
		case ast.PdfName:
			parts = append(parts, "/"+string(v))
		case ast.PdfReal:
			parts = append(parts, writer.FormatReal(float64(v)))
		default:
			parts = append(parts, fmt.Sprint(v))
		}
	}
	return strings.Join(append(parts, op.Operator), " ")
}

// object resolves object num of the output.
func (r result) object(num int) ast.PdfObject {
	r.t.Helper()
	o, err := document.ResolveObject(context.Background(), r.doc, ast.PdfIndirectRef{ObjectNum: num})
	if err != nil {
		r.t.Fatalf("resolving %d: %v", num, err)
	}
	return o
}

// resolve resolves obj in the output.
func (r result) resolve(obj ast.PdfObject) ast.PdfObject {
	r.t.Helper()
	o, err := document.ResolveObject(context.Background(), r.doc, obj)
	if err != nil {
		r.t.Fatalf("resolving %v: %v", obj, err)
	}
	return o
}

// render draws page i of doc at 72 dpi over white.
func render(t testing.TB, doc *document.Document, pg *document.Page) *image.NRGBA {
	t.Helper()
	c, _, err := page.Render(context.Background(), doc, pg, page.Options{DPI: 72, Background: [4]float32{1, 1, 1, 1}})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	return c.Image()
}

// maxChannelDiff returns the largest difference of one channel between two
// renderings of the same size.
func maxChannelDiff(t testing.TB, a, b *image.NRGBA) int {
	t.Helper()
	if a.Bounds() != b.Bounds() {
		t.Fatalf("renderings of %v and %v", a.Bounds(), b.Bounds())
	}
	worst := 0
	for i := range a.Pix {
		d := int(a.Pix[i]) - int(b.Pix[i])
		worst = max(worst, max(d, -d))
	}
	return worst
}

// near reports two numbers within tol.
func near(a, b, tol float64) bool {
	return math.Abs(a-b) <= tol
}

// cmyk is the DeviceCMYK target with litho's built-in profile.
var cmyk = Target{Family: "DeviceCMYK"}

// rgbTarget is the DeviceRGB target.
var rgbTarget = Target{Family: "DeviceRGB"}

// grayTarget is the DeviceGray target.
var grayTarget = Target{Family: "DeviceGray"}

// dict resolves obj in the output as a dictionary.
func (r result) dict(obj ast.PdfObject) ast.PdfDict {
	r.t.Helper()
	switch v := r.resolve(obj).(type) {
	case ast.PdfDict:
		return v
	case ast.PdfStream:
		return v.Dict
	}
	r.t.Fatalf("%v is no dictionary", obj)
	return nil
}

// decode returns the decoded data of a stream of the output.
func decode(t testing.TB, r result, obj ast.PdfObject) []byte {
	t.Helper()
	st, ok := obj.(ast.PdfStream)
	if !ok {
		st, ok = r.resolve(obj).(ast.PdfStream)
	}
	if !ok {
		t.Fatalf("%v is no stream", obj)
	}
	data, err := r.doc.DecodeStream(context.Background(), st)
	if err != nil {
		t.Fatalf("DecodeStream: %v", err)
	}
	return data
}

// iccRef is the ICCBased colour space over object num.
func iccRef(num int) ast.PdfArray {
	return ast.PdfArray{ast.PdfName("ICCBased"), ast.PdfIndirectRef{ObjectNum: num}}
}
