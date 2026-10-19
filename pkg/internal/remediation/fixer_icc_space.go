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
	"github.com/gotenberg/litho/pkg/internal/graphics/icc"
)

// maxICCBasedSearchDepth bounds how deeply [collectICCBasedProfiles] descends
// into the direct objects of one indirect object looking for an /ICCBased
// array. Anti-DoS guard, not spec-mandated: the deepest place ISO 32000-1
// writes one directly is a /Resources dictionary holding a /ColorSpace
// dictionary holding an /Indexed space over a DeviceN whose /Colorants hold a
// Separation over it (8.6.6), seven levels, so 32 leaves every conformant
// colour space whole while a crafted chain of direct arrays stops.
const maxICCBasedSearchDepth = 32

// matchICCSpaceV2 matches ISO 19005-1, 6.2.3.2 t1 when it fails an ICCBased
// colour space for its profile's version: PDF/A-1 rests on PDF 1.4, whose
// ICCBased spaces take ICC.1:1998-09 profiles (PDF Reference 1.4, 4.5.4), and
// veraPDF's test reads "version < 3.0".
func matchICCSpaceV2(v compliance.Violation) bool {
	return v.Rule.Specification == compliance.Rule_19005_1_6_2_3_2_t01.Specification &&
		clausePrefix(v, "6.2.3.2") && v.Rule.TestNumber == 1 &&
		contextContains(v, "exceeds the PDF/A part ceiling")
}

// fixICCSpaceV2 re-encodes, under PDF/A-1, every profile stream an /ICCBased
// colour space names whose ICC version PDF 1.4 predates as a version 2
// profile with the same colour transforms ([icc.ToV2]). ISO 19005-1, 6.2.3.2
// asks of every ICCBased space the profile of PDF Reference 1.4, 4.5.4, and
// PDF/A-2 and later admit version 4 (ISO 19005-2, 6.2.4.2), so other parts
// keep the profile they have.
//
// A stream is always an indirect object (ISO 32000-1, 7.3.8.1), so the
// converted profile is written at the source stream's own object number: every
// colour space naming it, on a page, an image, a shading, a form, an
// appearance stream, a glyph or a group, reads the one version 2 profile. A
// profile stream another fixer has already replaced, an output intent's
// destination profile that corrupt-icc re-encoded among them, is left to that
// replacement.
//
// A profile [icc.ToV2] refuses, one holding a lutAtoBType table among them,
// stays as it is and keeps its violation in the report's residual. The space
// characterizes the colours of the content painted in it, so neither a
// resampled table nor a stock profile of the same colour space converts them
// as the source does, the policy corrupt-icc applies to a destination profile
// for the printing condition it names. A profile the test fails for its device
// class as well, an abstract one, is re-encoded all the same: the conversion
// changes no colour, and the class violation stays.
//
// # Corpus evidence
//
//   - All corpora: 15 files fail ISO 19005-1, 6.2.3.2 t1 on the ICC version at
//     PDF/A-1b. Their PDF/A-1b remediation, default and aggressive, clears it
//     from 5: pdfjs issue17689.pdf (600 failed checks), issue19319.pdf,
//     issue19971.pdf and pdfjs_wikipedia.pdf, and verapdf
//     PDF_UA-1__7.20-t02-pass-a.pdf, and veraPDF agrees on every output.
//   - pdfjs issue2856.pdf: its two version 4.0 RGB profiles hold an mAB A2B0
//     table, which ToV2 refuses, and both tools keep the violation.
//   - govdocs thread_100_100114.pdf, thread_200_200629.pdf,
//     thread_400_400765.pdf and thread_200_200973.pdf: their profile streams
//     hold no ICC profile (version bytes 44 and 88), out of ToV2's reach.
//   - litho defaultcs_4.pdf and csnames_2b.pdf: their abstract profiles are
//     re-encoded and keep the device class violation, and iccbased_4.pdf's
//     version 5 profile is refused.
func fixICCSpaceV2(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, profile string) ([]string, error) {
	if compliance.PDFAPart(profile) != 1 {
		return nil, nil
	}
	refs, err := collectICCBasedProfiles(ctx, alloc, doc)
	if err != nil {
		return nil, err
	}
	converted := 0
	for _, ref := range refs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, pending := alloc.Pending()[ref.ObjectNum]; pending {
			continue
		}
		stream, err := document.ResolveStream(ctx, doc, ref)
		if err != nil {
			continue
		}
		data, err := doc.DecodeStream(ctx, stream)
		if err != nil || len(data) < 128 || data[8] < 3 {
			continue
		}
		v2, err := icc.ToV2(data)
		if err != nil {
			doc.Logger.Debug("remediation: icc-space-v2: ICCBased profile has no lossless version 2 form, ISO 19005-1, 6.2.3.2 t1 stays",
				slog.Int("object", ref.ObjectNum), slog.Any("err", err))
			continue
		}
		alloc.AllocAt(ref.ObjectNum, v2ProfileStream(stream.Dict, v2))
		converted++
	}
	if converted == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("re-encoded %d ICCBased colour space profile(s) as ICC version 2", converted)}, nil
}

// v2ProfileStream returns a profile stream of dict holding the version 2
// profile v2. The profile is encoded afresh, so the filter chain describing
// the old body no longer applies. A sampled curve is larger than the
// parametric one it replaces, and Flate, which every PDF/A part admits, keeps
// it near the source's size.
func v2ProfileStream(dict ast.PdfDict, v2 []byte) ast.PdfStream {
	newDict := make(ast.PdfDict, len(dict))
	maps.Copy(newDict, dict)
	delete(newDict, "Filter")
	delete(newDict, "DecodeParms")
	delete(newDict, "DL")
	return flateStream(newDict, v2)
}

// collectICCBasedProfiles returns, in ascending object-number order, the
// references to the profile streams the /ICCBased arrays of the document name (ISO 32000-1,
// 8.6.5.5), wherever an object the file defines writes one, directly or as an
// object of its own. Objects another fixer has queued are read in their queued
// form.
func collectICCBasedProfiles(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) ([]ast.PdfIndirectRef, error) {
	found := make(map[int]ast.PdfIndirectRef)
	for objNum, entry := range sweptObjects(doc) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if entry.Free || objNum == 0 {
			continue
		}
		obj, pending := alloc.Pending()[objNum]
		if !pending {
			resolved, err := doc.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: objNum, GenNum: entry.Generation})
			if err != nil || resolved == nil {
				continue
			}
			obj = resolved
		}
		if s, ok := obj.(ast.PdfStream); ok {
			obj = s.Dict
		}
		findICCBasedProfiles(obj, found, 0)
	}
	refs := make([]ast.PdfIndirectRef, 0, len(found))
	for _, objNum := range slices.Sorted(maps.Keys(found)) {
		refs = append(refs, found[objNum])
	}
	return refs, nil
}

// findICCBasedProfiles records in found the profile stream of every /ICCBased
// array obj holds directly, down to [maxICCBasedSearchDepth]. It follows no
// reference: an object of its own is reached by the sweep of
// [collectICCBasedProfiles], and a direct object is a tree.
func findICCBasedProfiles(obj ast.PdfObject, found map[int]ast.PdfIndirectRef, depth int) {
	if depth > maxICCBasedSearchDepth {
		return
	}
	switch v := obj.(type) {
	case ast.PdfArray:
		if len(v) >= 2 {
			if name, ok := v[0].(ast.PdfName); ok && name == "ICCBased" {
				if ref, ok := v[1].(ast.PdfIndirectRef); ok && ref.ObjectNum > 0 {
					found[ref.ObjectNum] = ref
				}
				return
			}
		}
		for _, e := range v {
			findICCBasedProfiles(e, found, depth+1)
		}
	case ast.PdfDict:
		for _, e := range v {
			findICCBasedProfiles(e, found, depth+1)
		}
	}
}
