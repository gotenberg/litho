package remediation

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// markedClosePDF builds a two-page document whose pages draw the content
// streams bodies holds from object 5 on. contents1 and contents2 are the pages'
// /Contents values.
func markedClosePDF(contents1, contents2 string, bodies ...string) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents " + contents1 + " >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents " + contents2 + " >>",
	}
	for _, b := range bodies {
		if strings.HasPrefix(b, "[") {
			objs = append(objs, b)
			continue
		}
		objs = append(objs, "<< /Length "+strconv.Itoa(len(b))+" >>\nstream\n"+b+"\nendstream")
	}
	return buildStructPDF(objs...)
}

// TestFixMarkedContentClose pins that the sequences a page's content leaves
// open are closed (ISO 32000-1, 14.6) in new objects the page names, with every
// source stream and shared array left as written, and that the EMCs go before
// a string the content never terminates (7.3.4.2).
func TestFixMarkedContentClose(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const (
		closed = "/P <</MCID 0>> BDC 0 0 m 1 1 l S EMC\n"
		open   = "/Artifact BMC\n20 90 m 180 90 l S"
	)
	for _, tc := range []struct {
		name               string
		contents1          string
		contents2          string
		bodies             []string
		want1, want2       string
		fixed              int
		rewritesFromSource bool
	}{
		{
			name:      "one sequence left open",
			contents1: "5 0 R", contents2: "6 0 R",
			bodies: []string{closed + open, closed},
			want1:  "BDC m l S EMC BMC m l S EMC", want2: "BDC m l S EMC",
			fixed: 1,
		},
		{
			name:      "two sequences left open",
			contents1: "5 0 R", contents2: "6 0 R",
			bodies: []string{"/Artifact BMC /Span BMC 0 0 m S", closed},
			want1:  "BMC BMC m S EMC EMC", want2: "BDC m l S EMC",
			fixed: 1,
		},
		{
			name:      "a stray EMC opens nothing",
			contents1: "5 0 R", contents2: "6 0 R",
			bodies: []string{"EMC 0 0 m S", closed},
			want1:  "EMC m S", want2: "BDC m l S EMC",
		},
		{
			name:      "an opener spanning the streams of an array",
			contents1: "[5 0 R 6 0 R]", contents2: "6 0 R",
			bodies: []string{"/Artifact BMC 0 0 m S", "EMC " + open},
			want1:  "BMC m S EMC BMC m l S EMC", want2: "EMC BMC m l S EMC",
			fixed: 2,
		},
		{
			name:      "an indirect array two pages share",
			contents1: "7 0 R", contents2: "7 0 R",
			bodies: []string{closed, open, "[5 0 R 6 0 R]"},
			want1:  "BDC m l S EMC BMC m l S EMC", want2: "BDC m l S EMC BMC m l S EMC",
			fixed: 2,
		},
		{
			name:      "a stream two pages share",
			contents1: "5 0 R", contents2: "5 0 R",
			bodies: []string{open},
			want1:  "BMC m l S EMC", want2: "BMC m l S EMC",
			fixed: 2,
		},
		{
			name:      "a stream two pages share ending inside a string",
			contents1: "5 0 R", contents2: "5 0 R",
			bodies: []string{open + "\n(dangling ( string"},
			want1:  "BMC m l S EMC", want2: "BMC m l S EMC",
			fixed: 2, rewritesFromSource: true,
		},
		{
			name:      "a stream ending inside a string it never terminates",
			contents1: "5 0 R", contents2: "6 0 R",
			bodies: []string{open + "\n(dangling ( string", closed},
			want1:  "BMC m l S EMC", want2: "BDC m l S EMC",
			fixed: 1, rewritesFromSource: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, alloc := newTestDoc(t, markedClosePDF(tc.contents1, tc.contents2, tc.bodies...))
			descs, err := fixMarkedContentClose(ctx, alloc, doc, Options{}, nil, "PDF/UA-1")
			if err != nil {
				t.Fatalf("fixMarkedContentClose: %v", err)
			}
			if got := len(descs); (got == 1) != (tc.fixed > 0) {
				t.Fatalf("descriptions = %v, want a fix on %d page(s)", descs, tc.fixed)
			}
			if tc.fixed > 0 && !strings.Contains(descs[0], strconv.Itoa(tc.fixed)+" page(s)") {
				t.Errorf("description %q, want %d page(s)", descs[0], tc.fixed)
			}
			// No source stream or shared array is written: the repair lives in
			// new objects the page names.
			for objNum := 5; objNum < 5+len(tc.bodies); objNum++ {
				if _, written := alloc.Pending()[objNum]; written {
					t.Errorf("object %d, a source stream or array, was rewritten", objNum)
				}
			}
			for objNum, want := range map[int]string{3: tc.want1, 4: tc.want2} {
				dict := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: objNum})
				data, err := liveContentBytes(ctx, alloc, doc, dict["Contents"])
				if err != nil {
					t.Fatalf("page %d content: %v", objNum, err)
				}
				ops, err := content.ParseStreamData(ctx, data)
				if err != nil {
					t.Fatalf("page %d parse: %v", objNum, err)
				}
				got := make([]string, 0, len(ops))
				for _, op := range ops {
					got = append(got, op.Operator)
				}
				if strings.Join(got, " ") != want {
					t.Errorf("page %d reads %q, want %q", objNum, strings.Join(got, " "), want)
				}
				if tc.contents1 == tc.contents2 {
					other := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 7 - objNum})
					mine, isRef := dict["Contents"].(ast.PdfIndirectRef)
					if theirs, ok := other["Contents"].(ast.PdfIndirectRef); !isRef || !ok || mine != theirs {
						t.Errorf("page %d names /Contents %v, the page it shares content with %v", objNum, dict["Contents"], other["Contents"])
					}
				}
				if _, open := content.UnclosedMarkedContent(ops); open != 0 {
					t.Errorf("page %d leaves %d sequence(s) open", objNum, open)
				}
				if tc.rewritesFromSource && objNum == 3 {
					if _, isRef := dict["Contents"].(ast.PdfIndirectRef); !isRef {
						t.Errorf("page %d /Contents = %T, want one rewritten stream", objNum, dict["Contents"])
					}
				}
			}
		})
	}
}
