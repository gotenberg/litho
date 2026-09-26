package remediation

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/models"
)

// TestRegistryRunsTheContentTierLast pins the order [remediate] runs the
// fixers in: the content tier after every other one, so the registry reads as
// the fixers run.
func TestRegistryRunsTheContentTierLast(t *testing.T) {
	t.Parallel()

	firstContent := ""
	for _, f := range registry {
		if f.safety == safetyContent {
			if firstContent == "" {
				firstContent = f.name
			}
			continue
		}
		if firstContent != "" {
			t.Errorf("%s (%s) is registered after the content-tier fixer %s", f.name, f.safety, firstContent)
		}
	}
}

// hexStringAnnotPDF builds a one-page document with two defects a PDF/A
// remediation repairs in different tiers: a malformed hex string in the page
// content, which the content-tier hex-string fixer respells (object 4), and a
// link annotation with no /F, which the resource-tier annot-flags fixer marks
// printable (object 5). annot-flags was registered after hex-string.
func hexStringAnnotPDF() []byte {
	body := "BT /F1 12 Tf 10 700 Td <414> Tj ET\n"
	return assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Annots [5 0 R] >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(body), body),
		"<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] /Border [0 0 0] >>",
	})
}

// TestRemediateRollbackKeepsTheOtherTiers pins what a writer's rollback to
// [Report.ContentSnapshot] leaves: exactly what a run without the content tier
// writes, and the fixes the report lists outside [Report.ContentFixed]. The
// snapshot used to be taken at the first content-tier fixer in registry order,
// so a rollback also discarded the repairs of every structural and resource
// fixer registered after it while the report still listed them as fixed: here
// the annotation flag annot-flags adds.
func TestRemediateRollbackKeepsTheOtherTiers(t *testing.T) {
	t.Parallel()

	const contentObj, annotObj = 4, 5
	pinned := []byte("0123456789abcdef")
	var contentTier []string
	for _, f := range registry {
		if f.safety == safetyContent {
			contentTier = append(contentTier, f.name)
		}
	}

	for _, profile := range []models.PDFAProfile{models.PDFA1b, models.PDFA2b, models.PDFA4} {
		t.Run(string(profile), func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			doc, alloc := newTestDoc(t, hexStringAnnotPDF())
			rep, err := RemediatePDFA(ctx, doc, alloc, profile, Options{PreserveContent: true, PinnedID: pinned})
			if err != nil {
				t.Fatalf("RemediatePDFA: %v", err)
			}
			if !slices.Contains(rep.ContentFixers, "hex-string") || rep.ContentSnapshot == nil {
				t.Fatalf("ContentFixers = %q, want hex-string among them, with a snapshot", rep.ContentFixers)
			}
			if _, ok := alloc.Pending()[contentObj]; !ok {
				t.Fatal("the run did not respell the hex string")
			}
			alloc.Restore(*rep.ContentSnapshot)
			kept := slices.Sorted(maps.Keys(alloc.Pending()))
			annot, annotKept := alloc.Pending()[annotObj].(ast.PdfDict)
			keptFixed := slices.DeleteFunc(slices.Clone(rep.Fixed), func(d string) bool {
				return slices.Contains(rep.ContentFixed, d)
			})

			doc, alloc = newTestDoc(t, hexStringAnnotPDF())
			off, err := RemediatePDFA(ctx, doc, alloc, profile, Options{PreserveContent: true, PinnedID: pinned}.WithDisabled(contentTier))
			if err != nil {
				t.Fatalf("RemediatePDFA without the content tier: %v", err)
			}
			want := slices.Sorted(maps.Keys(alloc.Pending()))

			if !slices.Equal(kept, want) {
				t.Errorf("the rollback keeps objects %v, want the %v a run without the content tier writes", kept, want)
			}
			if !slices.Equal(keptFixed, off.Fixed) {
				t.Errorf("the rollback keeps fixes %q, want the %q a run without the content tier reports", keptFixed, off.Fixed)
			}
			if slices.Contains(kept, contentObj) {
				t.Error("the rollback kept the content-tier repair of the page content")
			}
			if !annotKept {
				t.Fatal("the rollback discarded the annotation flag repair")
			}
			if _, ok := annot["F"]; !ok {
				t.Errorf("the kept annotation carries no /F: %v", annot)
			}
		})
	}
}
