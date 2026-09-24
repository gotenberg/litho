package remediation

import (
	"context"
	"maps"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// deviceNInlineColourSpacePDF builds a one-page document whose /ColorSpace
// DICTIONARY is indirect at object 5, while the DeviceN array inside it is
// INLINE.
//
// That pairing is the only shape that reaches the branch under test.
// [trimDeviceNColorants] writes at ref.ObjectNum when the array itself is
// indirect, and otherwise collects the trimmed array under its colour-space
// name and writes the parent dictionary at csRef.ObjectNum. So the array must
// be inline to skip the first write, and the dictionary must be indirect for
// the second to fire at all.
//
// Nine colorants, for the same reason as the sibling fixture: the trim skips
// any name list `<= pdfADeviceNComponents`.
func deviceNInlineColourSpacePDF() []byte {
	return assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792]" +
			" /Resources << /ColorSpace 5 0 R >> >>",
		"<< /Type /Nothing >>",
		"<< /CSN [/DeviceN [/Cyan /Magenta /Yellow /Black /Spot1 /Spot2 /Spot3 /Spot4 /Spot5]" +
			" /DeviceCMYK 6 0 R] >>",
		"<< /FunctionType 2 /Domain [0 1] /C0 [0 0 0 0] /C1 [0.1 0 0 0] /N 1 >>",
	})
}

// TestTrimDeviceNColorants_InlineColourSpaceIsTrimmedAndKeepsAPeerWrite pins
// the inline branch of the 8-component trim, which writes the PARENT
// /ColorSpace dictionary instead of the array.
//
// Two separate contracts are asserted, and the test is deliberately built so
// each is reported on its own rather than one hiding the other:
//
//	TRIM APPLIED    the rebuilt dictionary must actually carry the shortened
//	                colorant list. The inline branch writes newCS and counts a
//	                repair, so a dictionary written back untrimmed is a repair
//	                reported without being performed. That was the historical
//	                defect: the substitution matched only an indirect
//	                reference equal to ref, which is the zero value on the
//	                inline path. The replacement is now keyed by colour-space
//	                name.
//	PEER PRESERVED  csDict is copied wholesale into newCS. It used to be read
//	                with document.ResolveDict, so an earlier fixer's pending
//	                edit to that same dictionary was discarded. It is now read
//	                with resolveFromAllocOrDoc, the matching helper for a dict,
//	                which the function also uses for the catalog.
//
// The marker is a key the fixture does not declare, so its presence can only
// come from the pending copy.
func TestTrimDeviceNColorants_InlineColourSpaceIsTrimmedAndKeepsAPeerWrite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, deviceNInlineColourSpacePDF())

	const csObj = 5

	onDisk, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: csObj})
	if err != nil || onDisk == nil {
		t.Fatalf("fixture /ColorSpace dict at object %d did not resolve: %v", csObj, err)
	}
	inline, ok := onDisk["CSN"].(ast.PdfArray)
	if !ok {
		t.Fatalf("precondition: /CSN must be an INLINE array or the branch under test "+
			"is never reached; got %T", onDisk["CSN"])
	}
	names, ok := inline[1].(ast.PdfArray)
	if !ok || len(names) <= pdfADeviceNComponents {
		t.Fatalf("precondition: the fixture must declare more than %d colorants or the "+
			"trim never runs; got %v", pdfADeviceNComponents, inline[1])
	}
	if _, present := onDisk["Marker"]; present {
		t.Fatalf("precondition: the fixture must NOT declare /Marker, else it proves nothing")
	}

	// Stand in for an earlier colour-space fixer editing the same dictionary.
	peer := make(ast.PdfDict, len(onDisk)+1)
	maps.Copy(peer, onDisk)
	peer["Marker"] = ast.PdfName("peer")
	if !alloc.AllocAt(csObj, peer) {
		t.Fatalf("AllocAt(%d) refused the earlier fixer's pending write", csObj)
	}

	if _, err := fixAggressiveLimits(ctx, alloc, doc, Options{}, nil, ""); err != nil {
		t.Fatalf("fixAggressiveLimits: %v", err)
	}

	got, ok := alloc.Pending()[csObj].(ast.PdfDict)
	if !ok {
		t.Fatalf("object %d is not a pending dict (%T): the inline branch never wrote it, "+
			"so neither contract below can be judged", csObj, alloc.Pending()[csObj])
	}

	// Reported with Errorf, not Fatalf, so the peer check below still runs and
	// the two defects are diagnosed independently in one execution.
	if arr, isArr := got["CSN"].(ast.PdfArray); !isArr || len(arr) < 2 {
		t.Errorf("/CSN is %T after the repair, want an array; the rebuilt dictionary "+
			"does not carry the colour space at all", got["CSN"])
	} else if trimmed, isArr := arr[1].(ast.PdfArray); !isArr {
		t.Errorf("/CSN element 1 is %T, want the colorant name array", arr[1])
	} else if len(trimmed) != pdfADeviceNComponents {
		t.Errorf("the inline colour space still declares %d colorants, want %d: "+
			"trimDeviceNColorants wrote the parent dictionary and counted a repair "+
			"without applying the trim, because the substitution at :277-281 matches "+
			"only an indirect reference equal to ref, and ref is the zero value on "+
			"the inline path; /CSN = %v", len(trimmed), pdfADeviceNComponents, arr)
	}

	if _, kept := got["Marker"]; !kept {
		t.Errorf("/Marker is gone: trimDeviceNColorants rebuilt the /ColorSpace "+
			"dictionary from the on-disk copy and discarded the earlier fixer's "+
			"write; dict = %v", got)
	}
}
