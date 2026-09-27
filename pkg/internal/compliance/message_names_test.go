package compliance

import (
	"context"
	"go/ast"
	"go/constant"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"strings"
	"testing"

	pdfast "github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/models"
)

// TestFormatQuoteNeverTakesPdfName pins that no violation message formats a
// PDF name with %q. [pdfast.PdfName.String] writes the solidus, and %q goes
// through String, so a MIME type read as "/text/plain" and a missing entry,
// which resolves to the empty name, as "/", a value it does not have. A
// message quotes string(name) instead.
//
// The guard type-checks the package from source and inspects every call whose
// format argument is a constant, so it states a lower bound on what it saw: a
// failed type check leaves every operand untyped and would pass silently.
func TestFormatQuoteNeverTakesPdfName(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	var files []*ast.File
	for _, p := range paths {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", p, err)
		}
		files = append(files, f)
	}

	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}}
	conf := types.Config{Importer: importer.ForCompiler(fset, "source", nil)}
	if _, err := conf.Check("github.com/gotenberg/litho/pkg/internal/compliance", fset, files, info); err != nil {
		t.Fatalf("type-check: %v", err)
	}

	var formatCalls, nameOperands int
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sig, ok := info.Types[call.Fun].Type.(*types.Signature)
			if !ok || !sig.Variadic() || sig.Params().Len() < 2 {
				return true
			}
			fi := sig.Params().Len() - 2
			if len(call.Args) <= fi {
				return true
			}
			format := info.Types[call.Args[fi]].Value
			if format == nil || format.Kind() != constant.String {
				return true
			}
			formatCalls++
			args := call.Args[fi+1:]
			for i, verb := range formatVerbs(constant.StringVal(format)) {
				if i >= len(args) || !isPdfNameType(info.Types[args[i]].Type) {
					continue
				}
				nameOperands++
				if verb == 'q' {
					t.Errorf("%s: %%q formats the PdfName %s; quote string(%s)",
						fset.Position(args[i].Pos()), types.ExprString(args[i]), types.ExprString(args[i]))
				}
			}
			return true
		})
	}

	// Lower bounds well under what the package holds (over 1000 format calls,
	// about 90 of them formatting a PdfName with %s or %v), so a guard that
	// stopped seeing calls or types fails instead of passing.
	if formatCalls < 500 || nameOperands < 50 {
		t.Fatalf("guard saw %d format calls and %d PdfName operands; it no longer inspects the package", formatCalls, nameOperands)
	}
}

func isPdfNameType(t types.Type) bool {
	named, ok := t.(*types.Named)
	return ok && named.Obj().Name() == "PdfName" && named.Obj().Pkg() != nil &&
		named.Obj().Pkg().Path() == "github.com/gotenberg/litho/pkg/internal/core/ast"
}

// formatVerbs returns the verb of each operand-consuming directive in a fmt
// format string, in order.
func formatVerbs(format string) []rune {
	var verbs []rune
	for i := 0; i < len(format); i++ {
		if format[i] != '%' {
			continue
		}
		i++
		for i < len(format) && strings.IndexByte("+-# 0123456789.", format[i]) >= 0 {
			i++
		}
		if i >= len(format) {
			break
		}
		if format[i] != '%' {
			verbs = append(verbs, rune(format[i]))
		}
	}
	return verbs
}

// TestEmbeddedSubtypeMessageNamesTheDefect pins the /Subtype message of ISO
// 19005-3, 6.8 t1 and ISO 19005-4, 6.9 t1: an absent entry reads as missing,
// a name is quoted without its solidus, and the PDF/A-4 message names the file
// specification as the PDF/A-3 one does. A missing /Subtype printed as "/", a
// value the stream does not have.
func TestEmbeddedSubtypeMessageNamesTheDefect(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	for _, tc := range []struct {
		name, stream, want string
	}{
		{"missing", "<< /Type /EmbeddedFile /Length 4 >>", `object 4: embedded file /Subtype is missing`},
		{"null", "<< /Type /EmbeddedFile /Subtype null /Length 4 >>", `object 4: embedded file /Subtype is missing`},
		{"string", "<< /Type /EmbeddedFile /Subtype (text/plain) /Length 4 >>", `object 4: embedded file /Subtype is not a name`},
		{"invalid", "<< /Type /EmbeddedFile /Subtype /plain /Length 4 >>", `object 4: embedded file /Subtype "plain" is not a valid MIME type`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, catalog := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
				"<< /Type /Filespec /F (a.txt) /UF (a.txt) /AFRelationship /Data /EF << /F 5 0 R >> >>",
				tc.stream+"\nstream\ndata\nendstream",
			)
			for _, got := range []Violation{
				onlyContext(t, checkEmbeddedSubtypeMIME(ctx, doc, collectFileSpecs(ctx, doc, catalog)[0])),
				onlyContext(t, checkEmbeddedFileMIME(ctx, doc, catalog)),
			} {
				if got.Context != tc.want {
					t.Errorf("clause %s: context = %q, want %q", got.Rule.Clause, got.Context, tc.want)
				}
			}
		})
	}
}

// TestInlineEmbeddedFileNotPDFNamesItsContainer pins that ISO 19005-4, 6.9 t3
// names a file specification written inline by the object holding it and its
// path there, as every other clause 6.9 message does. It cited the object
// number of the file specification itself, which an inline one does not have,
// and so read "object 0", an object no file holds.
func TestInlineEmbeddedFileNotPDFNamesItsContainer(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	doc, catalog := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] /F 4"+
			" /AF [<< /Type /Filespec /F (a.txt) /UF (a.txt) /AFRelationship /Data /EF << /F 5 0 R >> >>] >>",
		"<< /Type /EmbeddedFile /Subtype /text#2Fplain /Length 4 >>\nstream\ndata\nendstream",
	)

	var embedded []fileSpecEntry
	for _, fs := range collectFileSpecs(ctx, doc, catalog) {
		if fs.hasEF {
			embedded = append(embedded, fs)
		}
	}
	got := onlyContext(t, checkEmbeddedFileCompliance(ctx, doc, embedded, Rule_19005_4_6_9_t03,
		[]models.PDFAProfile{models.PDFA4, models.PDFA2b, models.PDFA1b}))
	if want := "object 4 /AF[0]: embedded file is not a PDF document"; got.Context != want {
		t.Errorf("context = %q, want %q", got.Context, want)
	}
}

// TestNameMessagesQuoteTheName pins the messages that quote a name the file
// may leave out or write as another type: a missing entry reads as missing and
// a name is quoted without its solidus, where %q of the resolved PdfName
// printed "/" for both.
func TestNameMessagesQuoteTheName(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc := nopDoc()

	threeD := func(streamDict string) string {
		doc, _ := objsDocPDF(t,
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [] /Count 0 >>",
			streamDict+"\nstream\ndata\nendstream",
		)
		return onlyContext(t, check3DStreamSubtype(ctx, doc, pdfast.PdfDict{"3DD": pdfast.PdfIndirectRef{ObjectNum: 3}}, "annot")).Context
	}
	intent := func(value pdfast.PdfObject) string {
		img := pdfast.PdfDict{"Intent": value, "BitsPerComponent": pdfast.PdfInt(8)}
		for _, v := range checkImageXObject(ctx, doc, img, "Im1", 1, 2, false) {
			if v.Rule == renderingIntentRule(2) {
				return v.Context
			}
		}
		t.Fatalf("no rendering intent violation for /Intent %v", value)
		return ""
	}

	for _, tc := range []struct{ got, want string }{
		{threeD("<< /Length 4 >>"), "annot: 3D stream /Subtype is missing (must be U3D or PRC)"},
		{threeD("<< /Length 4 /Subtype /X3D >>"), `annot: 3D stream Subtype="X3D" (must be U3D or PRC)`},
		{threeD("<< /Length 4 /Subtype (U3D) >>"), "annot: 3D stream /Subtype is not a name (must be U3D or PRC)"},
		{intent(pdfast.PdfName("Vivid")), `page 1, XObject /Im1: invalid rendering intent "Vivid"`},
		{intent(pdfast.PdfNull{}), "page 1, XObject /Im1: /Intent is null, not a rendering intent"},
		{intent(pdfast.PdfInt(1)), "page 1, XObject /Im1: /Intent is not a name"},
	} {
		if tc.got != tc.want {
			t.Errorf("context = %q, want %q", tc.got, tc.want)
		}
	}
}

// onlyContext returns the one violation vs holds, failing the test otherwise.
func onlyContext(t *testing.T, vs []Violation) Violation {
	t.Helper()
	if len(vs) != 1 {
		t.Fatalf("got %d violations, want 1: %v", len(vs), vs)
	}
	return vs[0]
}
