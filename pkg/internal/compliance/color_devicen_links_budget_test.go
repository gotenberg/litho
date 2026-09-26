package compliance

import (
	"bytes"
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// deviceNLinksPDF builds a one-page PDF whose /Resources/ColorSpace declares
// directly, as /CS0, a DeviceN naming colorants spot colorants and no
// /Colorants, and whose content selects it once and fills with it fills
// times: every f is a link [walkColorLinks] follows to the colour space, which
// veraPDF judges once more for each link to a colour space written directly.
func deviceNLinksPDF(colorants, fills int) []byte {
	var names strings.Builder
	for i := range colorants {
		fmt.Fprintf(&names, "/Spot%d ", i)
	}
	var body strings.Builder
	body.WriteString("/CS0 cs\n")
	for range fills {
		body.WriteString("0 0 1 1 re f\n")
	}
	stream := body.String()
	return cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] "+
			"/Resources << /ColorSpace << /CS0 [/DeviceN ["+names.String()+"] /DeviceGray 5 0 R] >> >> "+
			"/Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
		tintTransformObj,
	)
}

// validateDeviceNLinks validates data at PDF/A-2b and returns the failures of
// the /Colorants rule, ISO 19005-2, 6.2.4.4 t1.
func validateDeviceNLinks(tb testing.TB, data []byte) []Violation {
	tb.Helper()
	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		tb.Fatalf("NewDocument: %v", err)
	}
	viols, err := ValidatePDFA(ctx, doc, models.PDFA2b)
	if err != nil {
		tb.Fatalf("ValidatePDFA: %v", err)
	}
	var missing []Violation
	for _, e := range viols {
		if v, ok := e.(Violation); ok && v.Rule.Clause == "6.2.4.4" && v.Rule.TestNumber == 1 {
			missing = append(missing, v)
		}
	}
	return missing
}

// deviceNLinksAllocBudget bounds what validating [deviceNLinksPDF] of 1000
// colorants and 10000 fills allocates. The walk follows one link per f, so a
// cost linear in the links times the colorants multiplies two things a
// crafted file controls, where one linear in the links alone stays within it.
const deviceNLinksAllocBudget = 128 << 20

// deviceNLinksTimeBudget bounds how long the same validation takes. It is
// generous, since the race detector and a loaded machine slow every walk,
// and the allocation budget is the finer of the two.
const deviceNLinksTimeBudget = 20 * time.Second

// TestDeviceNLinks_CostIsLinearInTheLinks pins that the /Colorants rule, ISO
// 19005-2, 6.2.4.4 t1, costs no more per link than the failure it adds, and
// that the context of each failure names a bounded number of colorants. It
// fails once per link, as veraPDF does, but it listed the spot colorants anew
// at every link: a DeviceN naming 1000 colorants that 100000 operators reach
// cost about 63 s and 5.5 GB, and this fixture, a tenth of it, 905 MB and 2.2
// s, where it now costs 39 MB and 30 ms.
//
// It is not parallel: runtime.ReadMemStats reports TotalAlloc for the whole
// process, and a parallel test would count what the others allocate.
func TestDeviceNLinks_CostIsLinearInTheLinks(t *testing.T) {
	const colorants, fills = 1000, 10000
	data := deviceNLinksPDF(colorants, fills)

	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	start := time.Now()
	missing := validateDeviceNLinks(t, data)
	elapsed := time.Since(start)
	runtime.ReadMemStats(&after)

	// cs and each f link to the colour space.
	if want := fills + 1; len(missing) != want {
		t.Errorf("6.2.4.4 t1 failures = %d, want %d: one per link", len(missing), want)
	}
	for _, v := range missing {
		if want := fmt.Sprintf(" and %d more ", colorants-maxMissingColorantsListed); !strings.Contains(v.Context, want) {
			t.Fatalf("context %.200q does not list %d colorants and count the rest", v.Context, maxMissingColorantsListed)
		}
	}
	if alloc := after.TotalAlloc - before.TotalAlloc; alloc > deviceNLinksAllocBudget {
		t.Errorf("validation allocated %d MB, budget %d MB", alloc>>20, deviceNLinksAllocBudget>>20)
	}
	if elapsed > deviceNLinksTimeBudget {
		t.Errorf("validation took %v, budget %v", elapsed, deviceNLinksTimeBudget)
	}
}

// BenchmarkDeviceNLinks validates a DeviceN naming 1000 spot colorants that
// 10000 fills reach.
func BenchmarkDeviceNLinks(b *testing.B) {
	data := deviceNLinksPDF(1000, 10000)
	b.ReportAllocs()
	for b.Loop() {
		validateDeviceNLinks(b, data)
	}
}
