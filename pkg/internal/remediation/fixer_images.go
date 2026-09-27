package remediation

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

func matchImageIssues(v compliance.Violation) bool {
	return descriptionContains(v, "Interpolate") || descriptionContains(v, "OPI key")
}

func fixImageIssues(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
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
			return nil, fmt.Errorf("fixing image issues on page %d: %w", i+1, err)
		}

		if page.Resources == nil {
			continue
		}

		pageNotes, err := fixImageXObjects(ctx, alloc, doc, page.Resources, visited)
		if err != nil {
			return nil, fmt.Errorf("fixing image issues on page %d: %w", i+1, err)
		}
		notes = append(notes, pageNotes...)
	}

	inline, err := fixInlineImageInterpolation(ctx, alloc, doc)
	if err != nil {
		return nil, fmt.Errorf("fixing inline image interpolation: %w", err)
	}
	if inline != "" {
		notes = append(notes, inline)
	}

	return notes, nil
}

func fixImageXObjects(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, resources ast.PdfDict, visited map[int]bool) ([]string, error) {
	xobjObj, ok := resources["XObject"]
	if !ok {
		return nil, nil
	}

	xobjDict, err := document.ResolveDict(ctx, doc, xobjObj)
	if err != nil {
		doc.Logger.Debug("resolving XObject dict for image fixes", slog.Any("err", err))
		return nil, nil
	}
	if xobjDict == nil {
		return nil, nil
	}

	var notes []string

	// Sorted rather than ranged: the notes below name the image they describe,
	// so Go's randomised map order would vary the order of the fix descriptions
	// the caller reports, and those reach `litho remediate` both as printed
	// lines and as the JSON "fixed" array. The dict rewrites themselves are
	// keyed by object number and so are order-independent; only the report is.
	for _, name := range sortedDictKeys(xobjDict) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		entryObj := xobjDict[name]

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
		if subtype != "Image" {
			continue
		}

		modified := false
		newDict := make(ast.PdfDict, len(xobjStream.Dict))
		maps.Copy(newDict, xobjStream.Dict)

		// Set /Interpolate to false if true (clause 6.2.4).
		if interpObj, ok := newDict["Interpolate"]; ok {
			if b, ok := interpObj.(ast.PdfBool); ok && bool(b) {
				newDict["Interpolate"] = ast.PdfBool(false)
				modified = true
				notes = append(notes, fmt.Sprintf("set /Interpolate to false in image %s", string(name)))
			}
		}

		// Remove /OPI key (clauses 6.2.7.1, 6.2.8.1).
		if _, ok := newDict["OPI"]; ok {
			delete(newDict, "OPI")
			modified = true
			notes = append(notes, fmt.Sprintf("removed /OPI from image %s", string(name)))
		}

		if modified {
			// Carry the original body across. Only the dict changed, and a nil
			// body serialises as /Length 0, which drops the image's pixels
			// (the same trap fixResourcesGraphics guards against). The bytes
			// stay raw so they still match the /Filter kept in newDict.
			data, err := xobjStream.Bytes(ctx)
			if err != nil {
				// The payload is unreadable, so it cannot be carried across.
				// Leaving the object untouched keeps the image intact and the
				// violation visible, which beats writing a valid dict over an
				// empty stream.
				doc.Logger.Debug("remediation: skipping image dict fix, body read failed",
					slog.String("name", string(name)), slog.Any("err", err))
				continue
			}
			alloc.AllocAt(ref.ObjectNum, ast.NewStream(newDict, data))
		}
	}

	return notes, nil
}

// interpolationKeys are the keys an inline image's dictionary asks for
// interpolation with: /Interpolate, and its abbreviation /I (ISO 32000-1,
// 8.9.7, Table 93).
var interpolationKeys = []ast.PdfName{"I", "Interpolate"}

// fixInlineImageInterpolation respells false every /I and /Interpolate true in
// the dictionaries of the inline images of every content stream the pages of
// doc draw, which [walkContentStreams] reaches: a page's content, and the form
// XObjects, tiling pattern cells, Type 3 glyphs and annotation appearances it
// draws, the streams compliance judges the image rules in. It returns the
// report of what it changed, or an empty one.
//
// ISO 19005-1, 6.2.4, ISO 19005-2/3, 6.2.8 and ISO 19005-4, 6.2.7.1 hold every
// image dictionary to an /Interpolate of false, which an inline image writes
// /I, and veraPDF judges each inline image as an image of its own. Each value
// is respelled in place ([respellInlineImageInterpolation]), so the image data
// is carried across byte for byte, and an image the splice cannot rewrite is
// left as written, its finding a residual for validation to report.
//
// # Corpus evidence
//
//   - All corpora: 8 files hold inline images that fail ISO 19005-1, 6.2.4
//     t3, ISO 19005-2, 6.2.8 t3 and ISO 19005-4, 6.2.7.1 t3, in page content
//     and, in pdfjs issue20722.pdf, tiling pattern cells. Their PDF/A-1b,
//     PDF/A-2b and PDF/A-4 remediations, default and aggressive, kept every
//     one, and now keep none, on litho and veraPDF 1.30.2 alike, and no other
//     rule moves on either tool.
//   - govdocs thread_100_100877.pdf: the 97 findings go, 110 residual
//     findings become 13 at each level.
//   - pdfjs bug1065245.pdf, issue20722.pdf at PDF/A-2b and PDF/A-4, and verapdf
//     PDF_A-1b__veraPDF_test_suite_6-2-4-t03-fail-a.pdf,
//     PDF_A-2b__veraPDF_test_suite_6-2-8-1-t02-fail-b.pdf and
//     PDF_A-4__veraPDF_test_suite_6-2-7-1-t03-fail-b.pdf: 28 outputs come
//     out conforming on both tools.
//   - All corpora: the inline image data and the extracted text of the 84
//     outputs of the 14 files that write /I, /Interpolate or /Intent in an
//     inline image are unchanged, and the outputs of the 6 with no such
//     finding are byte for byte the same.
//   - litho inlineinterp_2b.pdf: holds such an image in a form XObject, a
//     Type 3 glyph and an annotation appearance, which no other file does.
func fixInlineImageInterpolation(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) (string, error) {
	images := 0
	streams, err := walkContentStreams(ctx, alloc, doc, "images", func(ctx context.Context, data []byte) ([]byte, func()) {
		out, n := respellInlineImageInterpolation(ctx, data)
		if n == 0 {
			return nil, nil
		}
		return out, func() { images += n }
	})
	if err != nil {
		return "", err
	}
	if images == 0 {
		return "", nil
	}
	return fmt.Sprintf("set /Interpolate to false in %d inline image(s) in %d content stream(s)", images, streams), nil
}

// respellInlineImageInterpolation returns data with every /I and /Interpolate
// true of the dictionaries of its inline images respelled false in place, and
// how many images it changed ([respellInlineImageEntries]). A dictionary that
// writes both has both respelled: veraPDF judges it by /I alone, as
// compliance does, and a viewer may read either.
func respellInlineImageInterpolation(ctx context.Context, data []byte) ([]byte, int) {
	// The keyword true is spelled one way (ISO 32000-1, 7.3.2), so a stream
	// that does not hold it asks for no interpolation, and is not read.
	if !bytes.Contains(data, []byte("true")) {
		return data, 0
	}
	return respellInlineImageEntries(ctx, data, imageEntryRespell{
		splice: func(data []byte, key string, tok lexer.Token, at tokenPlace) (tokenSplice, bool) {
			if !isInterpolationKey(key) || tok.Type != lexer.TokenKeyword || tok.Value != "true" {
				return tokenSplice{}, false
			}
			start := at.end - len(tok.Value)
			if start < 0 || at.end > len(data) || string(data[start:at.end]) != tok.Value {
				return tokenSplice{}, false
			}
			return tokenSplice{start: start, end: at.end, text: "false"}, true
		},
		fixed: uninterpolated,
	})
}

// isInterpolationKey reports whether key, a name token as the lexer reads it,
// is one of [interpolationKeys].
func isInterpolationKey(key string) bool {
	name, ok := strings.CutPrefix(key, "/")
	return ok && slices.Contains(interpolationKeys, ast.PdfName(name))
}

// uninterpolated returns dict, an inline image's dictionary, with each of
// [interpolationKeys] that holds true set to false, and how many it set.
func uninterpolated(dict ast.PdfDict) (ast.PdfDict, int) {
	fixed, n := dict, 0
	for _, key := range interpolationKeys {
		if b, ok := dict[key].(ast.PdfBool); ok && bool(b) {
			if n == 0 {
				fixed = maps.Clone(dict)
			}
			fixed[key] = ast.PdfBool(false)
			n++
		}
	}
	return fixed, n
}
