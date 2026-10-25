package pdf

import (
	"context"
	"fmt"
	"math"
	"testing"
)

// stackedPDF is a 400-point square page on a peach tint holding two pieces
// of text each drawn several times over itself:
//
//   - Calcine, light orange, drawn three times a quarter point apart, a
//     fake bold, its first copy before the other lines and the other two
//     after them;
//   - Shadow, black, drawn again in white a point up and to the left;
//   - Twice, black, drawn again where it lies after the other lines, so its
//     second copy hides its first wholly;
//
// and Single, light orange, drawn once, whose ratio Calcine shares.
func stackedPDF() []byte {
	const content = "1 0.85 0.73 rg 0 0 400 400 re f\n" +
		"BT 1 0.6 0.2 rg /F1 12 Tf 20 300 Td (Calcine) Tj ET\n" +
		"BT 0 g /F1 12 Tf 200 300 Td (Twice) Tj ET\n" +
		"BT 0 g /F1 24 Tf 20 200 Td (Shadow) Tj ET\n" +
		"BT 1 g /F1 24 Tf 19 201 Td (Shadow) Tj ET\n" +
		"BT 1 0.6 0.2 rg /F1 12 Tf 20 100 Td (Single) Tj ET\n" +
		"BT 1 0.6 0.2 rg /F1 12 Tf 20.25 300 Td (Calcine) Tj ET\n" +
		"BT 1 0.6 0.2 rg /F1 12 Tf 20.5 300 Td (Calcine) Tj ET\n" +
		"BT 0 g /F1 12 Tf 200 300 Td (Twice) Tj ET\n"
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 400 400] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	})
}

// TestCheckTextContrastStacked judges each piece of text drawn several times
// over itself once, against what lies under the whole stack: the fake bold
// Calcine against the peach page, at Single's ratio, not against its own
// copies at 1:1, the shadowed Shadow by its black copy, and Twice once.
func TestCheckTextContrastStacked(t *testing.T) {
	t.Parallel()
	r := openLimited(t, stackedPDF(), Limits{})

	rep, err := r.CheckTextContrast(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Judged != 4 || rep.Undetermined != 0 {
		t.Errorf("judged %d, undetermined %d; want 4 and 0", rep.Judged, rep.Undetermined)
	}
	got := failuresByText(rep)
	if len(rep.Failures) != 2 {
		t.Fatalf("failures %+v, want Calcine and Single once each", rep.Failures)
	}
	calcine, ok := got["Calcine"]
	single, ok2 := got["Single"]
	if !ok || !ok2 {
		t.Fatalf("failures %v, want Calcine and Single", got)
	}
	if calcine.BackgroundColor != single.BackgroundColor || math.Abs(calcine.Ratio-single.Ratio) > 0.05 || calcine.Ratio < 1.5 {
		t.Errorf("Calcine = %+v, want Single's background and ratio, %+v", calcine, single)
	}
	if calcine.BBox[2]-single.BBox[2] < 0.4 {
		t.Errorf("Calcine box %v, want it to hold every copy, past Single's %v", calcine.BBox, single.BBox)
	}
}

// TestExtractUnseenTextStacked reports no copy of a piece of text drawn
// several times over itself: the page shows each, so none is unseen, though
// its later copies paint over the earlier ones, and Twice's first copy lies
// wholly under its second.
func TestExtractUnseenTextStacked(t *testing.T) {
	t.Parallel()
	r := openLimited(t, stackedPDF(), Limits{})

	got, err := r.ExtractUnseenText(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("reported %+v, want nothing", got)
	}
}

// TestStackRunsSharedPrefix keeps apart two texts set at one place that
// share a few first characters, a year, and nothing else: they are two
// texts, not one drawn twice, while a word drawn again whole joins its copy.
func TestStackRunsSharedPrefix(t *testing.T) {
	t.Parallel()
	const content = "BT 0 g /F1 12 Tf 20 300 Td (2023 Annual Report) Tj ET\n" +
		"BT 0 g /F1 12 Tf 20 200 Td (Between) Tj ET\n" +
		"BT 0.5 g /F1 12 Tf 20 300 Td (2023 Rapport annuel) Tj ET\n" +
		"BT 0.5 g /F1 12 Tf 20.3 200 Td (Between) Tj ET\n"
	data := buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 400 400] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	})
	r := openLimited(t, data, Limits{})
	ctx := context.Background()
	pg, err := r.resolvePage(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	s, err := r.readScene(ctx, pg)
	if err != nil {
		t.Fatal(err)
	}
	runs := contrastRuns(s)
	stacks, err := s.stackRuns(ctx, runs, nil)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, st := range stacks {
		got = append(got, unseenOf(s, runs[st.members[0]], st.glyphs, "").Text+fmt.Sprintf("/%d", len(st.members)))
	}
	want := []string{"2023 Annual Report/1", "Between/2", "2023 Rapport annuel/1"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("stacks %v, want %v", got, want)
	}
}
