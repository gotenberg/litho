package remediation

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
)

// spliceImageData is the data of [spliceImage], between its ID and its EI.
const spliceImageData = " ID \x00\xff)\r EI"

// TestFixAggressive_SpliceKeepsTheUnknownOpsRepair is the clobber guard
// against the real peer that splices the same stream first: unknown-ops
// (registry ahead of aggressive) removes an undefined operator from a page
// holding an inline image, and the aggressive rewrite, sent only the intent
// finding, builds on the spliced stream, so the operator stays removed.
func TestFixAggressive_SpliceKeepsTheUnknownOpsRepair(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc, alloc := newTestDoc(t, exponentPagePDF("q /Foo ri "+spliceImage+" Q zz", "", "", ""))

	if _, err := fixUnknownOps(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixUnknownOps: %v", err)
	}
	if body := string(pendingStreamBytes(t, alloc, 4)); strings.Contains(body, "zz") {
		t.Fatalf("control: unknown-ops did not remove zz: %q", body)
	}

	fixes, err := fixAggressive(ctx, alloc, doc, Options{}, aggressiveIntent, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixAggressive: %v", err)
	}
	if !slices.Equal(fixes, []string{intentReport}) {
		t.Errorf("fixes = %q, want %q", fixes, intentReport)
	}
	if got, want := string(pendingStreamBytes(t, alloc, 4)), "q /RelativeColorimetric ri\n "+spliceImage+" Q "; got != want {
		t.Errorf("page = %q, want %q: the rewrite rebuilt the page from the document", got, want)
	}
}

// TestFixAggressive_SpliceKeepsTheClampedNumbers is the clobber guard against
// the real peer that splices the same stream first: content-limits (registry
// ahead of aggressive) clamps a real past the PDF/A-1 limit on a page holding
// an inline image, and the aggressive rewrite builds on the clamped stream,
// so both repairs land and the image data survives both.
func TestFixAggressive_SpliceKeepsTheClampedNumbers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc, alloc := newTestDoc(t, exponentPagePDF("q 99999.5 w /Foo ri "+spliceImage+" Q", "", "", ""))

	if _, err := fixContentLimits(ctx, alloc, doc, Options{}, nil, "PDF/A-1b"); err != nil {
		t.Fatalf("fixContentLimits: %v", err)
	}
	if body := string(pendingStreamBytes(t, alloc, 4)); !strings.Contains(body, "32767 w") {
		t.Fatalf("control: content-limits did not clamp the width: %q", body)
	}

	violations := []compliance.Violation{{Rule: compliance.Rule_19005_1_6_2_9_t01}}
	if _, err := fixAggressive(ctx, alloc, doc, Options{}, violations, "PDF/A-1b"); err != nil {
		t.Fatalf("fixAggressive: %v", err)
	}
	if got, want := string(pendingStreamBytes(t, alloc, 4)), "q 32767 w /RelativeColorimetric ri\n "+spliceImage+" Q"; got != want {
		t.Errorf("page = %q, want %q", got, want)
	}
}

// TestFixAggressive_SpliceKeepsTheRenamedNames is the clobber guard against
// the real peer that rewrites the same stream first: name-utf8 (registry
// ahead of aggressive) renames an over-long colour space an inline image
// names, and the aggressive rewrite builds on the renamed stream, so both
// repairs land and the image data survives both.
func TestFixAggressive_SpliceKeepsTheRenamedNames(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	stream := fmt.Sprintf("q /Foo ri BI /W 4 /H 1 /BPC 8 /CS %s%s Q", longKeySpelled, spliceImageData)
	doc, alloc := newTestDoc(t, assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /ColorSpace << %s /DeviceGray >> >> >>", longKeySpelled),
		streamBody("", stream),
	}))
	if _, err := fixNameUTF8(ctx, alloc, doc, Options{}, nameLengthViolations, "PDF/A-2b"); err != nil {
		t.Fatalf("fixNameUTF8: %v", err)
	}
	if body := string(pendingStreamBytes(t, alloc, 4)); strings.Contains(body, longKeySpelled) {
		t.Fatalf("control: name-utf8 did not rename the colour space: %q", body)
	}

	fixes, err := fixAggressive(ctx, alloc, doc, Options{}, aggressiveIntent, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixAggressive: %v", err)
	}
	if !slices.Equal(fixes, []string{intentReport}) {
		t.Errorf("fixes = %q, want %q", fixes, intentReport)
	}
	body := string(pendingStreamBytes(t, alloc, 4))
	if strings.Contains(body, longKeySpelled) || !strings.Contains(body, "/RelativeColorimetric ri") || !strings.Contains(body, spliceImageData) {
		t.Errorf("page = %q, want the name renamed, the intent replaced and the image data kept", body)
	}
	if checkResolves(t, alloc, doc, 4, liveResourcesOf(t, alloc, doc, 3)) != 1 {
		t.Fatal("want the inline image's colour space checked")
	}
}
