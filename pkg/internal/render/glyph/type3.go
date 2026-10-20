package glyph

import (
	"context"
	"log/slog"
	"math"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/render/geom"
)

// type3Font holds what a Type 3 font draws its glyphs with (ISO 32000-1,
// 9.6.5, Table 112).
type type3Font struct {
	charProcs ast.PdfDict
	resources ast.PdfDict
	matrix    geom.Matrix
	logger    *slog.Logger
	doc       *document.Document
}

// defaultType3Matrix stands in for a /FontMatrix that is missing, not six
// numbers, or singular. Table 112 requires the entry; the common
// [0.001 0 0 0.001 0 0] is what PDF.js falls back on too (fonts.js,
// FONT_IDENTITY_MATRIX).
var defaultType3Matrix = geom.Matrix{0.001, 0, 0, 0.001, 0, 0}

// loadType3 reads a Type 3 font's /CharProcs, /Resources and /FontMatrix.
func loadType3(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) *type3Font {
	t := &type3Font{matrix: defaultType3Matrix, logger: doc.Logger, doc: doc}
	var err error
	if t.charProcs, err = document.ResolveDict(ctx, doc, fontDict["CharProcs"]); err != nil {
		doc.Logger.Debug("glyph: resolving Type 3 /CharProcs", slog.Any("err", err))
	}
	if t.resources, err = document.ResolveDict(ctx, doc, fontDict["Resources"]); err != nil {
		doc.Logger.Debug("glyph: resolving Type 3 /Resources", slog.Any("err", err))
	}
	if arr, err := document.ResolveArray(ctx, doc, fontDict["FontMatrix"]); err == nil && len(arr) == 6 {
		var m geom.Matrix
		valid := true
		for i, el := range arr {
			resolved, err := document.ResolveObject(ctx, doc, el)
			if err != nil {
				valid = false
				break
			}
			switch v := resolved.(type) {
			case ast.PdfInt:
				m[i] = float64(v)
			case ast.PdfReal:
				m[i] = float64(v)
			default:
				valid = false
			}
		}
		if valid && m.Det() != 0 && !math.IsNaN(m.Det()) && !math.IsInf(m.Det(), 0) {
			t.matrix = m
		}
	}
	return t
}

// type3Glyph resolves a Type 3 code: the glyph name its /Encoding gives it
// (9.6.6.1) keys the /CharProcs glyph description. A code that names no
// description draws nothing, as a Type 3 font has no .notdef glyph to fall
// back on.
func (f *Font) type3Glyph(ctx context.Context, code Code, g *Glyph) {
	t := f.type3
	g.Source = SourceType3
	g.Matrix = t.matrix
	g.Resources = t.resources
	name := f.enc.names[code.Bytes[0]]
	obj, ok := t.charProcs[ast.PdfName(name)]
	if name == "" || !ok {
		g.Notdef = true
		return
	}
	stream, err := document.ResolveStream(ctx, t.doc, obj)
	if err != nil || stream.Dict == nil {
		t.logger.Debug("glyph: resolving Type 3 glyph description", slog.String("name", name), slog.Any("err", err))
		g.Notdef = true
		return
	}
	g.CharProc = &stream
}
