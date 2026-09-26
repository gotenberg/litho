package remediation

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

func matchGraphics(v compliance.Violation) bool {
	return clausePrefix(v, "6.2.9", "6.2.6") || // rendering intent (ExtGState /RI and image /Intent)
		// Only ISO 19005-1 spells a clause 6.2.7, the PostScript XObject ban
		// this fixer repairs by replacing the XObject with an empty Form.
		// Matched WHOLE and scoped to part 1: ISO 19005-4 numbers its image
		// rules 6.2.7.1 and its JPEG2000 rules 6.2.7.3, so a prefix claimed
		// both families. Nothing here rewrites /BitsPerComponent, which
		// describes the image samples and cannot change without re-encoding
		// them, and nothing re-encodes JPX data at all.
		(v.Rule.Specification == "ISO_19005_1" && v.Rule.Clause == "6.2.7") ||
		clausePrefix(v, "6.2.5") || // Form XObject /Subtype2, /PS
		// ISO 19005-1 spells the ExtGState transfer-function rule 6.2.8,
		// where parts 2, 3 and 4 spell it 6.2.5, so the prefix above cannot
		// reach it and /TR and /TR2 went unrepaired for PDF/A-1 targets. The
		// clause is matched WHOLE here because part 1 states only t1 (/TR)
		// and t2 (/TR2), both repaired below; parts 2 to 4 could not be
		// matched this way, their 6.2.5 also covering halftone dictionaries.
		(v.Rule.Specification == "ISO_19005_1" && v.Rule.Clause == "6.2.8") ||
		clausePrefix(v, "6.2.4.2") || // overprint mode OPM
		clausePrefix(v, "6.2.8.1") || // Form XObject /OPI
		clausePrefix(v, "6.2.8.2") || // reference XObject /Ref
		// ISO 14289-1 §7.20 t1 restates ISO 19005-1 §6.2.6 t1 word for word:
		// a conforming file shall not contain any reference XObjects. The
		// PDF/A spelling was claimed and the PDF/UA one was not, so a UA-1
		// target kept a /Ref this fixer deletes for every other profile.
		//
		// Matched by rule, not by clause: §7.20 t2 wants form XObject content
		// incorporated into the structure tree, which deleting /Ref does
		// nothing for.
		v.Rule == compliance.Rule_14289_1_7_20_t01 ||
		clausePrefix(v, "6.2.10") && descriptionContains(v, "blend mode") || // BM (parts 2+)
		descriptionContains(v, "Alternates") || // image /Alternates
		descriptionContains(v, "Interpolate") || // image /Interpolate
		descriptionContains(v, "OPI") // image or form /OPI (all PDF/A parts)
}

// validRenderingIntents lists the four standard rendering intents per
// ISO 32000-2, Table 69.
var validRenderingIntents = map[ast.PdfName]bool{
	"AbsoluteColorimetric": true,
	"RelativeColorimetric": true,
	"Saturation":           true,
	"Perceptual":           true,
}

func fixGraphics(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("resolving catalog: %w", err)
	}

	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return nil, fmt.Errorf("resolving pages: %w", err)
	}

	var notes []string
	visited := make(map[int]bool)

	for i, page := range pages {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("fixing graphics on page %d: %w", i+1, err)
		}

		// The node holding the /Resources the page resolves its names
		// against owns every repair written inline below it: the page, or
		// the ancestor it inherits them from. It is read through the
		// allocator, so the /Default space output-intent injects into the
		// same /Resources ahead of graphics rides this write.
		holder, ok := resourcesHolder(ctx, alloc, doc, annotPageEntry{ref: page.Ref, dict: livePageDict(ctx, alloc, doc, page)})
		if !ok {
			continue
		}
		work := maps.Clone(holder.dict)
		resDict := liveDictValue(ctx, alloc, doc, work["Resources"])
		if resDict == nil {
			continue
		}

		landed, pending, dirty, err := fixResourcesGraphics(ctx, alloc, doc, resDict, work, visited)
		if err != nil {
			return nil, fmt.Errorf("fixing graphics on page %d: %w", i+1, err)
		}
		notes = append(notes, landed...)
		if dirty && alloc.AllocAt(holder.ref.ObjectNum, work) {
			notes = append(notes, pending...)
		}
	}

	return notes, nil
}

// fixResourcesGraphics repairs the graphics states one resource dictionary
// declares and the XObjects it names, recursively.
//
// owner is the caller's working copy of the dictionary that holds resources
// under /Resources, which [fixExtGStates] updates in place when a repair can
// only be written through it. landed describes the repairs already written;
// pending describes the ones that land with the caller's write of owner, which
// the caller makes only when dirty is set.
func fixResourcesGraphics(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, resources ast.PdfDict, owner ast.PdfDict, visited map[int]bool) (landed, pending []string, dirty bool, err error) {
	landed, pending, dirty, err = fixExtGStates(ctx, alloc, doc, resources, owner)
	if err != nil {
		return nil, nil, false, err
	}

	xobjNotes, err := fixXObjects(ctx, alloc, doc, resources, visited)
	if err != nil {
		return nil, nil, false, err
	}
	landed = append(landed, xobjNotes...)

	return landed, pending, dirty, nil
}

// fixExtGStates repairs every graphics state parameter dictionary the
// /ExtGState entry of resources declares, where it sits.
//
// ISO 32000-1, 7.3.7 lets /Resources, /ExtGState and each graphics state be
// direct objects. A state with an object number of its own is written at it;
// the others are written at the first link of the chain above them that has
// one, through [writeResourceCategory]: the /ExtGState dictionary, the
// /Resources dictionary, or owner, updated in place for the caller to write.
// The repair wrote a state only at an object number of its own, so a direct
// one kept every finding while the report listed it repaired. See
// [fixResourcesGraphics] for what the results mean.
//
// Every dictionary is read through the allocator: output-intent writes the
// same /Resources ahead of graphics, and rebuilding one from the document
// would discard that write.
//
// # Corpus evidence
//
//   - litho gstate_2b.pdf: the direct states GS7, GS9, H1, H2 and the form's
//     GSn kept 6.2.5 t1, t3, t4 and 6.2.6 t1 after a PDF/A-2b remediation,
//     and 6.2.8 t1 and 6.2.9 t1 after a PDF/A-1b one, on litho and veraPDF
//     alike.
func fixExtGStates(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, resources ast.PdfDict, owner ast.PdfDict) (landed, pending []string, dirty bool, err error) {
	gsObj, ok := resources["ExtGState"]
	if !ok {
		return nil, nil, false, nil
	}

	gsDict := liveDictValue(ctx, alloc, doc, gsObj)
	if gsDict == nil {
		doc.Logger.Debug("resolving ExtGState dict", slog.Any("value", gsObj))
		return nil, nil, false, nil
	}

	// Entries with no object number of their own accumulate here and are
	// written together: rebuilding the parent once per entry would keep only
	// the last change.
	var inline ast.PdfDict
	var inlineNotes []string

	// Sorted: the notes below become report lines, and ranging the dictionary
	// emitted them in Go's randomized map order, so the same file produced a
	// differently ordered report on every run.
	for _, name := range slices.Sorted(maps.Keys(gsDict)) {
		if err := ctx.Err(); err != nil {
			return nil, nil, false, err
		}
		entry := gsDict[name]
		gsEntry := liveDictValue(ctx, alloc, doc, entry)
		if gsEntry == nil {
			continue
		}
		newGS, notes := repairGraphicsState(ctx, doc, name, gsEntry)
		if newGS == nil {
			continue
		}
		if ref, isRef := entry.(ast.PdfIndirectRef); isRef {
			if alloc.AllocAt(ref.ObjectNum, newGS) {
				landed = append(landed, notes...)
			}
			continue
		}
		if inline == nil {
			inline = make(ast.PdfDict)
		}
		inline[name] = newGS
		inlineNotes = append(inlineNotes, notes...)
	}
	if len(inline) == 0 {
		return landed, nil, false, nil
	}

	newGSDict := maps.Clone(gsDict)
	maps.Copy(newGSDict, inline)
	written, ownerDirty := writeResourceCategory(alloc, owner, resources, "ExtGState", newGSDict)
	switch {
	case written:
		landed = append(landed, inlineNotes...)
	case ownerDirty:
		pending = inlineNotes
	}
	return landed, pending, ownerDirty, nil
}

// repairGraphicsState returns a repaired copy of the graphics state parameter
// dictionary gs, declared under name, and a description of each repair, or nil
// when gs needs none.
func repairGraphicsState(ctx context.Context, doc *document.Document, name ast.PdfName, gs ast.PdfDict) (ast.PdfDict, []string) {
	var notes []string
	newGS := maps.Clone(gs)

	// Fix invalid rendering intent (6.2.9 / 6.2.6).
	if riObj, ok := newGS["RI"]; ok {
		if riName, ok := riObj.(ast.PdfName); ok {
			if !validRenderingIntents[riName] {
				newGS["RI"] = ast.PdfName("RelativeColorimetric")
				notes = append(notes, fmt.Sprintf("replaced invalid rendering intent %q in ExtGState %s", string(riName), string(name)))
			}
		}
	}

	// Fix OPM=1 with ICCBased CMYK (6.2.4.2).
	if opmObj, ok := newGS["OPM"]; ok {
		if document.Float(opmObj) == 1 {
			newGS["OPM"] = ast.PdfInt(0)
			notes = append(notes, fmt.Sprintf("set OPM to 0 in ExtGState %s", string(name)))
		}
	}

	// Fix invalid blend mode (6.2.10).
	if bmObj, ok := newGS["BM"]; ok {
		if bmName, ok := bmObj.(ast.PdfName); ok && bmName != "Normal" && bmName != "Compatible" {
			newGS["BM"] = ast.PdfName("Normal")
			notes = append(notes, fmt.Sprintf("set blend mode to Normal in ExtGState %s", string(name)))
		}
	}

	// Strip forbidden transfer-function entries (ISO 19005-2 §6.2.5
	// forbids /TR; /TR2 is permitted only when set to /Default).
	if _, ok := newGS["TR"]; ok {
		delete(newGS, "TR")
		notes = append(notes, fmt.Sprintf("removed /TR from ExtGState %s", string(name)))
	}
	if tr2, ok := newGS["TR2"]; ok {
		if tr2Name, ok := tr2.(ast.PdfName); !ok || tr2Name != "Default" {
			newGS["TR2"] = ast.PdfName("Default")
			notes = append(notes, fmt.Sprintf("set /TR2 to /Default in ExtGState %s", string(name)))
		}
	}
	// §6.2.5 also forbids /HTO and /HTP on ExtGState.
	for _, key := range []ast.PdfName{"HTO", "HTP"} {
		if _, ok := newGS[key]; ok {
			delete(newGS, key)
			notes = append(notes, fmt.Sprintf("removed /%s from ExtGState %s", string(key), string(name)))
		}
	}
	if htObj, ok := newGS["HT"]; ok {
		if patched, changed := normalizeHalftone(ctx, doc, htObj); changed {
			newGS["HT"] = patched
			notes = append(notes, fmt.Sprintf("normalised /HT in ExtGState %s", string(name)))
		}
	}

	if len(notes) == 0 {
		return nil, nil
	}
	return newGS, notes
}

// writeResourceCategory writes rebuilt, a repaired copy of the resource
// category dictionary resources hold under category, at the first link of the
// chain above it that has an object number of its own, as ISO 32000-1, 7.3.7
// lets every link be a direct object: the category dictionary itself, the
// /Resources dictionary, or owner, the caller's working copy of the dictionary
// holding resources under /Resources, which it updates in place for the caller
// to write.
//
// resources must be the dictionary owner holds, as the allocator has it: the
// copy rebuilt around rebuilt carries everything else it holds, and a copy of
// the document's would discard what an earlier fixer queued there. written
// reports a write that landed; ownerDirty, that owner was updated and the
// repair lands with the caller's write of it.
func writeResourceCategory(alloc *writer.ObjAllocator, owner, resources ast.PdfDict, category ast.PdfName, rebuilt ast.PdfDict) (written, ownerDirty bool) {
	if ref, ok := resources[category].(ast.PdfIndirectRef); ok {
		return alloc.AllocAt(ref.ObjectNum, rebuilt), false
	}
	resObj, ok := owner["Resources"]
	if !ok {
		return false, false
	}
	newRes := maps.Clone(resources)
	newRes[category] = rebuilt
	if ref, ok := resObj.(ast.PdfIndirectRef); ok {
		return alloc.AllocAt(ref.ObjectNum, newRes), false
	}
	owner["Resources"] = newRes
	return false, true
}

// normalizeHalftone coerces a halftone dict so it complies with
// ISO 19005-2 §6.2.5: HalftoneType must be 1 or 5, HalftoneName is
// forbidden, the halftone a graphics state names carries no
// TransferFunction, and any non-primary component of a Type-5 halftone
// needs one (defaulted to /Identity when absent).
func normalizeHalftone(ctx context.Context, doc *document.Document, htObj ast.PdfObject) (ast.PdfDict, bool) {
	htDict, err := document.ResolveDict(ctx, doc, htObj)
	if err != nil || htDict == nil {
		return nil, false
	}
	newDict := make(ast.PdfDict, len(htDict))
	maps.Copy(newDict, htDict)

	changed := false
	if _, ok := newDict["HalftoneName"]; ok {
		delete(newDict, "HalftoneName")
		changed = true
	}
	// ISO 32000-1, 10.5.5, Table 130 wants a /TransferFunction on a halftone
	// only as a component of a type 5 halftone, and ISO 19005-2, 6.2.5 t6
	// forbids it elsewhere; the check lets /Default stand. Turning a type 6
	// halftone into a type 1 below kept the one its source carried, so t4
	// gave way to t6.
	if tf, ok := newDict["TransferFunction"]; ok {
		if name, _ := document.ResolveName(ctx, doc, tf); name != "Default" {
			delete(newDict, "TransferFunction")
			changed = true
		}
	}
	htType := 1
	if htTypeObj, ok := newDict["HalftoneType"]; ok {
		switch t := htTypeObj.(type) {
		case ast.PdfInt:
			htType = int(t)
		case ast.PdfReal:
			htType = int(t)
		}
	}
	if htType != 1 && htType != 5 {
		newDict["HalftoneType"] = ast.PdfInt(1)
		changed = true
		htType = 1
	}

	if htType == 5 {
		// Halftone Type 5 component rules (ISO 32000-2 §10.6.5.4):
		// primary CMYK components must not carry a TransferFunction,
		// non-primary (spot) components must carry one. Normalise
		// both directions.
		primaryCMYK := map[ast.PdfName]bool{
			"Cyan": true, "Magenta": true, "Yellow": true, "Black": true,
		}
		for k, v := range newDict {
			if k == "Type" || k == "HalftoneType" || k == "HalftoneName" {
				continue
			}
			compDict, err := document.ResolveDict(ctx, doc, v)
			if err != nil || compDict == nil {
				continue
			}
			_, hasTF := compDict["TransferFunction"]
			if primaryCMYK[k] {
				if !hasTF {
					continue
				}
				newComp := make(ast.PdfDict, len(compDict))
				maps.Copy(newComp, compDict)
				delete(newComp, "TransferFunction")
				newDict[k] = newComp
				changed = true
				continue
			}
			if hasTF {
				continue
			}
			newComp := make(ast.PdfDict, len(compDict)+1)
			maps.Copy(newComp, compDict)
			newComp["TransferFunction"] = ast.PdfName("Identity")
			newDict[k] = newComp
			changed = true
		}
	}

	if !changed {
		return nil, false
	}
	return newDict, true
}

func fixXObjects(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, resources ast.PdfDict, visited map[int]bool) ([]string, error) {
	xobjObj, ok := resources["XObject"]
	if !ok {
		return nil, nil
	}

	xobjDict := liveDictValue(ctx, alloc, doc, xobjObj)
	if xobjDict == nil {
		doc.Logger.Debug("resolving XObject dict", slog.Any("value", xobjObj))
		return nil, nil
	}

	var notes []string

	// Sorted, for the reason given in fixExtGStates.
	for _, name := range slices.Sorted(maps.Keys(xobjDict)) {
		entryObj := xobjDict[name]
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		ref, ok := entryObj.(ast.PdfIndirectRef)
		if !ok {
			continue
		}

		if visited[ref.ObjectNum] {
			continue
		}
		visited[ref.ObjectNum] = true

		// Resolve from allocator first, then from document.
		var xobjStream ast.PdfStream
		if obj, ok := alloc.Pending()[ref.ObjectNum]; ok {
			if s, ok := obj.(ast.PdfStream); ok {
				xobjStream = s
			}
		}
		if xobjStream.Dict == nil {
			resolved, err := document.ResolveStream(ctx, doc, ref)
			if err != nil {
				continue
			}
			xobjStream = resolved
		}
		if xobjStream.Dict == nil {
			continue
		}

		subtype, _ := xobjStream.Dict["Subtype"].(ast.PdfName)
		modified := false
		newDict := make(ast.PdfDict, len(xobjStream.Dict))
		maps.Copy(newDict, xobjStream.Dict)
		// The repairs to this object, reported once its write lands.
		var objNotes []string

		if subtype == "Image" {
			// Remove /Alternates (6.2.8 / 6.2.7.1).
			if _, ok := newDict["Alternates"]; ok {
				delete(newDict, "Alternates")
				modified = true
				objNotes = append(objNotes, fmt.Sprintf("removed /Alternates from image %s", string(name)))
			}

			// Remove /OPI: forbidden on image XObjects in every PDF/A part
			// (ISO 19005-1 §6.2.4, ISO 19005-2/3 §6.2.8, ISO 19005-4 §6.2.7.1).
			// OPI is pre-press workflow metadata with no rendering effect, so
			// dropping it preserves the image. The form branch already does this.
			if _, ok := newDict["OPI"]; ok {
				delete(newDict, "OPI")
				modified = true
				objNotes = append(objNotes, fmt.Sprintf("removed /OPI from image %s", string(name)))
			}

			// Set /Interpolate to false (6.2.4).
			if interpObj, ok := newDict["Interpolate"]; ok {
				if b, ok := interpObj.(ast.PdfBool); ok && bool(b) {
					newDict["Interpolate"] = ast.PdfBool(false)
					modified = true
					objNotes = append(objNotes, fmt.Sprintf("set /Interpolate to false in image %s", string(name)))
				}
			}

			// Fix invalid image rendering intent (6.2.6), mirroring the
			// ExtGState /RI repair above.
			if intentObj, ok := newDict["Intent"]; ok {
				if intentName, ok := intentObj.(ast.PdfName); ok && !validRenderingIntents[intentName] {
					newDict["Intent"] = ast.PdfName("RelativeColorimetric")
					modified = true
					objNotes = append(objNotes, fmt.Sprintf("replaced invalid rendering intent %q in image %s", string(intentName), string(name)))
				}
			}

			// A soft-mask (/SMask) or explicit-mask (/Mask) image is itself an
			// image XObject subject to the same repairs, but it is referenced
			// from the image dict rather than a Resources/XObject tree, so the
			// page walk never reaches it. veraPDF flags e.g. Interpolate==true on
			// an SMask (ISO 19005-2 §6.2.4). Recurse by wrapping each mask ref as
			// a one-entry XObject resource; /Mask may be a colour-key array, which
			// the indirect-reference assertion skips.
			for _, key := range []ast.PdfName{"SMask", "Mask"} {
				maskRef, ok := xobjStream.Dict[key].(ast.PdfIndirectRef)
				if !ok {
					continue
				}
				maskNotes, err := fixXObjects(ctx, alloc, doc, ast.PdfDict{
					"XObject": ast.PdfDict{key: maskRef},
				}, visited)
				if err != nil {
					return nil, err
				}
				notes = append(notes, maskNotes...)
			}
		}

		if subtype == "PS" {
			// PDF/A-1 §6.2.7 and PDF/A-2+ §6.2.9.3 forbid PostScript XObjects.
			// Convert to an empty Form XObject so existing Do references still
			// resolve to a valid no-op.
			newDict["Subtype"] = ast.PdfName("Form")
			newDict["FormType"] = ast.PdfInt(1)
			if _, hasBBox := newDict["BBox"]; !hasBBox {
				newDict["BBox"] = ast.PdfArray{ast.PdfReal(0), ast.PdfReal(0), ast.PdfReal(1), ast.PdfReal(1)}
			}
			for _, k := range []ast.PdfName{"Level1", "PS", "OPI", "Ref"} {
				delete(newDict, k)
			}
			// The body is replaced by an empty one, so the filter chain that
			// described the PostScript payload no longer applies: an empty
			// stream still declaring /Filter is one a reader decodes into an
			// error rather than the intended no-op (ISO 32000-1, 7.3.8.2).
			for _, k := range []ast.PdfName{"Filter", "DecodeParms"} {
				delete(newDict, k)
			}
			alloc.AllocAt(ref.ObjectNum, ast.NewStream(newDict, nil))
			notes = append(notes, fmt.Sprintf("replaced PostScript XObject %s with empty Form", string(name)))
			continue
		}

		if subtype == "Form" {
			// Remove /Subtype2 and /PS (6.2.5).
			if _, ok := newDict["Subtype2"]; ok {
				delete(newDict, "Subtype2")
				modified = true
				objNotes = append(objNotes, fmt.Sprintf("removed /Subtype2 from form XObject %s", string(name)))
			}
			if _, ok := newDict["PS"]; ok {
				delete(newDict, "PS")
				modified = true
				objNotes = append(objNotes, fmt.Sprintf("removed /PS from form XObject %s", string(name)))
			}

			// Remove /OPI (6.2.8.1).
			if _, ok := newDict["OPI"]; ok {
				delete(newDict, "OPI")
				modified = true
				objNotes = append(objNotes, fmt.Sprintf("removed /OPI from form XObject %s", string(name)))
			}

			// Remove /Ref (6.2.8.2).
			if _, ok := newDict["Ref"]; ok {
				delete(newDict, "Ref")
				modified = true
				objNotes = append(objNotes, fmt.Sprintf("removed /Ref from form XObject %s", string(name)))
			}

			// Recurse into form XObject resources. newDict is the working
			// copy written below, so a graphics state written directly in
			// the form's /Resources rides the same write as the keys removed
			// above.
			if formRes := liveDictValue(ctx, alloc, doc, newDict["Resources"]); formRes != nil {
				landed, pending, dirty, err := fixResourcesGraphics(ctx, alloc, doc, formRes, newDict, visited)
				if err != nil {
					return nil, err
				}
				notes = append(notes, landed...)
				if dirty {
					modified = true
					objNotes = append(objNotes, pending...)
				}
			}
		}

		if modified {
			// Preserve the original stream bytes: only the dict changed, so
			// re-emitting with nil data would blank the image pixels or form
			// content (serializeStream rewrites /Length from the data, leaving
			// an empty, corrupt stream).
			data, derr := xobjStream.Bytes(ctx)
			if derr != nil {
				// The original payload is unreadable, so it cannot be carried
				// across. Leave the object untouched rather than write the
				// fixed dict with an empty body: a still-flagged violation is
				// safer than a corrupt stream, and re-validation re-surfaces it.
				doc.Logger.Debug("skipping XObject dict fix; original stream unreadable",
					slog.String("name", string(name)), slog.Any("err", derr))
			} else if alloc.AllocAt(ref.ObjectNum, ast.NewStream(newDict, data)) {
				notes = append(notes, objNotes...)
			}
		}
	}

	return notes, nil
}
