package mutations

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"log/slog"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/icc"
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
	// ProfileRef, when set, is a profile stream already queued that the
	// intent names in place of a new one ([ProfileStream]), so intents
	// several edits write share one profile object (ISO 19005-2, 6.2.3).
	ProfileRef ast.PdfIndirectRef
}

// ProfileStream returns spec's profile as the ICC profile stream an output
// intent names (ISO 32000-1, 8.6.5.5, Table 66), FlateDecode-compressed, to
// queue once and pass as [OutputIntentSpec.ProfileRef].
func ProfileStream(spec OutputIntentSpec) ast.PdfStream {
	return flateProfile(spec)
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

	profileRef := spec.ProfileRef
	if profileRef.ObjectNum == 0 {
		profileRef = alloc.Alloc(flateProfile(spec))
	}
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

// OutputIntentConflict is an output intent of a subtype other than GTS_PDFA1
// whose destination profile gives the device space of a target's component
// count another meaning than the target's profile ([ConflictingOutputIntents]).
type OutputIntentConflict struct {
	// Subtype is its /S and Condition its /OutputConditionIdentifier.
	Subtype, Condition string
}

// ConflictingOutputIntents lists the output intents of the catalog's
// /OutputIntents, in array order, whose subtype is not GTS_PDFA1 and whose
// embedded /DestOutputProfile, an ICC profile of the given component count,
// holds other bytes than profile. ISO 32000-1, 14.11.5 has a destination
// profile characterise the device a document's device colours are meant for,
// so such an intent reads DeviceCMYK, for a four-component profile, as
// another device's colour than the one the target converts to. An intent
// whose profile does not decode or describes another component count gives
// that device space no meaning, and is not listed.
func ConflictingOutputIntents(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, profile []byte, components int) []OutputIntentConflict {
	catalogRef, ok := CatalogRef(ctx, doc)
	if !ok {
		return nil
	}
	catalog, err := LiveCatalogDict(ctx, alloc, doc, catalogRef)
	if err != nil {
		return nil
	}
	var out []OutputIntentConflict
	for _, entry := range liveArray(ctx, alloc, doc, catalog["OutputIntents"]) {
		d := liveDict(ctx, alloc, doc, entry)
		if d == nil {
			continue
		}
		if subtype, conflicts := conflicting(ctx, alloc, doc, d, profile, components); conflicts {
			out = append(out, OutputIntentConflict{Subtype: subtype, Condition: intentCondition(ctx, doc, d)})
		}
	}
	return out
}

// RetargetOutputIntents points every intent [ConflictingOutputIntents] lists
// at the profile stream profileRef, keeping its /S, its
// /OutputConditionIdentifier and its other entries, and dropping a
// /DestOutputProfileRef, which would name another profile (ISO 32000-2,
// Table 401). It returns the intents it retargeted. The edits are queued on
// alloc.
func RetargetOutputIntents(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, profile []byte, components int, profileRef ast.PdfIndirectRef) ([]OutputIntentConflict, error) {
	catalogRef, ok := CatalogRef(ctx, doc)
	if !ok {
		return nil, fmt.Errorf("retargeting the output intents: %w", ErrCatalogUnreadable)
	}
	catalog, err := LiveCatalogDict(ctx, alloc, doc, catalogRef)
	if err != nil {
		return nil, fmt.Errorf("retargeting the output intents: %w", err)
	}
	retarget := func(d ast.PdfDict) ast.PdfDict {
		out := maps.Clone(d)
		delete(out, "DestOutputProfileRef")
		out["DestOutputProfile"] = profileRef
		return out
	}
	var done []OutputIntentConflict
	replaced := map[int]ast.PdfDict{}
	arrayObj := catalog["OutputIntents"]
	entries := liveArray(ctx, alloc, doc, arrayObj)
	for i, entry := range entries {
		d := liveDict(ctx, alloc, doc, entry)
		if d == nil {
			continue
		}
		subtype, conflicts := conflicting(ctx, alloc, doc, d, profile, components)
		if !conflicts {
			continue
		}
		done = append(done, OutputIntentConflict{Subtype: subtype, Condition: intentCondition(ctx, doc, d)})
		if ref, isRef := entry.(ast.PdfIndirectRef); isRef {
			alloc.AllocAt(ref.ObjectNum, retarget(d))
			continue
		}
		replaced[i] = retarget(d)
	}
	if len(replaced) == 0 {
		return done, nil
	}
	out := make(ast.PdfArray, len(entries))
	copy(out, entries)
	for i, d := range replaced {
		out[i] = d
	}
	if ref, isRef := arrayObj.(ast.PdfIndirectRef); isRef {
		alloc.AllocAt(ref.ObjectNum, out)
		return done, nil
	}
	newCatalog := maps.Clone(catalog)
	newCatalog["OutputIntents"] = out
	alloc.AllocAt(catalogRef.ObjectNum, newCatalog)
	return done, nil
}

// conflicting reports an intent d of another subtype than GTS_PDFA1 whose
// embedded profile of the given component count differs from profile, with its
// subtype.
func conflicting(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, d ast.PdfDict, profile []byte, components int) (string, bool) {
	// An /S that resolves to no name is no GTS_PDFA1 intent, so its error
	// reads as the empty name.
	subtype, _ := document.ResolveName(ctx, doc, d["S"])
	if subtype == pdfaIntentSubtype {
		return string(subtype), false
	}
	data := liveStreamData(ctx, alloc, doc, d["DestOutputProfile"])
	if data == nil || bytes.Equal(data, profile) {
		return string(subtype), false
	}
	p, err := icc.Parse(data)
	if err != nil || p.Components() != components {
		return string(subtype), false
	}
	return string(subtype), true
}

// intentCondition returns an intent's /OutputConditionIdentifier as text,
// "" when it has none.
func intentCondition(ctx context.Context, doc *document.Document, d ast.PdfDict) string {
	obj, err := document.ResolveObject(ctx, doc, d["OutputConditionIdentifier"])
	if err != nil {
		return ""
	}
	s, ok := obj.(ast.PdfString)
	if !ok {
		return ""
	}
	return document.DecodePdfString(s)
}

// liveStreamData returns the decoded data of the stream obj names,
// preferring a version an earlier edit queued on alloc, or nil.
func liveStreamData(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, obj ast.PdfObject) []byte {
	var stream ast.PdfStream
	if ref, ok := obj.(ast.PdfIndirectRef); ok {
		if pending, isStream := alloc.Pending()[ref.ObjectNum].(ast.PdfStream); isStream {
			stream = pending
		}
	}
	if stream.Dict == nil {
		s, err := document.ResolveStream(ctx, doc, obj)
		if err != nil || s.Dict == nil {
			return nil
		}
		stream = s
	}
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		// A profile that does not decode characterises nothing.
		doc.Logger.Debug("output intent profile does not decode", slog.Any("err", err))
		return nil
	}
	return data
}
