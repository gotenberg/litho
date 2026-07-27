package compliance

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// Per ISO 32000-1, 8.4.2 the graphics state includes the text state, and Table
// 104 counts the text font and the text rendering mode among its parameters, so
// Q restores both. Reading Tf as if it outlived the enclosing Q attributed
// character codes to whichever font was selected last, which reported glyphs
// against a font that never showed them.
func TestCollectFontUsageRestoresTextStateOnQ(t *testing.T) {
	tf := func(name string) content.Operation {
		return content.Operation{Operator: "Tf", Operands: []ast.PdfObject{ast.PdfName(name), ast.PdfInt(12)}}
	}
	show := func(s string) content.Operation {
		return content.Operation{Operator: "Tj", Operands: []ast.PdfObject{ast.PdfString(s)}}
	}
	tr := func(mode int) content.Operation {
		return content.Operation{Operator: "Tr", Operands: []ast.PdfObject{ast.PdfInt(mode)}}
	}
	q := content.Operation{Operator: "q"}
	qq := content.Operation{Operator: "Q"}

	t.Run("font selected inside q does not outlive Q", func(t *testing.T) {
		usage := collectFontUsage([]content.Operation{
			tf("F1"), q, tf("F2"), show("A"), qq, show("B"),
		})
		if usage["F2"] == nil || !usage["F2"].charCodes['A'] {
			t.Fatal("code A should belong to F2, the font selected inside the q")
		}
		if usage["F2"].charCodes['B'] {
			t.Error("code B shown after Q must not be attributed to F2")
		}
		if usage["F1"] == nil || !usage["F1"].charCodes['B'] {
			t.Error("Q must restore F1, so code B belongs to it")
		}
	})

	t.Run("rendering mode is restored too", func(t *testing.T) {
		usage := collectFontUsage([]content.Operation{
			tf("F1"), q, tr(3), show("A"), qq, show("B"),
		})
		if usage["F1"] == nil {
			t.Fatal("F1 usage missing")
		}
		if !usage["F1"].visible {
			t.Error("Q restores rendering mode 0, so the text after it is visible")
		}
	})

	t.Run("invisible mode set outside q still applies after Q", func(t *testing.T) {
		usage := collectFontUsage([]content.Operation{
			tf("F1"), tr(3), q, tr(0), qq, show("A"),
		})
		if usage["F1"] == nil {
			t.Fatal("F1 usage missing")
		}
		if usage["F1"].visible {
			t.Error("Q restores rendering mode 3, so the text after it is invisible")
		}
	})

	t.Run("unbalanced Q leaves the state untouched", func(t *testing.T) {
		usage := collectFontUsage([]content.Operation{
			qq, tf("F1"), qq, show("A"),
		})
		if usage["F1"] == nil || !usage["F1"].charCodes['A'] {
			t.Error("a Q with no matching q must not discard the current font")
		}
	})

	t.Run("nested q restores one level at a time", func(t *testing.T) {
		usage := collectFontUsage([]content.Operation{
			tf("F1"), q, tf("F2"), q, tf("F3"), show("A"), qq, show("B"), qq, show("C"),
		})
		for font, code := range map[string]byte{"F3": 'A', "F2": 'B', "F1": 'C'} {
			if usage[font] == nil || !usage[font].charCodes[code] {
				t.Errorf("code %q should belong to %s", code, font)
			}
		}
	})
}
