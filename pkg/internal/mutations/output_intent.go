package mutations

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// OutputIntentSpec describes the output intent [SetOutputIntent] installs:
// the destination profile, its component count, and the entries ISO
// 32000-1, 14.11.5, Table 365 names the printing condition with.
type OutputIntentSpec struct {
	// Profile is the ICC profile, /DestOutputProfile, and Components its /N.
	Profile    []byte
	Components int
	// Condition is /OutputConditionIdentifier, required by Table 365.
	// Registry is /RegistryName and Info /Info, each left out when "".
	Condition, Registry, Info string
}

// OutputIntentResult says what [SetOutputIntent] did.
type OutputIntentResult struct {
	// Action is "replaced" when it replaced the GTS_PDFA1 intents, "added"
	// when it added one.
	Action string
	// Kept lists the /S of each intent of another subtype it left as it was,
	// in array order.
	Kept []string
}

// pdfaIntentSubtype is the output intent subtype SetOutputIntent writes: ISO
// 32000-2, Table 401 keeps GTS_PDFA1 for every part of PDF/A. GTS_PDFX would
// claim PDF/X conformance (ISO 15930), which a conversion does not establish.
const pdfaIntentSubtype = "GTS_PDFA1"

// SetOutputIntent makes spec's profile the document's GTS_PDFA1 output
// intent (ISO 32000-1, 14.11.5): every GTS_PDFA1 intent of the catalog's
// /OutputIntents takes the new profile and printing condition, and when there
// is none one is added at the end. Intents of any other subtype, GTS_PDFX
// included, are kept as they are: a PDF/X intent names the printing condition
// a document was prepared for, which only its producer can restate.
//
// The edits are queued on alloc, the profile as a FlateDecode stream of its
// own that every replaced intent names, so the intents agree on one profile
// object (ISO 19005-2, 6.2.3).
func SetOutputIntent(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, spec OutputIntentSpec) (OutputIntentResult, error) {
	catalogRef, ok := CatalogRef(ctx, doc)
	if !ok {
		return OutputIntentResult{}, fmt.Errorf("setting the output intent: %w", ErrCatalogUnreadable)
	}
	catalog, err := LiveCatalogDict(ctx, alloc, doc, catalogRef)
	if err != nil {
		return OutputIntentResult{}, fmt.Errorf("setting the output intent: %w", err)
	}

	profileRef := alloc.Alloc(flateProfile(spec))
	intent := func(base ast.PdfDict) ast.PdfDict {
		d := make(ast.PdfDict, len(base)+5)
		maps.Copy(d, base)
		// /DestOutputProfileRef names a profile outside the file (ISO
		// 32000-2, Table 401), which would contradict the embedded one.
		delete(d, "DestOutputProfileRef")
		d["Type"] = ast.PdfName("OutputIntent")
		d["S"] = ast.PdfName(pdfaIntentSubtype)
		d["OutputConditionIdentifier"] = document.EncodePdfString(spec.Condition)
		d["DestOutputProfile"] = profileRef
		setOrDrop(d, "RegistryName", spec.Registry)
		setOrDrop(d, "Info", spec.Info)
		return d
	}

	arrayObj := catalog["OutputIntents"]
	entries := liveArray(ctx, alloc, doc, arrayObj)
	var res OutputIntentResult
	out := make(ast.PdfArray, 0, len(entries)+1)
	for _, entry := range entries {
		d := liveDict(ctx, alloc, doc, entry)
		if d == nil {
			out = append(out, entry)
			continue
		}
		// An /S that resolves to no name is no GTS_PDFA1 intent, and is kept
		// as any other subtype is, so its error reads as the empty name.
		if s, _ := document.ResolveName(ctx, doc, d["S"]); s != pdfaIntentSubtype {
			res.Kept = append(res.Kept, string(s))
			out = append(out, entry)
			continue
		}
		res.Action = "replaced"
		if ref, isRef := entry.(ast.PdfIndirectRef); isRef {
			alloc.AllocAt(ref.ObjectNum, intent(d))
			out = append(out, ref)
			continue
		}
		out = append(out, intent(d))
	}
	if res.Action == "" {
		res.Action = "added"
		out = append(out, alloc.Alloc(intent(nil)))
	}

	if ref, isRef := arrayObj.(ast.PdfIndirectRef); isRef && entries != nil {
		alloc.AllocAt(ref.ObjectNum, out)
		return res, nil
	}
	newCatalog := make(ast.PdfDict, len(catalog)+1)
	maps.Copy(newCatalog, catalog)
	newCatalog["OutputIntents"] = out
	alloc.AllocAt(catalogRef.ObjectNum, newCatalog)
	return res, nil
}

// setOrDrop sets d[key] to the text string value, or removes key when value
// is "".
func setOrDrop(d ast.PdfDict, key ast.PdfName, value string) {
	if value == "" {
		delete(d, key)
		return
	}
	d[key] = document.EncodePdfString(value)
}

// liveArray resolves obj to an array, preferring a version an earlier edit
// queued on alloc, or nil.
func liveArray(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, obj ast.PdfObject) ast.PdfArray {
	if ref, ok := obj.(ast.PdfIndirectRef); ok {
		if a, isArray := alloc.Pending()[ref.ObjectNum].(ast.PdfArray); isArray {
			return a
		}
	}
	a, err := document.ResolveArray(ctx, doc, obj)
	if err != nil {
		return nil
	}
	return a
}

// liveDict resolves obj to a dictionary, preferring a version an earlier
// edit queued on alloc, or nil.
func liveDict(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, obj ast.PdfObject) ast.PdfDict {
	if ref, ok := obj.(ast.PdfIndirectRef); ok {
		return ResolveFromAllocOrDoc(ctx, alloc, doc, ref)
	}
	d, err := document.ResolveDict(ctx, doc, obj)
	if err != nil {
		return nil
	}
	return d
}

// flateProfile returns spec's profile as an ICC profile stream (ISO 32000-1,
// 8.6.5.5, Table 66), FlateDecode-compressed.
func flateProfile(spec OutputIntentSpec) ast.PdfStream {
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	// A bytes.Buffer grows without failing, so neither call can.
	_, _ = w.Write(spec.Profile)
	_ = w.Close()
	return ast.NewStream(ast.PdfDict{
		"N":      ast.PdfInt(spec.Components),
		"Filter": ast.PdfName("FlateDecode"),
		"Length": ast.PdfInt(buf.Len()),
	}, buf.Bytes())
}
