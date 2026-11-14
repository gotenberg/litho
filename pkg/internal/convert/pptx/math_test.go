package pptx

import (
	"testing"
)

// TestText_DrawsAnEquationAsItsText locks that an Office Math zone a
// paragraph holds outside an mc:AlternateContent (a14:m) is drawn as its
// plain text, in italics, in the run style of its first run.
func TestText_DrawsAnEquationAsItsText(t *testing.T) {
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
	if len(runs) != 1 || runs[0].Text != "a=bc" || !runs[0].Style.Italic || runs[0].Style.Size != 24 {
		t.Errorf("runs %+v, want one italic 24-point run a=bc", runs)
	}
}
