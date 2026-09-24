package typography

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// The assertions below count map entries rather than bytes allocated.
// runtime.ReadMemStats reports TotalAlloc for the whole PROCESS, so inside a
// t.Parallel() test the delta records everything the package allocated during
// the window, not the call under test: an earlier version of this file asserted
// a 32 MB ceiling and reported 35.4 MB and 35.5 MB for two calls whose real
// costs differ threefold. The entry count is exact, attributable, and
// discriminates just as sharply, which a planted revert confirms: removing the
// clamp turns 65,536 into 5,000,001.

// TestParseCIDWidths_RefusesARangePastTheCIDSpace pins that the /W range form
// is bounded by the 16-bit CID space before it expands.
//
// Each iteration inserts a map entry, so the declared end alone decides how
// much gets built. Measured before the bound, in isolation: endCID 5,000,000
// produced 5,000,001 entries costing 288.6 MB, and the 2147483647 a file may
// state extrapolates past 100 GB from an array of roughly 30 bytes. The walk is
// reached from Reader.ExtractText by way of [ResolveFont].
func TestParseCIDWidths_RefusesARangePastTheCIDSpace(t *testing.T) {
	t.Parallel()

	arr := ast.PdfArray{ast.PdfInt(0), ast.PdfInt(5_000_000), ast.PdfReal(500)}
	if got := len(parseCIDWidths(arr)); got != maxCIDValue+1 {
		t.Errorf("entries = %d, want %d: the /W range is not bounded by the CID space",
			got, maxCIDValue+1)
	}
}

// TestParseCIDVerticalWidths_RefusesARangePastTheCIDSpace is the same for /W2,
// which stores [3]float64 per CID and so cost 640.6 MB at the same range.
func TestParseCIDVerticalWidths_RefusesARangePastTheCIDSpace(t *testing.T) {
	t.Parallel()

	arr := ast.PdfArray{
		ast.PdfInt(0), ast.PdfInt(5_000_000),
		ast.PdfReal(500), ast.PdfReal(0), ast.PdfReal(-1000),
	}
	if got := len(parseCIDVerticalWidths(arr)); got != maxCIDValue+1 {
		t.Errorf("entries = %d, want %d: the /W2 range is not bounded by the CID space",
			got, maxCIDValue+1)
	}
}

// TestParseCIDWidths_CoversTheWholeCIDSpace is the control. A bound that
// refused legitimate ranges would satisfy the tests above while silently
// dropping the widths of every CID a real font declares.
func TestParseCIDWidths_CoversTheWholeCIDSpace(t *testing.T) {
	t.Parallel()

	arr := ast.PdfArray{ast.PdfInt(0), ast.PdfInt(maxCIDValue), ast.PdfReal(500)}
	widths := parseCIDWidths(arr)

	if got := len(widths); got != maxCIDValue+1 {
		t.Fatalf("entries = %d, want %d: the bound rejects the legal CID space", got, maxCIDValue+1)
	}
	if widths[maxCIDValue] != 500 {
		t.Errorf("CID %d width = %v, want 500: the last legal CID was dropped",
			maxCIDValue, widths[maxCIDValue])
	}
}

// TestParseCIDVerticalWidths_CoversTheWholeCIDSpace is the matching control.
func TestParseCIDVerticalWidths_CoversTheWholeCIDSpace(t *testing.T) {
	t.Parallel()

	arr := ast.PdfArray{
		ast.PdfInt(0), ast.PdfInt(maxCIDValue),
		ast.PdfReal(500), ast.PdfReal(0), ast.PdfReal(-1000),
	}
	metrics := parseCIDVerticalWidths(arr)

	if got := len(metrics); got != maxCIDValue+1 {
		t.Fatalf("entries = %d, want %d: the bound rejects the legal CID space", got, maxCIDValue+1)
	}
	if metrics[maxCIDValue] != [3]float64{500, 0, -1000} {
		t.Errorf("CID %d metrics = %v, want [500 0 -1000]", maxCIDValue, metrics[maxCIDValue])
	}
}

// TestParseCIDWidths_NegativeStartIsBounded covers the other end. Clamping only
// the range end still leaves a start of -2147483648 walking billions of values,
// so the start needs bounding too.
//
// The start here is -5,000,000 rather than the -2147483648 a file may state, so
// that removing the clamp to check this test still discriminates costs about
// 5 million map entries instead of two billion. A guard whose absence cannot be
// survived cannot be demonstrated.
func TestParseCIDWidths_NegativeStartIsBounded(t *testing.T) {
	t.Parallel()

	arr := ast.PdfArray{ast.PdfInt(-5_000_000), ast.PdfInt(maxCIDValue), ast.PdfReal(500)}
	if got := len(parseCIDWidths(arr)); got != maxCIDValue+1 {
		t.Errorf("entries = %d, want %d: a negative range start is unbounded", got, maxCIDValue+1)
	}
}

// TestParseCIDWidths_IndividualFormUnaffected pins that the other /W form is
// untouched: it is bounded by the array it reads, never by a declared range, so
// the new clamp must not change it.
func TestParseCIDWidths_IndividualFormUnaffected(t *testing.T) {
	t.Parallel()

	arr := ast.PdfArray{ast.PdfInt(10), ast.PdfArray{ast.PdfReal(100), ast.PdfReal(200), ast.PdfReal(300)}}
	widths := parseCIDWidths(arr)

	if len(widths) != 3 {
		t.Fatalf("entries = %d, want 3: %v", len(widths), widths)
	}
	for cid, want := range map[int]float64{10: 100, 11: 200, 12: 300} {
		if widths[cid] != want {
			t.Errorf("CID %d width = %v, want %v", cid, widths[cid], want)
		}
	}
}
