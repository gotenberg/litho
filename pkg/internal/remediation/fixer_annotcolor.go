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

// The data colour space signature of an ICC profile occupies header bytes 16
// to 19 (ISO 15076-1, 7.2.6). "RGB " carries a trailing space: every
// signature is four bytes.
const (
	iccColorSpaceStart = 16
	iccColorSpaceEnd   = 20
)

// matchAnnotColor matches ISO 19005-1, 6.5.3 test 3 alone: "An annotation
// dictionary shall not contain the C array or the IC array unless the colour
// space of the DestOutputProfile in the PDF/A-1 OutputIntent dictionary,
// defined in 6.2.2, is RGB". The rule exists only in part 1, so the fixer
// never runs for a later part.
func matchAnnotColor(v compliance.Violation) bool {
	return v.Rule == compliance.Rule_19005_1_6_5_3_t03
}

// fixAnnotColor removes the /C and /IC colour arrays from every annotation
// when the output intent the remediated file carries does not have an RGB
// destination profile, which ISO 19005-1, 6.5.3 requires of an annotation
// that carries either array.
//
// The two PDF/A-1 colour rules can conflict. A document painting DeviceCMYK
// needs a CMYK destination profile (ISO 19005-1, 6.2.3.3), which the
// output-intent fixer installs, and 6.5.3 then forbids every annotation colour
// array. Dropping the arrays is the side of the conflict that changes the
// least. /C colours the icon background, the pop-up title bar and a link
// border, and /IC fills the interior of the markup annotations that define it
// (ISO 32000-1, 12.5.2 Table 164, 12.5.6). An annotation with its own
// appearance stream renders from that stream (ISO 32000-1, 12.5.5), so only
// the border and icon colour of an annotation without one changes, and only
// in a viewer that draws it from these keys.
//
// Outside that conflict the arrays stay. In a file carrying no intent,
// output-intent installs an sRGB one for them (see [addAnnotColorIntent]), so
// this fixer acts under an intent the content needs or the source declares,
// and on a file output-intent does not extend.
//
// The decision reads the output intents through the allocator, not the
// document: output-intent and corrupt-icc run first and may install or
// replace the destination profile, so a source with no intent can leave with
// an RGB one, and its annotations keep their colours. It keeps them only when
// a GTS_PDFA1 intent carries a readable RGB profile and no other intent
// carries a profile that is not RGB. ISO 19005-1, 6.2.2 makes every
// /DestOutputProfile one object, so on a conforming file this is the PDF/A-1
// intent's colour space. On one that is not, litho reads the first intent
// carrying a profile and veraPDF the last GTS_PDFA1 one (PDFA-1B.xml,
// variable gOutputCS), and only this answer satisfies both. A profile that
// does not decode names no colour space, so it is not an RGB one, and a file
// that carries no intent at all has no RGB one either.
//
// An intent another fixer installed or replaced in this round is not yet the
// one the output carries, so the fixer waits for the next fixpoint round,
// which reads it from its own source. For mixed content, output-intent
// installs a CMYK intent and a /DefaultRGB. When DeviceRGB the /DefaultRGB
// cannot reach remains, such as an appearance stream with resources of its
// own, the next round replaces that CMYK intent with an sRGB one, which
// permits the arrays. Acting in the round that installed the CMYK intent
// drops colours the output never had to lose. The fixer waits the same way on
// a document whose cross-reference table was rebuilt: output-intent installs
// nothing there until the full rewrite, and the intent it installs then may
// be an RGB one.
//
// Both arrays go whenever either is present and not null, matching the
// validator: a null value is absent (ISO 32000-1, 7.3.7). Indirect
// annotations are rewritten in place, and inline ones through their page's
// /Annots array.
//
// # Corpus evidence
//
//   - All corpora: 42 files at PDF/A-1a and 1b, 39 in aggressive mode, kept
//     6.5.3 test 3 after remediation on litho and veraPDF. The sRGB intent
//     output-intent adds clears it from 22 of them, 19 in aggressive mode, and
//     the fixer from the other 20, whose content needs a CMYK intent. Neither
//     moves another rule on either validator.
//   - govdocs thread_400_400575, pdfjs bug1899804, bug921760, issue13915,
//     issue14105 and stamps: output-intent installs a CMYK intent in the first
//     round and an sRGB one in the second, at PDF/A-1a and 1b in both modes.
//     Acting in the first round dropped their annotation colours and cleared
//     no rule on litho, nor on veraPDF at PDF/A-1b.
//   - pdfjs issue9552: its cross-reference table is rebuilt, so its sRGB intent
//     arrives in the second round. Acting in the first dropped the colours of
//     8 annotations the same way.
func fixAnnotColor(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, profile string) ([]string, error) {
	if compliance.PDFAPart(profile) != 1 {
		return nil, nil
	}
	catalogRef, ok := catalogRefForFixer(ctx, doc)
	if !ok {
		return nil, nil
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, nil
	}
	live := outputIntentProfiles(ctx, alloc, doc, catalogDict)
	if admitsAnnotColor(live) {
		return nil, nil
	}
	// An allocator with nothing pending reads the document alone: the intents
	// this round started from.
	var source []intentProfile
	if sourceCatalog, err := document.ResolveDict(ctx, doc, catalogRef); err == nil && sourceCatalog != nil {
		source = outputIntentProfiles(ctx, writer.NewObjAllocator(0), doc, sourceCatalog)
	}
	if !slices.Equal(live, source) {
		doc.Logger.Debug("remediation: annot-color: output intents changed this round, deferring to the next",
			slog.Any("source", source), slog.Any("live", live))
		return nil, nil
	}
	// output-intent installs nothing on a document whose cross-reference table
	// was rebuilt, and the writer rewrites such a document in full, so the
	// intent it installs arrives in the next round, from a sound source.
	if doc.Rebuilt {
		doc.Logger.Debug("remediation: annot-color: rebuilt document, deferring to the next round")
		return nil, nil
	}

	pagesObj, ok := catalogDict["Pages"]
	if !ok {
		return nil, nil
	}
	var pages []annotPageEntry
	collectPageRefs(ctx, alloc, doc, pagesObj, &pages, make(map[int]bool))

	var count int
	for _, pe := range pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		annotsObj, ok := pe.dict["Annots"]
		if !ok {
			continue
		}
		// installInlineAnnots rebuilds this array wholesale and writes it back,
		// so it is read through the allocator: annotations and forbidden-annots
		// run first and compact it at its own object number.
		annots := liveArray(ctx, alloc, doc, annotsObj)
		if len(annots) == 0 {
			continue
		}

		var inlineFixed map[int]ast.PdfDict
		for i, annotObj := range annots {
			var annotRef ast.PdfIndirectRef
			if r, ok := annotObj.(ast.PdfIndirectRef); ok {
				annotRef = r
			}
			annotDict := liveAnnotation(ctx, alloc, doc, annotObj)
			if annotDict == nil {
				continue
			}
			if !colorArrayPresent(ctx, doc, annotDict, "C") && !colorArrayPresent(ctx, doc, annotDict, "IC") {
				continue
			}

			newAnnot := make(ast.PdfDict, len(annotDict))
			maps.Copy(newAnnot, annotDict)
			delete(newAnnot, "C")
			delete(newAnnot, "IC")

			if annotRef.ObjectNum > 0 {
				alloc.AllocAt(annotRef.ObjectNum, newAnnot)
				count++
				continue
			}
			if inlineFixed == nil {
				inlineFixed = make(map[int]ast.PdfDict, 1)
			}
			inlineFixed[i] = newAnnot
		}
		count += installInlineAnnots(ctx, alloc, doc, pe, annotsObj, annots, inlineFixed)
	}

	if count == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("removed /C and /IC colour arrays from %d annotation(s): the PDF/A-1 output intent is not RGB", count)}, nil
}

// colorArrayPresent reports whether annot carries key with a value that is not
// null, the reading ISO 19005-1, 6.5.3 test 3 applies to /C and /IC.
func colorArrayPresent(ctx context.Context, doc *document.Document, annot ast.PdfDict, key ast.PdfName) bool {
	v, ok := annot[key]
	return ok && !entryResolvesToNull(ctx, doc, v)
}

// annotColorPresent reports whether an annotation on some page of catalog
// carries a /C or /IC array [fixAnnotColor] would remove, reading every object
// through alloc as that fixer does.
//
// An annotation whose subtype the run removes does not count. ISO 19005-1,
// 6.5.2 forbids its subtype, and the annotations fixer, which every 6.5.3
// violation brings in, removes it later in the same round (see
// [annotSubtypeRemoved]), so its arrays never reach the output.
//
// # Corpus evidence
//
//   - verapdf PDF_A-4 6-3-3-t01-pass-d and 6-3-2-t01-fail-u, pdfjs issue20062:
//     the only coloured annotation is one PDF/A-1 forbids, which aggressive
//     mode removes, so counting it added an sRGB intent the output did not
//     need.
func annotColorPresent(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, catalog ast.PdfDict, opts Options, profile string) bool {
	pagesObj, ok := catalog["Pages"]
	if !ok {
		return false
	}
	var pages []annotPageEntry
	collectPageRefs(ctx, alloc, doc, pagesObj, &pages, make(map[int]bool))
	for _, pe := range pages {
		if ctx.Err() != nil {
			return false
		}
		annotsObj, ok := pe.dict["Annots"]
		if !ok {
			continue
		}
		for _, annotObj := range liveArray(ctx, alloc, doc, annotsObj) {
			annot := liveDictValue(ctx, alloc, doc, annotObj)
			if annot == nil {
				continue
			}
			if !colorArrayPresent(ctx, doc, annot, "C") && !colorArrayPresent(ctx, doc, annot, "IC") {
				continue
			}
			// Best-effort: an unreadable /Subtype yields an empty name, which
			// the annotations fixer removes, exactly as it reads it.
			subtype, _ := document.ResolveName(ctx, doc, annot["Subtype"])
			if !annotSubtypeRemoved(subtype, opts, profile) {
				return true
			}
		}
	}
	return false
}

// intentProfile is one output intent that carries a destination profile: its
// /S subtype and the profile's data colour space signature, empty when the
// profile cannot be read.
type intentProfile struct {
	subtype    ast.PdfName
	colorSpace string
}

// outputIntentProfiles lists, in array order, the output intents in catalog
// that carry a destination profile, reading every object through alloc.
func outputIntentProfiles(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, catalog ast.PdfDict) []intentProfile {
	oiObj, ok := catalog["OutputIntents"]
	if !ok {
		return nil
	}
	var out []intentProfile
	for _, entry := range liveArray(ctx, alloc, doc, oiObj) {
		intent := liveDictValue(ctx, alloc, doc, entry)
		if intent == nil {
			continue
		}
		dest, ok := intent["DestOutputProfile"]
		if !ok || entryResolvesToNull(ctx, doc, dest) {
			continue
		}
		// Best-effort: an unreadable /S names no subtype, so it is not the
		// PDF/A-1 intent.
		subtype, _ := document.ResolveName(ctx, doc, intent["S"])
		out = append(out, intentProfile{subtype: subtype, colorSpace: liveProfileColorSpace(ctx, alloc, doc, dest)})
	}
	return out
}

// admitsAnnotColor reports whether profiles make ISO 19005-1, 6.5.3 admit an
// annotation colour array: a GTS_PDFA1 intent carries an RGB destination
// profile and no intent carries one that is not RGB. See [fixAnnotColor] for
// why every carried profile counts.
func admitsAnnotColor(profiles []intentProfile) bool {
	pdfa1RGB := false
	for _, p := range profiles {
		if p.colorSpace != "RGB " {
			return false
		}
		if p.subtype == "GTS_PDFA1" {
			pdfa1RGB = true
		}
	}
	return pdfa1RGB
}

// liveProfileColorSpace returns the data colour space signature of the ICC
// profile obj names, read through the allocator so a profile an earlier fixer
// installed or replaced is the one measured. It returns "" when the profile
// cannot be read or decoded or is too short to carry the signature.
func liveProfileColorSpace(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, obj ast.PdfObject) string {
	var stream ast.PdfStream
	if ref, isRef := obj.(ast.PdfIndirectRef); isRef {
		s, ok := liveStream(ctx, alloc, doc, ref)
		if !ok {
			return ""
		}
		stream = s
	} else {
		s, err := document.ResolveStream(ctx, doc, obj)
		if err != nil {
			return ""
		}
		stream = s
	}
	data := iccProfileData(ctx, doc, stream)
	if len(data) < iccColorSpaceEnd {
		return ""
	}
	return string(data[iccColorSpaceStart:iccColorSpaceEnd])
}
