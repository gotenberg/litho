package compliance

import (
	"fmt"
	"testing"
)

// patSelectionPDF declares Separation /Spot twice, once on the page as /CS0 and
// once inside a tiling pattern as /CS1, with differing tint transforms. content
// decides how, or whether, the page selects the pattern.
func patSelectionPDF(content string) []byte {
	const cell = "/CS1 cs 1 scn 0 0 1 1 re f"
	pattern := fmt.Sprintf("<< /Type /Pattern /PatternType 1 /PaintType 1 /TilingType 1 "+
		"/BBox [0 0 1 1] /XStep 1 /YStep 1 /Resources << /ColorSpace << /CS1 8 0 R >> >> "+
		"/Length %d >>\nstream\n%s\nendstream", len(cell), cell)

	return cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources "+
			"<< /ColorSpace << /CS0 5 0 R >> /Pattern << /P0 6 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"[/Separation /Spot /DeviceRGB 7 0 R]",
		pattern,
		sepTintA,
		"[/Separation /Spot /DeviceRGB 9 0 R]",
		sepTintB,
	)
}

// TestPatternNeedsPatternSpaceToBeSelected pins that a scn name operand selects
// a pattern only when a Pattern colour space is current.
//
// ISO 32000-1 §8.6.8 and Table 74: scn sets components within the space cs
// already selected, and only the Pattern form takes a name. A name written with
// no Pattern space selected is malformed and paints nothing. Measured against
// veraPDF 1.30.2, which reports nothing for such a pattern and reports it once
// the space is selected. pdfjs issue18466.pdf is the real instance: it writes
// "/P12 scn /P12 SCN" with no /Pattern cs anywhere on the page.
func TestPatternNeedsPatternSpaceToBeSelected(t *testing.T) {
	t.Parallel()

	bare := patSelectionPDF("/CS0 cs 1 scn 0 0 10 10 re f /P0 scn 20 0 10 10 re f")
	if firesRule2b(t, bare, "6.2.4.4", 2) {
		t.Error("a scn name with no Pattern space selected must not make the pattern count as painted")
	}

	selected := patSelectionPDF("/CS0 cs 1 scn 0 0 10 10 re f /Pattern cs /P0 scn 20 0 10 10 re f")
	if !firesRule2b(t, selected, "6.2.4.4", 2) {
		t.Error("a pattern selected by /Pattern cs then scn must be judged")
	}
}

// TestPatternSelectionIsPerSide pins that fill and stroke selection are tracked
// separately: scn sets the fill colour and SCN the stroke colour, so selecting
// /Pattern for one side does not make a name on the other side a pattern use.
func TestPatternSelectionIsPerSide(t *testing.T) {
	t.Parallel()

	strokeOnly := patSelectionPDF("/CS0 cs 1 scn 0 0 10 10 re f /Pattern CS /P0 SCN 2 w 0 0 m 20 20 l S")
	if !firesRule2b(t, strokeOnly, "6.2.4.4", 2) {
		t.Error("a pattern selected by /Pattern CS then SCN must be judged")
	}

	// /Pattern selected for stroking, but the name given to the fill operator.
	crossed := patSelectionPDF("/CS0 cs 1 scn 0 0 10 10 re f /Pattern CS /P0 scn 20 0 10 10 re f")
	if firesRule2b(t, crossed, "6.2.4.4", 2) {
		t.Error("a scn name must not count while only the stroke space is a Pattern space")
	}
}

// TestPatternSelectionRestoredByQ pins that Q restores the selected space, so a
// pattern named after the restore is judged by the state that q saved.
//
// The graphics state stack carries the current colour space (ISO 32000-1
// §8.4.2), so a /Pattern cs inside q ... Q does not survive the restore.
func TestPatternSelectionRestoredByQ(t *testing.T) {
	t.Parallel()

	data := patSelectionPDF("/CS0 cs 1 scn q /Pattern cs Q /P0 scn 20 0 10 10 re f")
	if firesRule2b(t, data, "6.2.4.4", 2) {
		t.Error("Q must restore the previous colour space, leaving the later scn name no Pattern space")
	}
}
