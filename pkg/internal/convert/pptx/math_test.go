package pptx

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// TestText_ReadsAnEquation reads the Office Math zone a paragraph holds
// (a14:m) into a formula of the run style of its first run, its text kept
// for the fit of the body, the paragraph centred as a math paragraph's
// centerGroup justification asks (ECMA-376 Part 1, 22.1.2.51).
func TestText_ReadsAnEquation(t *testing.T) {
	t.Parallel()
	body := `<a:bodyPr/><a:lstStyle/><a:p><a14:m xmlns:a14="http://schemas.microsoft.com/office/drawing/2010/main"><m:oMathPara xmlns:m="http://schemas.openxmlformats.org/officeDocument/2006/math"><m:oMath>` +
		`<m:r><a:rPr lang="en-US" sz="2400" i="1"/><m:t>a=</m:t></m:r><m:f><m:num><m:r><m:t>b</m:t></m:r></m:num><m:den><m:r><m:t>c</m:t></m:r></m:den></m:f>` +
		`</m:oMath></m:oMathPara></a14:m></a:p>`
	s := oneSlide(t, phShape("Equation", "", -1, 0, 0, 914400, 914400, body))
	sh := shapeNamed(s.Shapes, "Equation")
	if sh == nil || sh.Text == nil || len(sh.Text.Paragraphs) != 1 {
		t.Fatalf("shape %+v", sh)
	}
	runs := sh.Text.Paragraphs[0].Runs
	if len(runs) != 1 || runs[0].Text != "a=bc" || runs[0].Style.Size != 24 || runs[0].Formula == nil {
		t.Fatalf("runs %+v, want one 24-point formula run a=bc", runs)
	}
	if st := runs[0].Formula.Style(); st.Size != 24 || !st.Display {
		t.Errorf("formula style %+v", st)
	}
	if got := runs[0].Formula.AltText(); got != "𝑎 = 𝑏 𝑐" {
		t.Errorf("alt text %q", got)
	}
	if sh.Text.Paragraphs[0].Align != css.AlignCenter {
		t.Errorf("paragraph aligned %v", sh.Text.Paragraphs[0].Align)
	}
}

// TestText_TakesTheEquationChoice takes the mc:Choice of an equation's
// shape, which requires the Office 2010 drawing extensions, over its
// Fallback, which holds the equation's picture (ECMA-376 Part 3, 10.2.1).
func TestText_TakesTheEquationChoice(t *testing.T) {
	t.Parallel()
	body := `<a:bodyPr/><a:lstStyle/><a:p><a14:m xmlns:a14="http://schemas.microsoft.com/office/drawing/2010/main"><m:oMath xmlns:m="http://schemas.openxmlformats.org/officeDocument/2006/math">` +
		`<m:r><m:t>x</m:t></m:r></m:oMath></a14:m></a:p>`
	tree := `<mc:AlternateContent xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006">` +
		`<mc:Choice xmlns:a14="http://schemas.microsoft.com/office/drawing/2010/main" Requires="a14">` + phShape("Equation", "", -1, 0, 0, 914400, 914400, body) + `</mc:Choice>` +
		`<mc:Fallback>` + phShape("Picture", "", -1, 0, 0, 914400, 914400, `<a:bodyPr/><a:lstStyle/><a:p/>`) + `</mc:Fallback></mc:AlternateContent>`
	s := oneSlide(t, tree)
	sh := shapeNamed(s.Shapes, "Equation")
	if sh == nil || shapeNamed(s.Shapes, "Picture") != nil {
		t.Fatalf("shapes %+v, want the Choice's", s.Shapes)
	}
	if runs := sh.Text.Paragraphs[0].Runs; len(runs) != 1 || runs[0].Formula == nil {
		t.Fatalf("runs %+v", runs)
	}
}
