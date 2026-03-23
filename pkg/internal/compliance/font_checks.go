package compliance

import (
	"context"
	"fmt"
	"log/slog"
	"math"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/typography"
	"github.com/gotenberg/litho/pkg/models"
)

// checkGlyphWidthConsistency verifies that for every embedded font used for
// rendering, the glyph width information in the font dictionary (/Widths or
// /W) is consistent with the widths in the embedded font program
// (6.2.11.5 / 6.2.10.5).
func checkGlyphWidthConsistency(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	var errs []error
	fonts := collectFontDicts(ctx, doc, catalog)
	for name, fontDict := range fonts {
		if ctx.Err() != nil {
			return errs
		}

		// Skip Type3 fonts (glyphs defined by content streams, not binary).
		if isType3Font(ctx, doc, fontDict) {
			continue
		}

		// Get the font descriptor.
		fdDict := findFontDescriptor(ctx, doc, fontDict)
		if fdDict == nil {
			continue
		}

		// Get the embedded font stream.
		fontData := extractFontProgramData(ctx, doc, fdDict)
		if fontData == nil {
			continue // Not embedded.
		}

		// Extract binary widths from the font program.
		binaryWidths := typography.ExtractBinaryWidths(fontData)
		if binaryWidths == nil {
			continue
		}
		unitsPerEm := typography.ReadUnitsPerEm(fontData)
		if unitsPerEm == 0 {
			unitsPerEm = 1000 //nolint:mnd // safe default
		}

		// Compare against PDF dictionary widths.
		if err := compareWidths(ctx, doc, fontDict, binaryWidths, unitsPerEm, name); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

// extractFontProgramData gets the raw font program bytes from FontDescriptor.
func extractFontProgramData(ctx context.Context, doc *document.Document, fdDict ast.PdfDict) []byte {
	for _, key := range []ast.PdfName{"FontFile2", "FontFile3", "FontFile"} {
		ffObj, ok := fdDict[key]
		if !ok {
			continue
		}
		stream, err := document.ResolveStream(ctx, doc, ffObj)
		if err != nil {
			doc.Logger.Debug("resolving font program stream", slog.Any("err", err))
		}
		if stream.Dict == nil {
			continue
		}
		data, err := doc.DecodeStream(stream)
		if err != nil {
			// Fall back to raw data.
			rawData, rawErr := stream.Bytes()
			if rawErr == nil && len(rawData) > 0 {
				return rawData
			}
			continue
		}
		return data
	}
	return nil
}

// compareWidths compares PDF-declared widths against binary font widths.
func compareWidths(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, binaryWidths map[uint16]uint16, unitsPerEm uint16, fontName string) error {
	// CIDFont (Type0): check /DescendantFonts[0]/W.
	// Simple fonts are skipped — char code → GID mapping requires cmap
	// parsing which is not available in the compliance module.
	if dfObj, ok := fontDict["DescendantFonts"]; ok {
		dfArr, err := document.ResolveArray(ctx, doc, dfObj)
		if err != nil {
			doc.Logger.Debug("resolving DescendantFonts array", slog.Any("err", err))
		}
		if len(dfArr) > 0 {
			cidDict, err := document.ResolveDict(ctx, doc, dfArr[0])
			if err != nil {
				doc.Logger.Debug("resolving CIDFont dict", slog.Any("err", err))
			}
			if cidDict != nil {
				if wObj, ok := cidDict["W"]; ok {
					return compareCIDWidths(ctx, doc, wObj, binaryWidths, unitsPerEm, fontName)
				}
			}
		}
	}

	return nil
}

// widthTolerance is the maximum allowed difference between PDF-declared
// and binary font widths, in 1/1000 text space units. PDF spec allows
// rounding differences, so a tolerance of 1.0 is used.
const widthTolerance = 1.0

// compareCIDWidths checks /W array against binary widths for CIDFonts.
func compareCIDWidths(ctx context.Context, doc *document.Document, wObj ast.PdfObject, binaryWidths map[uint16]uint16, unitsPerEm uint16, fontName string) error {
	wArr, err := document.ResolveArray(ctx, doc, wObj)
	if err != nil {
		doc.Logger.Debug("resolving CID width array", slog.Any("err", err))
	}
	if wArr == nil {
		return nil
	}

	// /W array format: [cid [w1 w2 ...]] or [cidFirst cidLast w]
	i := 0
	for i < len(wArr) {
		cidStartRes, err := document.ResolveObject(ctx, doc, wArr[i])
		if err != nil {
			doc.Logger.Debug("resolving CID start value", slog.Any("err", err))
		}
		cidStartInt, ok := cidStartRes.(ast.PdfInt)
		if !ok {
			i++
			continue
		}
		cidStart := int(cidStartInt)
		i++
		if i >= len(wArr) {
			break
		}

		nextRes, err := document.ResolveObject(ctx, doc, wArr[i])
		if err != nil {
			doc.Logger.Debug("resolving CID width entry", slog.Any("err", err))
		}
		switch next := nextRes.(type) {
		case ast.PdfArray:
			// [cidStart [w1 w2 w3 ...]]
			for j, wEntry := range next {
				cid := cidStart + j
				gid := uint16(cid) //nolint:gosec

				pdfWidth := document.GetFloat(wEntry)
				if pdfWidth == 0 {
					continue
				}
				bw, ok := binaryWidths[gid]
				if !ok {
					continue
				}
				expectedWidth := float64(bw) * 1000.0 / float64(unitsPerEm)
				if math.Abs(pdfWidth-expectedWidth) > widthTolerance {
					return fmt.Errorf("%w: %s CID %d (PDF=%.1f, font=%.1f)",
						models.ErrGlyphWidthMismatch, fontName, cid, pdfWidth, expectedWidth)
				}
			}
			i++
		case ast.PdfInt:
			// [cidStart cidEnd w]
			cidEnd := int(next)
			i++
			if i >= len(wArr) {
				break
			}
			pdfWidth := document.GetFloat(wArr[i])
			i++
			if pdfWidth == 0 {
				continue
			}
			for cid := cidStart; cid <= cidEnd; cid++ {
				gid := uint16(cid) //nolint:gosec
				bw, ok := binaryWidths[gid]
				if !ok {
					continue
				}
				expectedWidth := float64(bw) * 1000.0 / float64(unitsPerEm)
				if math.Abs(pdfWidth-expectedWidth) > widthTolerance {
					return fmt.Errorf("%w: %s CID %d (PDF=%.1f, font=%.1f)",
						models.ErrGlyphWidthMismatch, fontName, cid, pdfWidth, expectedWidth)
				}
			}
		default:
			i++
		}
	}
	return nil
}

// checkNotdefGlyphUsage detects explicit .notdef glyph references in font
// Encoding /Differences arrays (6.2.11.8 / 6.2.10.9). A .notdef entry
// indicates a character code maps to the undefined glyph.
func checkNotdefGlyphUsage(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	var errs []error
	fonts := collectFontDicts(ctx, doc, catalog)
	for name, fontDict := range fonts {
		encObj, ok := fontDict["Encoding"]
		if !ok {
			continue
		}
		encDict, err := document.ResolveDict(ctx, doc, encObj)
		if err != nil {
			doc.Logger.Debug("resolving font encoding dict", slog.Any("err", err))
		}
		if encDict == nil {
			continue
		}
		diffObj, ok := encDict["Differences"]
		if !ok {
			continue
		}
		diffArr, err := document.ResolveArray(ctx, doc, diffObj)
		if err != nil {
			doc.Logger.Debug("resolving encoding differences array", slog.Any("err", err))
		}
		if diffArr == nil {
			continue
		}
		for _, item := range diffArr {
			if n, ok := item.(ast.PdfName); ok && n == ".notdef" {
				errs = append(errs, fmt.Errorf("%w: %s",
					models.ErrNotdefGlyphUsed, name))
				break
			}
		}
	}
	return errs
}
