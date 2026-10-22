package pdf

import (
	"context"
	"fmt"
	"testing"
)

func TestRenderPage_OverprintSimulation(t *testing.T) {
	t.Parallel()
	// Magenta under overprint mode 1 over cyan (ISO 32000-1, 8.6.7): the
	// simulation keeps the cyan, which shows as blue; without it magenta
	// knocks the cyan out.
	const content = "1 0 0 0 k 0 0 40 40 re f /OP gs 0 1 0 0 k 0 0 40 40 re f"
	r := openLimited(t, buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 40 40] /Contents 4 0 R /Resources << /ExtGState << /OP 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /ExtGState /OP true /OPM 1 >>",
	}), Limits{})
	ctx := context.Background()
	plain, err := r.RenderPage(ctx, 1, WithRenderDPI(72))
	if err != nil {
		t.Fatal(err)
	}
	simulated, err := r.RenderPage(ctx, 1, WithRenderDPI(72), WithOverprintSimulation())
	if err != nil {
		t.Fatal(err)
	}
	if c := nrgbaAt(t, plain, 20, 20); c.R < 128 {
		t.Errorf("without simulation %v, want magenta", c)
	}
	if c := nrgbaAt(t, simulated, 20, 20); c.R >= 128 || c.B < 128 {
		t.Errorf("with simulation %v, want cyan and magenta, blue", c)
	}
}
