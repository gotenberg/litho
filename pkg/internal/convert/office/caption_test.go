package office

import (
	"strings"
	"testing"
)

// TestFigureSequence holds the sequences that number figures: any name
// but those of tables, equations and text, without case.
func TestFigureSequence(t *testing.T) {
	for name, want := range map[string]bool{
		"Figure": true, "Illustration": true, "Abbildung": true, "Drawing": true,
		"Table": false, "TABLE": false, "Equation": false, "Text": false, "": false, " ": false,
	} {
		if got := FigureSequence(name); got != want {
			t.Errorf("FigureSequence(%q) = %v, want %v", name, got, want)
		}
	}
}

// TestCaption collapses a caption's white space and bounds it.
func TestCaption(t *testing.T) {
	if got := Caption("  Figure\t1:\n  Sales  "); got != "Figure 1: Sales" {
		t.Errorf("Caption = %q", got)
	}
	if got := Caption(strings.Repeat("é", MaxAltText)); len(got) > MaxAltText || strings.ContainsRune(got, '�') {
		t.Errorf("Caption of %d bytes, want at most %d whole characters", len(got), MaxAltText)
	}
}
