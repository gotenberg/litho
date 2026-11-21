// Package picture lays an image file out as a [typeset.Document]: one page
// a picture, each the size of its picture at the resolution its file
// states, or the caller's paper with the picture fitted to it.
//
// The pictures are read by package imagefile, which keeps their samples:
// a JPEG or a JPEG 2000 file is written as it is, every other format
// decoded and written losslessly. An SVG file stays vector, drawn by
// package svg as an HTML page's picture is.
package picture

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/imagefile"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// maxPageSide is the largest page side in points: ISO 32000-1, Annex C.2,
// Table C.1 bounds a page at 14,400 units in either dimension. It is the
// implementation limit, not a litho guard.
const maxPageSide = office.MaxPageSide

// minPageSide is the smallest page side in points: Annex C.2, Table C.1
// gives 3 units as the minimum page size.
const minPageSide = 3

// pointsPerInch is the PDF's unit, 1/72 inch (ISO 32000-1, 8.3.2.3).
const pointsPerInch = 72

// Options sets up one conversion.
type Options struct {
	// PageWidth and PageHeight, both set, are the paper every page is: the
	// picture is scaled down to fit it, never up, and centred. Both zero
	// give each page its picture's size.
	PageWidth, PageHeight float64
	// Landscape turns every page that is higher than it is wide, Portrait
	// every page that is wider than it is high; the picture is then
	// fitted to the turned page.
	Landscape, Portrait bool
	// Version is the PDF version written: under PDF 1.5 a JPEG 2000 file
	// is decoded and a 16-bit sample written at 8 bits (see
	// imagefile.Options.PDF14).
	Version typeset.Version
	// DefaultLang is the document's natural language, "" for none.
	DefaultLang string
	// MaxInputBytes caps the source, office.DefaultMaxInputBytes when 0.
	MaxInputBytes int64
	// MaxPages caps the pages, limits.DefaultMaxConvertPages when 0.
	MaxPages int
	// Logger receives diagnostics, nil for none.
	Logger *slog.Logger
	// Sink, when set, takes each page as it is made: a multi-page file
	// holds one decoded picture at a time.
	Sink typeset.PageSink
}

// Convert lays the image file src out, one page a picture (see package
// imagefile for the pictures of a file). The page is the picture's size,
// its pixels at the resolution its file states, else 96 pixels an inch;
// an SVG file's page is its width and height, else its view box, in CSS
// pixels, else 300 by 150 pixels. A page of the picture's size may pass
// the 14,400 points of ISO 32000-1, Annex C.2 (see
// [typeset.FittedPageLimit]); a side past the limit of the version, or
// past 14,400 points on the options' paper, is cut to it and the picture
// scaled down to fit, as a side under 3 points is grown to it with the
// picture centred, each with a warning. The structure tree
// holds a Document element with a Figure a page, described by what the
// format defines as the picture's description.
//
// It returns an error wrapping office.ErrMalformed for a source that is no
// image file of a format litho reads, its message naming what the bytes
// look like when they are a format litho does not read, or whose bytes
// do not read as their format, limits.ErrInputTooLarge for a source past
// MaxInputBytes or a picture past the pixel cap of ctx's budget,
// limits.ErrTooManyPages past MaxPages, the budget's refusal, and ctx's
// error when ctx ends first.
func Convert(ctx context.Context, src io.Reader, fonts *text.Fonts, o Options) (*typeset.Document, error) {
	data, err := office.ReadAll(ctx, src, o.MaxInputBytes)
	if err != nil {
		return nil, err
	}
	maxPages := o.MaxPages
	if maxPages <= 0 {
		maxPages = limits.DefaultMaxConvertPages
	}
	c := &converter{opts: o, doc: &typeset.Document{}, root: &typeset.StructElem{Role: "Document"}}
	c.doc.Structure = c.root
	c.doc.Metadata.Lang = o.DefaultLang
	switch format := imagefile.Sniff(data); format {
	case imagefile.Unknown:
		if name := imagefile.Unsupported(data); name != "" {
			return nil, fmt.Errorf("%w: the source is %s, which litho does not convert", office.ErrMalformed, name)
		}
		return nil, fmt.Errorf("%w: the source is not an image of a format litho converts", office.ErrMalformed)
	case imagefile.SVG:
		if err := c.svg(ctx, data, fonts); err != nil {
			return nil, err
		}
	case imagefile.PNG, imagefile.JPEG, imagefile.GIF, imagefile.WebP, imagefile.BMP, imagefile.TIFF, imagefile.JPEG2000, imagefile.ICO:
		info, err := imagefile.Read(ctx, data, format, imagefile.Options{
			PDF14:       o.Version == typeset.Version14,
			MaxPictures: maxPages,
			Warn:        c.fileWarning,
		}, func(p *imagefile.Picture) error {
			return c.raster(ctx, p)
		})
		if err != nil {
			return nil, convertError(err)
		}
		c.doc.Metadata.Title = info.Title
	}
	if err := c.flush(ctx); err != nil {
		return nil, err
	}
	return c.doc, nil
}

// convertError classifies err, a read's: a file litho does not read or
// whose bytes do not read is malformed, a picture past the pixel cap too
// large; limits and the context stay as they are. The message is err's,
// which names the format and the fault.
func convertError(err error) error {
	switch {
	case errors.Is(err, limits.ErrPictureTooLarge):
		return classified{sentinel: limits.ErrInputTooLarge, err: err}
	case errors.Is(err, imagefile.ErrMalformed), errors.Is(err, imagefile.ErrUnsupported):
		return classified{sentinel: office.ErrMalformed, err: err}
	}
	return err
}

// classified is an error that wraps a sentinel beside its cause, read as
// its cause.
type classified struct {
	sentinel, err error
}

func (e classified) Error() string   { return e.err.Error() }
func (e classified) Unwrap() []error { return []error{e.sentinel, e.err} }

// converter is the state of one conversion. It does not outlive the call.
type converter struct {
	opts Options
	doc  *typeset.Document
	root *typeset.StructElem
	// pages counts the pages made; pending is the page made last, handed
	// to the sink once the next one is made or the conversion ends.
	pages   int
	pending *typeset.Page
}

// fileWarning records what a file states that the output does not keep.
func (c *converter) fileWarning(msg string, omitted bool) {
	effect := warn.Approximated
	code := warn.ContentUnsupported
	if omitted {
		effect, code = warn.Omitted, warn.PictureUnreadable
	}
	c.doc.Warnings = append(c.doc.Warnings, typeset.Warning{Code: code, Effect: effect, Message: msg})
}

// warn records a warning on the page being made.
func (c *converter) warn(code warn.Code, effect warn.Effect, msg string) {
	c.doc.Warnings = append(c.doc.Warnings, typeset.Warning{Code: code, Effect: effect, Message: msg, Page: c.pages})
}

// addPage adds p, holding its figure fig, as the next page: to the
// document, or to the sink once the next page is made.
func (c *converter) addPage(ctx context.Context, p *typeset.Page, fig *typeset.StructElem) error {
	if c.opts.Sink != nil && c.pending != nil {
		if err := c.opts.Sink.Page(ctx, c.pending); err != nil {
			return err
		}
	}
	c.root.Kids = append(c.root.Kids, fig)
	if c.opts.Sink != nil {
		c.pending = p
		return nil
	}
	c.doc.Pages = append(c.doc.Pages, p)
	return nil
}

// flush hands the last page to the sink.
// fitted reports whether a page takes its picture's size rather than the
// options' paper: such a page is [typeset.Page.Fitted].
func (c *converter) fitted() bool {
	return c.opts.PageWidth <= 0 || c.opts.PageHeight <= 0
}

func (c *converter) flush(ctx context.Context) error {
	if c.opts.Sink == nil || c.pending == nil {
		return nil
	}
	p := c.pending
	c.pending = nil
	return c.opts.Sink.Page(ctx, p)
}

// pageFor returns the page a picture of natural size w by h points takes
// and the rectangle the picture is drawn in on it: the options' paper, or
// the picture's size, turned as the options ask, the picture scaled down
// to fit it, never up, and centred (as a slide is fitted to a caller's
// paper). A side past its limit or under 3 points is brought within
// them, with a warning: a paper side's limit is 14,400 points, and that
// of a page of the picture's size, which [converter.fitted] reports, is
// [typeset.FittedPageLimit] of the version.
func (c *converter) pageFor(w, h float64) (float64, float64, typeset.Rect) {
	pw, ph := w, h
	limit := typeset.FittedPageLimit(c.opts.Version)
	if !c.fitted() {
		pw, ph = c.opts.PageWidth, c.opts.PageHeight
		limit = maxPageSide
	}
	if c.opts.Landscape && pw < ph || c.opts.Portrait && pw > ph {
		pw, ph = ph, pw
	}
	if pw > limit || ph > limit {
		c.warn(warn.LimitReached, warn.Approximated, fmt.Sprintf("a page of %.0f by %.0f points is cut to %.0f points a side, the page limit, and the picture scaled down to fit it", pw, ph, limit))
		pw, ph = min(pw, limit), min(ph, limit)
	}
	if pw < minPageSide || ph < minPageSide {
		c.warn(warn.LimitReached, warn.Approximated, fmt.Sprintf("a page of %.2f by %.2f points is grown to %d points a side, the PDF minimum, the picture centred on it", pw, ph, minPageSide))
		pw, ph = max(pw, minPageSide), max(ph, minPageSide)
	}
	s := min(pw/w, ph/h, 1)
	dw, dh := float64(w*s), float64(h*s)
	return pw, ph, typeset.Rect{X: (pw - dw) / 2, Y: (ph - dh) / 2, Width: dw, Height: dh}
}
