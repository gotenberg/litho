package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// patternTransparencyPDF builds a page whose /Resources declare a tiling
// pattern (object 4), whose OWN /Resources hold the offending /ExtGState
// (object 5). Object 6 is a shading pattern: a plain dictionary with no
// /Resources at all, which the walk must skip rather than trip over.
//
// # Corpus evidence
//
//   - verapdf: the fixtures carry this shape and describe it as "A Type1
//     Pattern uses an ExtGState, which contains the /ca key with value 0.5 and
//     is used for rendering on the page".
func patternTransparencyPDF() []byte {
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] "+
			"/Resources << /Pattern << /P0 4 0 R /P1 6 0 R >> >> >>",
		"<< /Type /Pattern /PatternType 1 /PaintType 1 /TilingType 1 "+
			"/BBox [0 0 1 1] /XStep 1 /YStep 1 "+
			"/Resources << /ExtGState << /GS0 5 0 R >> >> /Length 0 >>\nstream\n\nendstream",
		"<< /Type /ExtGState /ca 0.5 /CA 0.5 /BM /Multiply >>",
		"<< /Type /Pattern /PatternType 2 /Shading << /ShadingType 2 >> >>",
	)
}

func runPatternTransparency(t *testing.T) (*writer.ObjAllocator, []string) {
	t.Helper()

	doc, alloc := newTestDoc(t, patternTransparencyPDF())
	fixes, err := fixTransparency(context.Background(), alloc, doc, Options{}, nil, "PDF/A-1b")
	if err != nil {
		t.Fatalf("fixTransparency: %v", err)
	}
	return alloc, fixes
}

// TestFixTransparency_ReachesAPatternsOwnResources pins the gap this closes.
//
// The walk covered page resources, Form XObject resources and annotation
// appearance resources, but never a pattern's own /Resources, so an /ExtGState
// declared there stayed flagged after remediation. litho's compliance check
// reaches it and reports "[page 1, ExtGState GS0, ca=0.5]", which is why the
// violation survived a repair that was otherwise already written.
func TestFixTransparency_ReachesAPatternsOwnResources(t *testing.T) {
	t.Parallel()

	alloc, fixes := runPatternTransparency(t)

	gs, ok := alloc.Pending()[5].(ast.PdfDict)
	if !ok {
		t.Fatalf("the pattern's ExtGState was not rewritten; fixes=%v", fixes)
	}
	for key, want := range map[ast.PdfName]float64{"ca": 1.0, "CA": 1.0} {
		v, ok := gs[key].(ast.PdfReal)
		if !ok {
			t.Errorf("/%s is %v, want a real number", key, gs[key])
			continue
		}
		if float64(v) != want {
			t.Errorf("/%s = %v, want %v", key, float64(v), want)
		}
	}
	if bm, ok := gs["BM"].(ast.PdfName); !ok || bm != "Normal" {
		t.Errorf("/BM = %v, want /Normal", gs["BM"])
	}
	if len(fixes) == 0 {
		t.Error("the repair reported nothing despite rewriting the ExtGState")
	}
}

// TestFixTransparency_SurvivesAShadingPattern is the negative control for the
// type branch. A shading pattern (PatternType 2) is a plain dictionary with no
// /Resources; resolving it as a stream would fail, so the walk must branch on
// type rather than assume every pattern is a stream.
func TestFixTransparency_SurvivesAShadingPattern(t *testing.T) {
	t.Parallel()

	alloc, _ := runPatternTransparency(t)

	if _, present := alloc.Pending()[6]; present {
		t.Error("the shading pattern was rewritten; it carries no resources to repair")
	}
}

// TestFixPatternTransparency_DeclinesWhenThereIsNoPattern keeps the helper
// honest about resources it should not touch: no /Pattern key means no work,
// and a nil resources dictionary must not panic.
func TestFixPatternTransparency_DeclinesWhenThereIsNoPattern(t *testing.T) {
	t.Parallel()

	doc, alloc := newTestDoc(t, patternTransparencyPDF())
	if n := fixPatternTransparency(context.Background(), doc, alloc, nil, make(map[int]bool), 0); n != 0 {
		t.Errorf("nil resources fixed %d, want 0", n)
	}
	if n := fixPatternTransparency(context.Background(), doc, alloc, ast.PdfDict{}, make(map[int]bool), 0); n != 0 {
		t.Errorf("empty resources fixed %d, want 0", n)
	}
	if n := fixPatternTransparency(context.Background(), doc, alloc,
		ast.PdfDict{"XObject": ast.PdfDict{}}, make(map[int]bool), 0); n != 0 {
		t.Errorf("resources without /Pattern fixed %d, want 0", n)
	}
}

// TestMatchTransparency_StillScopedToPartOne guards the matcher the new walk
// runs under. ISO 19005-1 forbids transparency outright; parts 2 and later
// permit it, so widening this claim would strip alpha from files that are
// allowed to keep it.
func TestMatchTransparency_StillScopedToPartOne(t *testing.T) {
	t.Parallel()

	if !matchTransparency(compliance.Violation{Rule: compliance.Rule{
		Specification: "ISO_19005_1", Clause: "6.4", TestNumber: 6,
	}}) {
		t.Error("must claim ISO 19005-1 6.4 t6")
	}
	for _, spec := range []string{"ISO_19005_2", "ISO_19005_3", "ISO_19005_4"} {
		if matchTransparency(compliance.Violation{Rule: compliance.Rule{
			Specification: spec, Clause: "6.4", TestNumber: 6,
		}}) {
			t.Errorf("must not claim %s 6.4 t6: parts 2+ permit transparency", spec)
		}
	}
}
