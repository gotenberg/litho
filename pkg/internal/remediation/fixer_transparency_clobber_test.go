package remediation

import (
	"context"
	"maps"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// pageExtGStatePDF puts the offending /ExtGState straight on the page's
// /Resources, object 4, so a peer fixer's pending write lands on the same
// object the transparency walk rewrites.
func pageExtGStatePDF() []byte {
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] "+
			"/Resources << /ExtGState << /GS0 4 0 R >> >> >>",
		"<< /Type /ExtGState /ca 0.5 /CA 0.5 >>",
	)
}

// TestFixTransparency_KeepsAPeerFixersPendingExtGStateWrite pins that the
// transparency repair reads an /ExtGState through the allocator.
//
// graphics (registry 106) and transparency (registry 107) are CONSECUTIVE
// registry entries that write the same /ExtGState objects. fixExtGStates reads
// each entry through resolveFromAllocOrDoc; fixExtGStateDict used to read it
// with document.ResolveDict and no alloc-aware helper at all, so running second
// it rebuilt the dictionary from the on-disk copy and dropped whatever graphics
// just wrote there.
//
// The pending write stands in for that peer. Provoking graphics for real needs
// a second violation on the same object (an /HT or /TR entry it repairs);
// seeding the allocator reproduces the state it leaves behind without coupling
// this test to another fixer's matcher.
//
// The fixpoint loop in pkg/pdf hides this in the output, exactly as it hid
// 9bbdcd41: a discarded repair is simply reapplied on the next round, so only a
// unit test can see it.
func TestFixTransparency_KeepsAPeerFixersPendingExtGStateWrite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, pageExtGStatePDF())

	const gsObj = 4
	gs := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: gsObj})
	if gs == nil {
		t.Fatalf("fixture ExtGState object %d did not resolve", gsObj)
	}

	// Stand in for graphics (106): copy the dict, add one key, AllocAt the
	// same object number. /HT is what fixExtGStates normalises there.
	peer := make(ast.PdfDict, len(gs)+1)
	maps.Copy(peer, gs)
	peer["HT"] = ast.PdfName("Default")
	if !alloc.AllocAt(gsObj, peer) {
		t.Fatalf("AllocAt(%d) refused the peer's pending write", gsObj)
	}

	if _, err := fixTransparency(ctx, alloc, doc, Options{}, nil, "PDF/A-1b"); err != nil {
		t.Fatalf("fixTransparency: %v", err)
	}

	got, ok := alloc.Pending()[gsObj].(ast.PdfDict)
	if !ok {
		t.Fatalf("object %d is %T, want a pending dictionary", gsObj, alloc.Pending()[gsObj])
	}

	// The fixer's own job must still be done: this is a composition test, not
	// an assertion that it leaves the ExtGState alone.
	if v, isReal := got["ca"].(ast.PdfReal); !isReal || float64(v) != 1.0 {
		t.Errorf("/ca = %v, want 1.0: the transparency repair did not run", got["ca"])
	}

	if _, kept := got["HT"]; !kept {
		t.Errorf("/HT is gone: fixExtGStateDict rebuilt the ExtGState from the on-disk "+
			"copy and discarded the earlier fixer's write; ExtGState = %v", got)
	}
}

// liveOwnerResources returns the /Resources dictionary the output will carry
// for the page or form dictionary at ownerObj, following an inline value or a
// reference through the allocator first.
func liveOwnerResources(t *testing.T, alloc *writer.ObjAllocator, doc *document.Document, ownerObj int) ast.PdfDict {
	t.Helper()

	ctx := context.Background()
	var owner ast.PdfDict
	switch v := alloc.Pending()[ownerObj].(type) {
	case ast.PdfStream:
		owner = v.Dict
	case ast.PdfDict:
		owner = v
	default:
		owner = resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: ownerObj})
		if owner == nil {
			if s, err := document.ResolveStream(ctx, doc, ast.PdfIndirectRef{ObjectNum: ownerObj}); err == nil {
				owner = s.Dict
			}
		}
	}
	if owner == nil {
		t.Fatalf("owner object %d did not resolve", ownerObj)
	}
	res := liveDictValue(ctx, alloc, doc, owner["Resources"])
	if res == nil {
		t.Fatalf("/Resources of object %d is %v and did not resolve", ownerObj, owner["Resources"])
	}
	return res
}

// TestFixExtGStateDict_KeepsAnEarlierFixersPendingResourcesWrite pins that the
// inline /ExtGState repair rebuilds the /Resources dictionary around it from the
// copy output-intent queued, not from the document.
//
// When the /ExtGState chain is inline, fixExtGStateDict writes the whole
// /Resources dictionary back: at its own object number when it is indirect, or
// into the page's working copy when it is inline. It built that copy from the
// resources the walk was handed, the parsed page's, so the /DefaultRGB
// output-intent (registry 68) injected ahead of transparency (107) was lost.
// Both fire on PDF/A-1, where ISO 19005-1, 6.4 forbids the transparency and
// 6.2.3.3 the uncalibrated device colour.
func TestFixExtGStateDict_KeepsAnEarlierFixersPendingResourcesWrite(t *testing.T) {
	t.Parallel()

	const res = "<< /ExtGState << /GS0 << /Type /ExtGState /CA 0.5 >> >> >>"
	for _, tc := range []struct {
		name string
		objs []string
	}{
		{name: "indirect resources", objs: []string{
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 4 0 R >>",
			res,
		}},
		{name: "inline resources", objs: []string{
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources " + res + " >>",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			doc, alloc := newTestDoc(t, assemblePDF(tc.objs))

			const pageObj = 3

			if n := injectTestDefaultRGB(t, alloc, doc); n == 0 {
				t.Fatal("control: output-intent patched no page, so there is no earlier edit to lose")
			}
			if cs := liveDictValue(ctx, alloc, doc, liveOwnerResources(t, alloc, doc, pageObj)["ColorSpace"]); cs["DefaultRGB"] == nil {
				t.Fatalf("control: output-intent did not queue /DefaultRGB for page %d", pageObj)
			}

			if _, err := fixTransparency(ctx, alloc, doc, Options{}, nil, ""); err != nil {
				t.Fatalf("fixTransparency: %v", err)
			}

			got := liveOwnerResources(t, alloc, doc, pageObj)
			gsDict := liveDictValue(ctx, alloc, doc, got["ExtGState"])
			gs := liveDictValue(ctx, alloc, doc, gsDict["GS0"])
			if ca := document.Float(gs["CA"]); ca != 1 {
				t.Errorf("/GS0 /CA is %v in the /Resources the output reads, want 1: the "+
					"repair did not land there, so the check below proves nothing", gs["CA"])
			}
			if cs := liveDictValue(ctx, alloc, doc, got["ColorSpace"]); cs["DefaultRGB"] == nil {
				t.Errorf("/DefaultRGB is gone: fixExtGStateDict rebuilt /Resources from the "+
					"document and discarded output-intent's write; /Resources = %v", got)
			}
		})
	}
}
