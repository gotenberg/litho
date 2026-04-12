package mutations

import (
	"context"
	"fmt"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// TextStampOptions holds everything needed to stamp a line of text on one
// or more pages. It is the internal form of the public TextStamp API in
// pkg/pdf.
type TextStampOptions struct {
	// Text is the string to draw. It is escaped for PDF literal syntax by
	// the stamp engine for standard fonts, and hex-encoded via the font's
	// Unicode→GID map for custom TTF fonts.
	Text string
	// X and Y are the position in points from the page's bottom-left
	// corner. When Rotation is non-zero, (X, Y) is the rotation origin.
	X, Y float64
	// FontSize is the text size in points. Zero defaults to 12.
	FontSize float64
	// Opacity is 0.0-1.0 where 1.0 is fully opaque. A zero value is
	// treated as 1.0 for back-compat with the original StampText API.
	Opacity float64
	// Color is the RGB fill color (0.0-1.0 components). The zero value
	// draws in black.
	Color [3]float64
	// Rotation is the counter-clockwise rotation in degrees around (X, Y).
	Rotation float64
	// StandardFont selects one of the 14 standard PDF Type1 fonts by its
	// /BaseFont name (e.g., "Helvetica", "Times-Roman"). Ignored if
	// TTFBytes is set.
	StandardFont ast.PdfName
	// TTFBytes embeds a TrueType font file. When set, the stamp uses a
	// Type0 composite font with Identity-H encoding and a generated
	// ToUnicode CMap.
	TTFBytes []byte
}

// StampText draws a single line of text on the given 1-based page numbers.
// See [TextStampOptions] for the available parameters. If pageNums is
// empty, the stamp is applied to every page.
func StampText(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	pages []*document.Page,
	opts TextStampOptions,
	pageNums []int,
) error {
	if len(pageNums) == 0 {
		pageNums = make([]int, len(pages))
		for i := range pages {
			pageNums[i] = i + 1
		}
	}

	fontSize := opts.FontSize
	if fontSize == 0 {
		fontSize = 12
	}

	// Allocate the font resource.
	var (
		fontRef     ast.PdfIndirectRef
		fontResName ast.PdfName
		encoded     string
		err         error
	)
	if len(opts.TTFBytes) > 0 {
		var f *embeddedTTF
		f, err = parseTTF(opts.TTFBytes)
		if err != nil {
			return fmt.Errorf("parsing TTF for stamp: %w", err)
		}

		// Subset the raw TTF to just the glyphs referenced by the stamp
		// text (plus .notdef and any composite-glyph dependencies). The
		// subsetter reuses the parsed font's Unicode→GID map so text
		// encoding continues to work with the same embeddedTTF. On any
		// subsetting failure we fall back to full embedding so the
		// stamp still renders. Correctness over size.
		keepGIDs := collectUsedGIDs(opts.Text, f)
		if subsetBytes, subErr := subsetTTF(opts.TTFBytes, keepGIDs); subErr == nil {
			f.raw = subsetBytes
		} else if doc != nil {
			doc.Logger.Debug("TTF subsetting failed, falling back to full embedding",
				"err", subErr,
			)
		}

		fontRef, err = embedTTFFont(ctx, alloc, f)
		if err != nil {
			return fmt.Errorf("embedding TTF for stamp: %w", err)
		}
		encoded = encodeTextAsGIDs(opts.Text, f)
		fontResName = ast.PdfName(fmt.Sprintf("StampF%d", fontRef.ObjectNum))
	} else {
		baseFont := opts.StandardFont
		if baseFont == "" {
			baseFont = "Helvetica"
		}
		fontDict := ast.PdfDict{
			"Type":     ast.PdfName("Font"),
			"Subtype":  ast.PdfName("Type1"),
			"BaseFont": baseFont,
		}
		// Symbol and ZapfDingbats use their built-in encodings; all other
		// standard fonts use WinAnsiEncoding so extended Latin characters
		// render correctly.
		if baseFont != "Symbol" && baseFont != "ZapfDingbats" {
			fontDict["Encoding"] = ast.PdfName("WinAnsiEncoding")
		}
		fontRef = alloc.Alloc(fontDict)
		fontResName = ast.PdfName(fmt.Sprintf("StampF%d", fontRef.ObjectNum))
		encoded = "(" + escapePDFString(opts.Text) + ")"
	}

	extGSName, extGSRef := buildExtGState(alloc, opts.Opacity)

	var ops strings.Builder
	ops.WriteString("q ")
	ops.WriteString(extGStateInvocation(extGSName))

	// Fill color: "r g b rg" (defaults to black when all zero).
	fmt.Fprintf(&ops, "%s %s %s rg ",
		FormatFloat(opts.Color[0]),
		FormatFloat(opts.Color[1]),
		FormatFloat(opts.Color[2]),
	)

	if opts.Rotation != 0 {
		// Translate to the rotation origin, rotate, then draw at (0, 0).
		ops.WriteString(buildTransformMatrix(opts.X, opts.Y, 1, 1, opts.Rotation))
		fmt.Fprintf(&ops, "BT /%s %s Tf 0 0 Td %s Tj ET ",
			fontResName, FormatFloat(fontSize), encoded,
		)
	} else {
		fmt.Fprintf(&ops, "BT /%s %s Tf %s %s Td %s Tj ET ",
			fontResName, FormatFloat(fontSize),
			FormatFloat(opts.X), FormatFloat(opts.Y),
			encoded,
		)
	}
	ops.WriteString("Q")

	resources := []stampResource{
		{category: "Font", name: fontResName, ref: fontRef},
	}
	if extGSName != "" {
		resources = append(resources, stampResource{
			category: "ExtGState", name: extGSName, ref: extGSRef,
		})
	}

	return applyStampToPages(ctx, doc, alloc, pages, pageNums, resources, ops.String())
}
