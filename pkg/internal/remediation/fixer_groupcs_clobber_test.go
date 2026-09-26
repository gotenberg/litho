package remediation

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/mutations"
	"github.com/gotenberg/litho/pkg/models"
)

// pageGroupPDF builds a two-page document whose first page, object 3, carries
// a transparency group blending in DeviceRGB and paints in DeviceRGB, and
// whose second page, object 4, carries no group. The document has no
// OutputIntent, so at PDF/A-1b the group fails ISO 19005-1, 6.4 t3 and its
// blending space fails 6.2.3.3 t1.
func pageGroupPDF() []byte {
	const content = "1 0 0 rg 20 20 100 100 re f\n"
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 5 0 R"+
			" /Group << /Type /Group /S /Transparency /CS /DeviceRGB >> >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 5 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
	)
}

// matchedBy returns the violations the registered fixer name matches, and
// fails the test when there are none: a fixer that is never planned proves
// nothing about how it composes with its peers.
func matchedBy(t *testing.T, name string, violations []compliance.Violation) (fixer, []compliance.Violation) {
	t.Helper()
	for _, f := range registry {
		if f.name != name {
			continue
		}
		var matched []compliance.Violation
		for _, v := range violations {
			if f.matches(v) {
				matched = append(matched, v)
			}
		}
		if len(matched) == 0 {
			t.Fatalf("precondition: no violation matches %s, so remediation never runs it", name)
		}
		return f, matched
	}
	t.Fatalf("no fixer named %s in the registry", name)
	return fixer{}, nil
}

// livePageGroup returns the /Group entry the page objNum carries once the
// queued repairs apply.
func livePageGroup(alloc *writer.ObjAllocator, doc *document.Document, objNum int) (ast.PdfObject, bool) {
	page := resolveFromAllocOrDoc(context.Background(), alloc, doc, ast.PdfIndirectRef{ObjectNum: objNum})
	group, ok := page["Group"]
	return group, ok
}

// TestFixGroupCS_KeepsThePageGroupTransparencyRemoved pins that group-cs does
// not put back the page transparency group the transparency fixer has just
// removed.
//
// transparency and group-cs are consecutive registry entries, and at PDF/A-1b
// both run on this file: transparency on the 6.4 t3 finding, group-cs on the
// 6.2.3.3 finding against the group's DeviceRGB blending space. transparency
// removes the group, as ISO 19005-1, 6.4 t3 requires. group-cs then found a
// page without a /Group and synthesized one, on both pages, although no
// violation asked for one: the output failed 6.4 t3 again, and only a second
// remediation round removed the groups.
func TestFixGroupCS_KeepsThePageGroupTransparencyRemoved(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, pageGroupPDF())
	errs, err := compliance.ValidatePDFA(ctx, doc, models.PDFA1b)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	violations := toViolations(errs)

	for _, name := range []string{"transparency", "group-cs"} {
		f, matched := matchedBy(t, name, violations)
		if _, err := f.apply(ctx, alloc, doc, Options{}, matched, string(models.PDFA1b)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if name == "transparency" {
			if _, kept := livePageGroup(alloc, doc, 3); kept {
				t.Fatal("precondition: transparency left the page group 6.4 t3 flags")
			}
		}
	}

	for _, objNum := range []int{3, 4} {
		if group, ok := livePageGroup(alloc, doc, objNum); ok {
			t.Errorf("page object %d carries /Group %v after group-cs: ISO 19005-1, 6.4 t3 "+
				"forbids a page transparency group, and no violation asked for one", objNum, group)
		}
	}
}

// TestRemediatePDFA_PageGroupStaysRemovedAfterOneRound pins the whole engine:
// one PDF/A-1b remediation pass leaves no 6.4 finding. Before, the pass left
// 6.4 t3 failing on both pages, and the writer's fixpoint loop needed a second
// round to clear it.
func TestRemediatePDFA_PageGroupStaysRemovedAfterOneRound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, pageGroupPDF())
	if _, err := RemediatePDFA(ctx, doc, alloc, models.PDFA1b, Options{}); err != nil {
		t.Fatalf("RemediatePDFA: %v", err)
	}

	var out bytes.Buffer
	if err := mutations.FullRewrite(ctx, &out, mutations.FullRewriteConfig{Doc: doc, Overrides: alloc.Pending()}); err != nil {
		t.Fatalf("FullRewrite: %v", err)
	}
	reread, err := document.NewDocument(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()), nil)
	if err != nil {
		t.Fatalf("reopening the output: %v", err)
	}
	errs, err := compliance.ValidatePDFA(ctx, reread, models.PDFA1b)
	if err != nil {
		t.Fatalf("ValidatePDFA on the output: %v", err)
	}
	for _, v := range toViolations(errs) {
		if v.Rule.Clause == "6.4" {
			t.Errorf("the output of one round still fails %s t%d [%s]", v.Rule.Clause, v.Rule.TestNumber, v.Context)
		}
	}
}

// transparentPagePDF builds a two-page document with no OutputIntent whose
// first page paints through a graphics state with constant alpha 0.5 and
// carries no /Group, and whose second page paints opaquely. ISO 19005-2,
// 6.2.10 t2 flags the first page alone.
func transparentPagePDF() []byte {
	const transparent = "/GS0 gs 0.5 g 20 20 100 100 re f\n"
	const opaque = "0.5 g 20 20 100 100 re f\n"
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 5 0 R"+
			" /Resources << /ExtGState << /GS0 << /Type /ExtGState /ca 0.5 >> >> >> >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 6 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(transparent), transparent),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(opaque), opaque),
	)
}

// TestFixGroupCS_SynthesizesTheGroupOnTheFlaggedPage is the control: at
// PDF/A-2b, group-cs gives the page 6.2.10 t2 flags a transparency group with
// a blending space, and leaves the page that contains no transparency alone.
func TestFixGroupCS_SynthesizesTheGroupOnTheFlaggedPage(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, transparentPagePDF())
	errs, err := compliance.ValidatePDFA(ctx, doc, models.PDFA2b)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	violations := toViolations(errs)
	var flagged []string
	for _, v := range violations {
		if v.Rule == compliance.Rule_19005_2_6_2_10_t02 {
			flagged = append(flagged, v.Context)
		}
	}
	if len(flagged) != 1 || flagged[0] != "page 1" {
		t.Fatalf("precondition: 6.2.10 t2 flags %q, want page 1 alone", flagged)
	}

	f, matched := matchedBy(t, "group-cs", violations)
	fixes, err := f.apply(ctx, alloc, doc, Options{}, matched, string(models.PDFA2b))
	if err != nil {
		t.Fatalf("group-cs: %v", err)
	}
	if !containsFix(fixes, "normalised /Group on 1 page(s)") {
		t.Errorf("group-cs fixes = %q, want one page normalised", fixes)
	}

	group, ok := livePageGroup(alloc, doc, 3)
	if !ok {
		t.Fatal("the page 6.2.10 t2 flags has no /Group after group-cs")
	}
	groupDict, ok := group.(ast.PdfDict)
	if !ok {
		t.Fatalf("page /Group is %T, want a dictionary", group)
	}
	if cs, ok := groupDict["CS"].(ast.PdfArray); !ok || len(cs) != 2 || cs[0] != ast.PdfName("ICCBased") {
		t.Errorf("page /Group /CS = %v, want [/ICCBased ref]", groupDict["CS"])
	}
	if group, ok := livePageGroup(alloc, doc, 4); ok {
		t.Errorf("the page no violation flags carries /Group %v after group-cs", group)
	}
}

// transparentPagesPDF builds a two-page document with no OutputIntent whose
// pages both paint through a graphics state with constant alpha 0.5. The
// first page, object 3, carries the /Group entry firstGroup, none when it is
// empty; the second, object 4, carries a transparency group blending in
// ICCBased sRGB, object 7.
func transparentPagesPDF(firstGroup string) []byte {
	const content = "/GS0 gs 0.5 g 20 20 100 100 re f\n"
	const resources = " /Resources << /ExtGState << /GS0 << /Type /ExtGState /ca 0.5 >> >> >>"
	if firstGroup != "" {
		firstGroup = " /Group " + firstGroup
	}
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 5 0 R"+resources+firstGroup+" >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 6 0 R"+resources+
			" /Group << /Type /Group /S /Transparency /CS [/ICCBased 7 0 R] >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		fmt.Sprintf("<< /N 3 /Length %d >>\nstream\n%s\nendstream", len(sRGBICCProfile), sRGBICCProfile),
	)
}

// flaggedForGroup validates doc at PDF/A-2b and checks that ISO 19005-2,
// 6.2.10 t2 flags the first page alone.
func flaggedForGroup(t *testing.T, doc *document.Document) []compliance.Violation {
	t.Helper()

	errs, err := compliance.ValidatePDFA(context.Background(), doc, models.PDFA2b)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	violations := toViolations(errs)
	var flagged []string
	for _, v := range violations {
		if v.Rule == compliance.Rule_19005_2_6_2_10_t02 {
			flagged = append(flagged, v.Context)
		}
	}
	if len(flagged) != 1 || flagged[0] != "page 1" {
		t.Fatalf("precondition: 6.2.10 t2 flags %q, want page 1 alone", flagged)
	}
	return violations
}

// blendingSpaceOf returns the /CS of the group page pageNum, counted from 1,
// carries once the queued repairs apply.
func blendingSpaceOf(t *testing.T, alloc *writer.ObjAllocator, doc *document.Document, pageNum int) ast.PdfObject {
	t.Helper()

	ctx := context.Background()
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil || len(pages) < pageNum {
		t.Fatalf("ResolvePages: %d pages, %v", len(pages), err)
	}
	group, ok := livePageDict(ctx, alloc, doc, pages[pageNum-1])["Group"]
	if !ok {
		t.Fatalf("page %d has no /Group", pageNum)
	}
	groupDict, ok := group.(ast.PdfDict)
	if ref, isRef := group.(ast.PdfIndirectRef); isRef {
		groupDict, ok = resolveFromAllocOrDoc(ctx, alloc, doc, ref), true
	}
	if !ok || groupDict == nil {
		t.Fatalf("page %d /Group is %T, want a dictionary", pageNum, group)
	}
	return groupDict["CS"]
}

// TestFixGroupCS_KeepsADeviceIndependentBlendingSpace pins that group-cs keeps
// the ICCBased blending space of a page group no violation names.
//
// ISO 19005-2, 6.2.10 t2 asks the page that contains transparency, in a
// document without a PDF/A OutputIntent, for a group with a /CS, and the
// second page has one. group-cs dropped every /CS that was not a device space,
// so repairing the first page stripped the second page's and left it failing
// 6.2.10 t2, and a later round stripped the sRGB group-cs had given the first.
func TestFixGroupCS_KeepsADeviceIndependentBlendingSpace(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, transparentPagesPDF(""))
	f, matched := matchedBy(t, "group-cs", flaggedForGroup(t, doc))
	if _, err := f.apply(ctx, alloc, doc, Options{}, matched, string(models.PDFA2b)); err != nil {
		t.Fatalf("group-cs: %v", err)
	}

	if cs := blendingSpaceOf(t, alloc, doc, 2); fmt.Sprint(cs) != "[/ICCBased 7 0 R]" {
		t.Errorf("the second page group blends in %v, want the [/ICCBased 7 0 R] it carried", cs)
	}
	if cs := blendingSpaceOf(t, alloc, doc, 1); cs == nil {
		t.Error("the page 6.2.10 t2 flags has a group without a blending space")
	}

	// A second round runs group-cs on the first round's output, whose first
	// page now blends in the sRGB group-cs gave it.
	var out bytes.Buffer
	if err := mutations.FullRewrite(ctx, &out, mutations.FullRewriteConfig{Doc: doc, Overrides: alloc.Pending()}); err != nil {
		t.Fatalf("FullRewrite: %v", err)
	}
	next, nextAlloc := newTestDoc(t, out.Bytes())
	// Stand in for the finding that makes group-cs run again: one naming a
	// device blending space elsewhere in the document.
	device := []compliance.Violation{{Rule: compliance.Rule_19005_2_6_2_4_3_t02,
		Context: "page 9: transparency group blending colour space is DeviceRGB without RGB OutputIntent"}}
	if _, err := fixGroupCS(ctx, nextAlloc, next, Options{}, device, string(models.PDFA2b)); err != nil {
		t.Fatalf("group-cs, second round: %v", err)
	}
	for _, pageNum := range []int{1, 2} {
		if cs := blendingSpaceOf(t, nextAlloc, next, pageNum); cs == nil {
			t.Errorf("page %d lost its blending space on the second round", pageNum)
		}
	}
}

// TestFixGroupCS_GivesAFlaggedGroupItsBlendingSpace pins that group-cs gives a
// blending space to the page group 6.2.10 t2 flags for having none. The fixer
// skipped a group without a /CS, so the page stayed failing.
func TestFixGroupCS_GivesAFlaggedGroupItsBlendingSpace(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, transparentPagesPDF("<< /Type /Group /S /Transparency >>"))
	f, matched := matchedBy(t, "group-cs", flaggedForGroup(t, doc))
	if _, err := f.apply(ctx, alloc, doc, Options{}, matched, string(models.PDFA2b)); err != nil {
		t.Fatalf("group-cs: %v", err)
	}

	cs, ok := blendingSpaceOf(t, alloc, doc, 1).(ast.PdfArray)
	if !ok || len(cs) != 2 || cs[0] != ast.PdfName("ICCBased") {
		t.Errorf("the flagged page group blends in %v, want [/ICCBased ref]", blendingSpaceOf(t, alloc, doc, 1))
	}
}
