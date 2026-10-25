package annot

import (
	"context"
	"fmt"
	"log/slog"
	"math"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/render/geom"
)

// Mode selects which annotation flags govern visibility.
type Mode uint8

const (
	// View draws what a viewer shows on screen: /NoView hides an annotation.
	View Mode = iota

	// Print draws what a viewer prints: only annotations with /Print set.
	Print
)

// Options configure [Collect].
type Options struct {
	// Mode is [View] by default.
	Mode Mode

	// Synthesize builds an appearance for an annotation that has no
	// appearance stream, for the subtypes whose appearance ISO 32000-1
	// defines from the dictionary alone (see [Appearance.Synthesized]).
	// Without it such an annotation is skipped with [ReasonNoAppearance].
	Synthesize bool
}

// Annotation flags, ISO 32000-1, 12.5.3, Table 165.
const (
	FlagInvisible = 1 << 0
	FlagHidden    = 1 << 1
	FlagPrint     = 1 << 2
	FlagNoZoom    = 1 << 3
	FlagNoRotate  = 1 << 4
	FlagNoView    = 1 << 5
)

// ctxCheckInterval is how many annotations [Collect] reads between context
// checks.
const ctxCheckInterval = 64

// Appearance is one annotation appearance to draw.
//
// The renderer runs Stream as a form XObject (ISO 32000-1, 8.10) with CTM
// FormMatrix.Mul(A).Mul(pageCTM), clipped to BBox in form space, with
// Resources, or the page's resources when PageResources is set.
type Appearance struct {
	// Index is the annotation's position in the page's /Annots array.
	Index int

	// Ref is the annotation dictionary's reference, zero when /Annots holds
	// it directly.
	Ref ast.PdfIndirectRef

	// Subtype is the annotation's /Subtype.
	Subtype ast.PdfName

	// State is the /AS name that selected Stream from a subdictionary, empty
	// when /AP /N is a stream.
	State ast.PdfName

	// Stream is the appearance stream, a form XObject.
	Stream ast.PdfStream

	// StreamRef is Stream's reference, zero when it is written directly.
	StreamRef ast.PdfIndirectRef

	// Resources is the stream's /Resources, nil when it has none.
	Resources ast.PdfDict

	// PageResources reports that the stream has no /Resources entry. ISO
	// 32000-1, 7.8.3 then looks names up in the resources of the page the
	// form is drawn on, an obsolete PDF 1.1 usage that viewers still honour.
	PageResources bool

	// BBox is the form's /BBox, normalised, in form space.
	BBox geom.Rect

	// FormMatrix is the form's /Matrix, the identity when absent.
	FormMatrix geom.Matrix

	// Rect is the annotation's /Rect, normalised, in default user space.
	Rect geom.Rect

	// A is the matrix of Algorithm 8.1 (ISO 32000-1, 12.5.5) that maps the
	// transformed appearance box onto Rect.
	A geom.Matrix

	// Flags is the annotation's /F.
	Flags int

	// NoRotate and NoZoom report the flags of the same name (Table 165).
	// The caller applies them: they depend on the page's device transform.
	NoRotate, NoZoom bool

	// OC is the annotation's /OC entry as written, nil when absent (ISO
	// 32000-1, 8.11.3.3, Table 164).
	OC ast.PdfObject

	// FormOC is the appearance stream's own /OC entry as written, nil when
	// absent (8.11.3.3).
	FormOC ast.PdfObject

	// Synthesized reports that the file holds no appearance stream for the
	// annotation and Stream is one [Collect] built from its dictionary, as
	// [Options.Synthesize] asks. Stream then draws in default user space:
	// FormMatrix and A are the identity, and BBox covers the drawing.
	Synthesized bool
}

// Transform returns FormMatrix.Mul(A), the matrix that maps form space to
// default user space (Algorithm 8.1, step c).
func (a Appearance) Transform() geom.Matrix {
	return a.FormMatrix.Mul(a.A)
}

// Reason says why [Collect] leaves an annotation out.
type Reason uint8

const (
	// ReasonMalformed is an /Annots element that is not a dictionary, or a
	// dictionary with no usable /Rect.
	ReasonMalformed Reason = iota + 1

	// ReasonHidden is an annotation with /Hidden set.
	ReasonHidden

	// ReasonInvisible is a non-standard annotation with /Invisible set.
	ReasonInvisible

	// ReasonNoView is an annotation with /NoView set, in [View] mode.
	ReasonNoView

	// ReasonNotPrinted is an annotation without /Print, in [Print] mode.
	ReasonNotPrinted

	// ReasonClosedPopup is a /Popup annotation that is not open.
	ReasonClosedPopup

	// ReasonNoAppearance is an annotation with no normal appearance stream
	// for its state.
	ReasonNoAppearance

	// ReasonBadForm is an appearance stream that lacks a usable /BBox.
	ReasonBadForm
)

// String returns the reason's name.
func (r Reason) String() string {
	switch r {
	case ReasonMalformed:
		return "malformed"
	case ReasonHidden:
		return "hidden"
	case ReasonInvisible:
		return "invisible"
	case ReasonNoView:
		return "noview"
	case ReasonNotPrinted:
		return "not-printed"
	case ReasonClosedPopup:
		return "closed-popup"
	case ReasonNoAppearance:
		return "no-appearance"
	case ReasonBadForm:
		return "bad-form"
	}
	return fmt.Sprintf("reason(%d)", uint8(r))
}

// Skip is an annotation [Collect] leaves out.
type Skip struct {
	// Index is the annotation's position in the page's /Annots array.
	Index int

	// Subtype is the annotation's /Subtype, empty when unreadable.
	Subtype ast.PdfName

	// Reason says why it is left out.
	Reason Reason
}

// standardSubtypes lists the annotation types of ISO 32000-1, 12.5.6.1,
// Table 169, and those ISO 32000-2, Table 171 adds (Redact, Projection,
// RichMedia).
var standardSubtypes = map[ast.PdfName]bool{
	"Text": true, "Link": true, "FreeText": true, "Line": true,
	"Square": true, "Circle": true, "Polygon": true, "PolyLine": true,
	"Highlight": true, "Underline": true, "Squiggly": true, "StrikeOut": true,
	"Stamp": true, "Caret": true, "Ink": true, "Popup": true,
	"FileAttachment": true, "Sound": true, "Movie": true, "Widget": true,
	"Screen": true, "PrinterMark": true, "TrapNet": true, "Watermark": true,
	"3D": true, "Redact": true, "Projection": true, "RichMedia": true,
}

// Collect returns, in /Annots order, the appearances a viewer draws for page
// in the given mode, and the annotations it leaves out. It returns an error
// only when ctx is done: an unreadable /Annots draws no annotation, as a
// viewer would still draw the page.
func Collect(ctx context.Context, doc *document.Document, page *document.Page, opts Options) ([]Appearance, []Skip, error) {
	if page == nil || page.Dict == nil {
		return nil, nil, nil
	}
	annots, err := document.ResolveArray(ctx, doc, page.Dict["Annots"])
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, nil, fmt.Errorf("resolving page /Annots: %w", ctxErr)
		}
		doc.Logger.Debug("unreadable page /Annots, drawing no annotation", slog.Any("err", err))
		return nil, nil, nil
	}

	var (
		out   []Appearance
		skips []Skip
	)
	for i, elem := range annots {
		if i%ctxCheckInterval == 0 {
			if err := ctx.Err(); err != nil {
				return nil, nil, fmt.Errorf("collecting annotation %d: %w", i, err)
			}
		}
		app, subtype, reason := collectOne(ctx, doc, elem, opts)
		if reason != 0 {
			skips = append(skips, Skip{Index: i, Subtype: subtype, Reason: reason})
			continue
		}
		app.Index = i
		out = append(out, app)
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, fmt.Errorf("collecting annotations: %w", err)
	}
	return out, skips, nil
}

// collectOne reads one /Annots element. A non-zero reason means it is left
// out.
func collectOne(ctx context.Context, doc *document.Document, elem ast.PdfObject, opts Options) (Appearance, ast.PdfName, Reason) {
	dict, err := document.ResolveDict(ctx, doc, elem)
	if err != nil || dict == nil {
		doc.Logger.Debug("annotation is not a dictionary", slog.Any("err", err))
		return Appearance{}, "", ReasonMalformed
	}
	subtype, err := document.ResolveName(ctx, doc, dict["Subtype"])
	if err != nil {
		doc.Logger.Debug("unreadable annotation /Subtype", slog.Any("err", err))
	}

	flags := 0
	if fObj, err := document.ResolveObject(ctx, doc, dict["F"]); err == nil {
		if f, ok := fObj.(ast.PdfInt); ok {
			flags = int(f)
		}
		// Table 164 makes /F an integer; a real is not read as flags.
	} else {
		doc.Logger.Debug("unreadable annotation /F, reading it as 0", slog.Any("err", err))
	}
	if reason := visibility(ctx, doc, dict, subtype, flags, opts.Mode); reason != 0 {
		return Appearance{}, subtype, reason
	}

	rect, ok := readRect(ctx, doc, dict["Rect"])
	if !ok {
		// /Rect is required for every annotation (ISO 32000-1, 12.5.2,
		// Table 164): without it there is nowhere to draw.
		return Appearance{}, subtype, ReasonMalformed
	}

	stream, streamRef, state, ok := normalAppearance(ctx, doc, dict)
	if !ok {
		if !opts.Synthesize {
			return Appearance{}, subtype, ReasonNoAppearance
		}
		app, ok := synthesize(ctx, doc, dict, subtype, rect)
		if !ok {
			return Appearance{}, subtype, ReasonNoAppearance
		}
		app.Ref = refOf(elem)
		app.Subtype = subtype
		app.State = state
		app.Flags = flags
		// A Text annotation's icon behaves as if NoZoom and NoRotate were
		// set (ISO 32000-1, 12.5.6.4).
		app.NoRotate = flags&FlagNoRotate != 0 || subtype == "Text"
		app.NoZoom = flags&FlagNoZoom != 0 || subtype == "Text"
		app.OC = dict["OC"]
		return app, subtype, 0
	}

	// An appearance stream is run as a form XObject (12.5.5) whatever its
	// /Subtype says: producers omit it, and poppler 26.09.0 draws an
	// appearance even under /Subtype /Image. /BBox is required in a form
	// (8.10.2, Table 95), and poppler draws nothing without one.
	bbox, ok := readRect(ctx, doc, stream.Dict["BBox"])
	if !ok {
		return Appearance{}, subtype, ReasonBadForm
	}
	formMatrix := readMatrix(ctx, doc, stream.Dict["Matrix"])

	app := Appearance{
		Ref:        refOf(elem),
		Subtype:    subtype,
		State:      state,
		Stream:     stream,
		StreamRef:  streamRef,
		BBox:       bbox,
		FormMatrix: formMatrix,
		Rect:       rect,
		A:          algorithm81(bbox, formMatrix, rect),
		Flags:      flags,
		NoRotate:   flags&FlagNoRotate != 0,
		NoZoom:     flags&FlagNoZoom != 0,
		OC:         dict["OC"],
		FormOC:     stream.Dict["OC"],
	}
	res, err := document.ResolveDict(ctx, doc, stream.Dict["Resources"])
	if err != nil {
		doc.Logger.Debug("unreadable appearance /Resources, drawing without", slog.Any("err", err))
	}
	app.Resources = res
	// An absent or null /Resources (a null entry is an absent one, 7.3.9)
	// falls back to the page's; an unreadable one draws with none.
	app.PageResources = res == nil && err == nil
	return app, subtype, 0
}

// visibility applies the annotation flags of ISO 32000-1, 12.5.3, Table 165,
// and the /Open entry of a pop-up, and returns why the annotation is not
// drawn, or 0.
func visibility(ctx context.Context, doc *document.Document, dict ast.PdfDict, subtype ast.PdfName, flags int, mode Mode) Reason {
	// Hidden: do not display or print, whatever the annotation type.
	if flags&FlagHidden != 0 {
		return ReasonHidden
	}
	// Invisible applies only to a non-standard type with no handler, and
	// litho has a handler for none. Viewers diverge both ways: poppler
	// 26.09.0 draws an invisible annotation of any type, PDF.js hides one of
	// any type. The table is followed here.
	if flags&FlagInvisible != 0 && !standardSubtypes[subtype] {
		return ReasonInvisible
	}
	switch mode {
	case Print:
		// Print: print the annotation when set, otherwise never.
		if flags&FlagPrint == 0 {
			return ReasonNotPrinted
		}
	case View:
		// NoView: do not display on screen, though it may print (PDF 1.3).
		if flags&FlagNoView != 0 {
			return ReasonNoView
		}
	}
	// A pop-up shows its parent's text in a window while /Open is true and
	// is not displayed otherwise (12.5.6.14, Table 183, default false).
	// Poppler 26.09.0 draws a closed pop-up's appearance too; the table is
	// followed here.
	if subtype == "Popup" {
		open, err := document.ResolveObject(ctx, doc, dict["Open"])
		if err != nil {
			doc.Logger.Debug("unreadable pop-up /Open, reading it as false", slog.Any("err", err))
		}
		if b, ok := open.(ast.PdfBool); !ok || !bool(b) {
			return ReasonClosedPopup
		}
	}
	return 0
}

// normalAppearance returns the normal appearance stream of dict (ISO 32000-1,
// 12.5.5, Table 168): /AP /N when it is a stream, or the entry of the /N
// subdictionary that /AS names. It returns the state name in the second case.
func normalAppearance(ctx context.Context, doc *document.Document, dict ast.PdfDict) (ast.PdfStream, ast.PdfIndirectRef, ast.PdfName, bool) {
	ap, err := document.ResolveDict(ctx, doc, dict["AP"])
	if err != nil || ap == nil {
		if err != nil {
			doc.Logger.Debug("unreadable annotation /AP", slog.Any("err", err))
		}
		return ast.PdfStream{}, ast.PdfIndirectRef{}, "", false
	}
	nObj := ap["N"]
	n, err := document.ResolveObject(ctx, doc, nObj)
	if err != nil {
		doc.Logger.Debug("unreadable annotation /AP /N", slog.Any("err", err))
		return ast.PdfStream{}, ast.PdfIndirectRef{}, "", false
	}
	switch v := n.(type) {
	case ast.PdfStream:
		return v, refOf(nObj), "", true
	case ast.PdfDict:
		state := appearanceState(ctx, doc, dict, v)
		entry, ok := v[state]
		if !ok {
			// /AS names no appearance: nothing is drawn, as for a check
			// box whose off state has no stream.
			return ast.PdfStream{}, ast.PdfIndirectRef{}, state, false
		}
		s, err := document.ResolveStream(ctx, doc, entry)
		if err != nil || s.Dict == nil {
			if err != nil {
				doc.Logger.Debug("unreadable appearance state stream",
					slog.String("state", string(state)), slog.Any("err", err))
			}
			return ast.PdfStream{}, ast.PdfIndirectRef{}, state, false
		}
		return s, refOf(entry), state, true
	}
	return ast.PdfStream{}, ast.PdfIndirectRef{}, "", false
}

// appearanceState returns the state that selects from the /N subdictionary
// states.
//
// /AS is required when the appearance dictionary holds subdictionaries (ISO
// 32000-1, 12.5.2, Table 164). Without it, poppler, whose renders are the
// render gate's goldens, takes the only state when there is exactly one and
// /Off otherwise; that leniency is followed here.
func appearanceState(ctx context.Context, doc *document.Document, dict, states ast.PdfDict) ast.PdfName {
	as, err := document.ResolveName(ctx, doc, dict["AS"])
	if err == nil && as != "" {
		return as
	}
	if err != nil {
		doc.Logger.Debug("unreadable annotation /AS", slog.Any("err", err))
	}
	if len(states) == 1 {
		for name := range states {
			return name
		}
	}
	return "Off"
}

// readRect reads a rectangle (ISO 32000-1, 7.9.5) of four finite numbers,
// normalised so that Min holds the lower-left corner.
func readRect(ctx context.Context, doc *document.Document, obj ast.PdfObject) (geom.Rect, bool) {
	arr, err := document.ResolveArray(ctx, doc, obj)
	if err != nil || len(arr) != 4 {
		if err != nil {
			doc.Logger.Debug("unreadable rectangle", slog.Any("err", err))
		}
		return geom.Rect{}, false
	}
	arr = document.ResolveRectangle(ctx, doc, arr)
	var v [4]float64
	for i, e := range arr {
		f, ok := document.Number(e)
		if !ok || math.IsNaN(f) || math.IsInf(f, 0) {
			return geom.Rect{}, false
		}
		v[i] = f
	}
	return geom.Rect{
		Min: geom.Point{X: min(v[0], v[2]), Y: min(v[1], v[3])},
		Max: geom.Point{X: max(v[0], v[2]), Y: max(v[1], v[3])},
	}, true
}

// readMatrix reads a form's /Matrix, the identity when it is absent or not
// six finite numbers (ISO 32000-1, 8.10.2, Table 95).
func readMatrix(ctx context.Context, doc *document.Document, obj ast.PdfObject) geom.Matrix {
	if obj == nil {
		return geom.Identity
	}
	arr, err := document.ResolveArray(ctx, doc, obj)
	if err != nil || len(arr) != 6 {
		doc.Logger.Debug("malformed form /Matrix, using the identity", slog.Any("err", err), slog.Int("len", len(arr)))
		return geom.Identity
	}
	var m geom.Matrix
	for i, e := range arr {
		r, err := document.ResolveObject(ctx, doc, e)
		if err != nil {
			doc.Logger.Debug("unreadable form /Matrix element, using the identity", slog.Any("err", err))
			return geom.Identity
		}
		f, ok := document.Number(r)
		if !ok || math.IsNaN(f) || math.IsInf(f, 0) {
			return geom.Identity
		}
		m[i] = f
	}
	return m
}

// algorithm81 returns the matrix A of ISO 32000-1, 12.5.5, Algorithm 8.1: the
// form's /BBox, transformed by its /Matrix, gives a bounding box, and A scales
// and translates that box's corners onto the annotation's /Rect.
//
// A box that collapses to a line or a point has no scale to map. PDF.js then
// translates the form to the rectangle's lower-left corner, unscaled, which a
// real-world producer's /BBox [0 0 0 0] needed; that leniency is followed.
func algorithm81(bbox geom.Rect, formMatrix geom.Matrix, rect geom.Rect) geom.Matrix {
	corners := [4]geom.Point{
		bbox.Min, {X: bbox.Max.X, Y: bbox.Min.Y}, bbox.Max, {X: bbox.Min.X, Y: bbox.Max.Y},
	}
	first := formMatrix.Apply(corners[0])
	box := geom.Rect{Min: first, Max: first}
	for _, c := range corners[1:] {
		p := formMatrix.Apply(c)
		box.Min.X, box.Min.Y = min(box.Min.X, p.X), min(box.Min.Y, p.Y)
		box.Max.X, box.Max.Y = max(box.Max.X, p.X), max(box.Max.Y, p.Y)
	}
	w, h := box.Max.X-box.Min.X, box.Max.Y-box.Min.Y
	if w == 0 || h == 0 || math.IsNaN(w) || math.IsNaN(h) || math.IsInf(w, 0) || math.IsInf(h, 0) {
		return geom.Matrix{1, 0, 0, 1, rect.Min.X, rect.Min.Y}
	}
	sx := (rect.Max.X - rect.Min.X) / w
	sy := (rect.Max.Y - rect.Min.Y) / h
	return geom.Matrix{sx, 0, 0, sy, rect.Min.X - box.Min.X*sx, rect.Min.Y - box.Min.Y*sy}
}

// Placement returns the matrix A of ISO 32000-1, 12.5.5, Algorithm 8.1 that
// places form, the dictionary of an annotation's appearance stream, on the
// annotation's rectangle rect, as [Collect] places it in [Appearance.A]. A
// caller that draws the appearance into page content applies A with cm before
// invoking the form with Do, which applies the form's own /Matrix. It returns
// false when rect or the form's /BBox is not a rectangle of four finite
// numbers: [Collect] draws no such appearance.
func Placement(ctx context.Context, doc *document.Document, rect ast.PdfObject, form ast.PdfDict) (geom.Matrix, bool) {
	r, ok := readRect(ctx, doc, rect)
	if !ok {
		return geom.Matrix{}, false
	}
	bbox, ok := readRect(ctx, doc, form["BBox"])
	if !ok {
		return geom.Matrix{}, false
	}
	return algorithm81(bbox, readMatrix(ctx, doc, form["Matrix"]), r), true
}

// refOf returns obj's reference, zero when obj is direct.
func refOf(obj ast.PdfObject) ast.PdfIndirectRef {
	ref, _ := obj.(ast.PdfIndirectRef) // a direct object has no reference
	return ref
}
