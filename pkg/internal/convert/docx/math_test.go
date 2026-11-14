package docx

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
)

// TestMath_InlineAndDisplay lays an m:oMath out as a replaced box on its
// line's baseline, and an m:oMathPara as a block of its own justified as
// its m:jc says, each a Formula with the equation's tokens as its
// alternate description (ECMA-376 Part 1, 22.1.2.77 and 22.1.2.78).
func TestMath_InlineAndDisplay(t *testing.T) {
	t.Parallel()
	body := `<w:p><w:r><w:t>Let </w:t></w:r><m:oMath><m:r><w:rPr><w:sz w:val="28"/></w:rPr><m:t>x=1</m:t></m:r></m:oMath></w:p>` +
		`<w:p><m:oMathPara><m:oMathParaPr><m:jc m:val="left"/></m:oMathParaPr><m:oMath><m:f><m:num><m:r><m:t>a</m:t></m:r></m:num>` +
		`<m:den><m:r><m:t>b</m:t></m:r></m:den></m:f></m:oMath></m:oMathPara></w:p>`
	b := build(t, body, nil)
	formulas := role(b.root, "Formula")
	if len(formulas) != 2 {
		t.Fatalf("%d formulas", len(formulas))
	}
	inline := formulas[0]
	if inline.Kind != layout.Replaced || inline.Picture == nil || !inline.Picture.HasBaseline || inline.Alt != "𝑥 = 1" {
		t.Fatalf("inline formula %+v", inline)
	}
	if inline.Style.FontSize != 14 {
		t.Errorf("inline formula at %g points, want its run's 14", inline.Style.FontSize)
	}
	if b.d.formulas == nil {
		t.Fatal("no setter")
	}
	block := find(b.root, func(n *layout.Node) bool {
		return n.Kind == layout.Element && n.Style.Display == css.Block && len(n.Children) == 1 && n.Children[0] == formulas[1]
	})
	if len(block) != 1 || block[0].Style.TextAlign != css.AlignLeft {
		t.Fatalf("display formula not in a left-aligned block: %d", len(block))
	}
	if formulas[1].Alt != "𝑎 𝑏" {
		t.Errorf("alt text %q", formulas[1].Alt)
	}
}
