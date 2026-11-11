package compose

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"hash"
	"io"
	"log/slog"
	"math"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// Options tunes [Write]. The zero value is ready to use.
type Options struct {
	// Logger receives debug diagnostics, such as a font program that fails
	// to subset and is embedded whole. Nil discards them.
	Logger *slog.Logger
	// Producer is the /Producer of the Info dictionary, "litho" when empty.
	Producer string
	// Version is the PDF version the file is written for, [Version17] when
	// empty. See [Write] for what each version changes.
	Version Version
	// TempDir is the directory of the spool file, the operating system's
	// temporary directory when empty.
	TempDir string
	// Spool is the bytes of the streams Write makes, the page contents and
	// the font programs, it holds in memory: past it, the next ones go to
	// a spool file in TempDir until the file is written. 0 is 4 MiB, a
	// negative value never spools. The output is the same either way.
	Spool int64
}

// defaultSpool is [Options.Spool] when zero: 4 MiB. Memory guard, not
// spec-mandated. Most conversions write less, so they never touch the
// disk, and below it the streams weigh little beside what a conversion
// already holds (its fonts and its laid-out pages, tens of MiB). Past it,
// the file's own streams grow with its pages, about 4 KiB a page of text,
// and a long document holds them all until its last byte is written.
const defaultSpool = 4 << 20

// Write validates doc with [Document.Validate], then writes it to dst as
// one PDF file of the version [Options.Version] names. It returns the
// validation error, unchanged, when doc is refused, an error wrapping
// [ErrInvalidVersion] for a version it does not target, an error wrapping
// [ErrInvalidFont] for a face whose program does not load or a glyph it
// lacks, and an error wrapping ctx.Err() when ctx ends first. Nothing
// reaches dst before the whole file is built, so a failure other than a
// write error on dst leaves dst untouched.
//
// Write reads doc and never mutates it. Its output depends on doc and opts
// alone: two calls with equal input write equal bytes, whether the
// streams went through the spool file or not. The document's Warnings are
// not written; the caller surfaces them.
//
// The streams Write makes past [Options.Spool] bytes are written to a
// temp file in [Options.TempDir] as they are made and read back as the
// file is written to dst; the file is removed before Write returns, on
// every path. A spool file that cannot be created or written fails Write
// before dst receives anything; one that cannot be read back fails it
// while dst is being written. The pictures of the document are never
// spooled: they are written from their [ImageSource] bytes.
//
// # Output
//
//   - A classic cross-reference table, no object streams. Every content
//     stream and lossless image is Flate-compressed.
//   - One content stream per page in paint order, under a flip to the
//     model's top-left origin.
//   - One Type0 font per distinct [Face.Key], subset to the glyphs drawn and
//     shared by every page (see package embed).
//   - One image XObject per distinct [ImageSource] pointer.
//   - One shading dictionary per distinct [Shading] geometry and stops,
//     painted with sh inside the clip of its path; translucent stops add a
//     luminosity soft mask per placement.
//   - Link annotations, the outline, the Info dictionary from
//     [Document.Metadata] (no dates), and /Lang when Metadata.Lang is set.
//   - With [Document.Structure], a structure tree, marked content with
//     MCIDs, a parent tree and /MarkInfo << /Marked true >>; content with no
//     parent is marked /Artifact.
//
// No XMP packet, output intent or PDF/A identification is written: those
// belong to Writer.SetMetadata and the remediation stages.
//
// # Versions
//
// The header declares the version, and the file holds no construct a later
// version defines: what the model asks for that the version lacks is
// written as the nearest thing the version has, so a reader of that
// version reads the whole file.
//
//   - Before PDF 1.5, a page has no /Tabs entry (ISO 32000-1, Table 30),
//     and a structure type PDF 1.5 added, a table row group among them
//     (Table 337), is mapped by the structure tree's /RoleMap to a type
//     PDF 1.4 defines (PDF Reference 1.4, 9.7.4; ISO 19005-1, 6.8.3.4).
//   - PDF 2.0 writes each destination that names a structure element as a
//     structure destination (ISO 32000-2, 12.3.2.3), carries the document
//     information in an XMP metadata stream instead of the Info dictionary
//     it deprecates (14.3.3), and writes a structure type PDF 2.0 removed
//     as its replacement (14.8.6).
//
// At every version, a role that is not a standard structure type of the
// version is mapped by /RoleMap to one that is.
//
// # Limitations
//
//   - A group's opacity is a constant alpha, not a transparency group:
//     overlapping children blend one by one (see [Group]).
//   - An element's own content precedes its child elements in its /K.
//   - An Adobe-marked CMYK JPEG is not inverted and renders as its negative.
//   - Arrays sized by the document, the page tree's /Kids and the parent
//     tree's /Nums, are flat and pass the 8191-element limit ISO 32000-1,
//     Annex C advises for documents of over 4095 pages.
func Write(ctx context.Context, doc *Document, dst io.Writer, opts Options) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("composing PDF: %w", err)
	}
	if doc == nil {
		return fmt.Errorf("%w: nil document", ErrNoPages)
	}
	if err := doc.Validate(); err != nil {
		return err
	}
	version, err := resolveVersion(opts.Version)
	if err != nil {
		return err
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	producer := opts.Producer
	if producer == "" {
		producer = "litho"
	}

	spool := writer.NewSpool(opts.TempDir)
	defer func() {
		if err := spool.Close(); err != nil {
			// The output is complete or abandoned: a spool file left
			// behind changes no byte of it.
			logger.Debug("compose: removing the spool file failed", slog.Any("error", err))
		}
	}()
	held := opts.Spool
	if held == 0 {
		held = defaultSpool
	}
	w := &docWriter{
		doc:      doc,
		alloc:    writer.NewObjAllocator(0),
		logger:   logger,
		fonts:    make(map[string]*fontEntry),
		images:   make(map[*ImageSource]ast.PdfIndirectRef),
		shadings: make(map[string]shadingRefs),
		id:       sha256.New(),
		spool:    spool,
		hold:     held,
		mark:     1,
		shared:   make(map[int]bool),
	}
	trailer, err := w.write(ctx, producer)
	if err != nil {
		return err
	}
	if err := w.spill(ctx); err != nil {
		return err
	}
	if err := writer.WriteRewrite(ctx, dst, writer.RewriteConfig{
		Version:   string(version),
		Allocator: w.alloc,
		Trailer:   trailer,
	}); err != nil {
		return fmt.Errorf("writing PDF: %w", err)
	}
	return nil
}

// docWriter holds the state of one [Write] call. It is used by one
// goroutine.
type docWriter struct {
	doc    *Document
	alloc  *writer.ObjAllocator
	logger *slog.Logger

	pagesRef ast.PdfIndirectRef
	pageRefs []ast.PdfIndirectRef

	fonts     map[string]*fontEntry
	fontOrder []*fontEntry
	images    map[*ImageSource]ast.PdfIndirectRef
	// shadings maps a [shadingKey] to the objects written for it.
	shadings map[string]shadingRefs
	// st is nil for an untagged document.
	st *structState
	// id hashes what the file /ID is derived from.
	id hash.Hash

	// spool takes the streams queued past hold bytes, negative for never;
	// held counts the bytes of those kept in memory. Objects from mark on
	// have not been considered yet; shared holds the objects whose bytes
	// are the model's own, the pictures, which spooling would copy without
	// freeing anything.
	spool  *writer.Spool
	hold   int64
	held   int64
	mark   int
	shared map[int]bool
}

// spill considers the objects queued since the last call: a stream Write
// made stays in memory while the bytes held stay within w.hold, and goes
// to the spool file past it. Streams only ever move to the file, so the
// memory they hold is bounded by w.hold and one page's worth.
func (w *docWriter) spill(ctx context.Context) error {
	next := w.alloc.NextObjNum()
	pending := w.alloc.Pending()
	for num := w.mark; num < next; num++ {
		s, ok := pending[num].(ast.PdfStream)
		if !ok || w.shared[num] {
			continue
		}
		// Every stream queued since the mark is one Write built in
		// memory, so Bytes reads nothing.
		data, err := s.Bytes(ctx)
		if err != nil {
			return fmt.Errorf("writing PDF: %w", err)
		}
		if w.hold < 0 || w.held+int64(len(data)) <= w.hold {
			w.held += int64(len(data))
			continue
		}
		spooled, err := w.spool.Stream(s.Dict, data)
		if err != nil {
			return fmt.Errorf("writing PDF: %w", err)
		}
		w.alloc.AllocAt(num, spooled)
		// Past the threshold every later stream spools: held stays put.
		w.held = w.hold
	}
	w.mark = next
	return nil
}

// write queues every object of the document and returns the trailer.
func (w *docWriter) write(ctx context.Context, producer string) (ast.PdfDict, error) {
	doc := w.doc
	// Pages are reserved first so a link or an outline entry can name a
	// page before it is written.
	w.pagesRef = w.alloc.Alloc(ast.PdfNull{})
	w.pageRefs = make([]ast.PdfIndirectRef, len(doc.Pages))
	for i := range doc.Pages {
		w.pageRefs[i] = w.alloc.Alloc(ast.PdfNull{})
	}
	if doc.Structure != nil {
		w.st = newStructState(w, doc.Structure)
	}

	kids := make(ast.PdfArray, len(doc.Pages))
	for i, page := range doc.Pages {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("writing page %d: %w", i+1, err)
		}
		if err := w.page(ctx, i, page); err != nil {
			return nil, err
		}
		if err := w.spill(ctx); err != nil {
			return nil, err
		}
		kids[i] = w.pageRefs[i]
	}
	// ISO 32000-1, 7.7.3.2: a flat tree of one Pages node is a valid page
	// tree.
	w.alloc.AllocAt(w.pagesRef.ObjectNum, ast.PdfDict{
		"Type":  ast.PdfName("Pages"),
		"Kids":  kids,
		"Count": ast.PdfInt(len(doc.Pages)),
	})

	for _, fe := range w.fontOrder {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("writing fonts: %w", err)
		}
		fe.subset.Write(ctx, w.alloc, w.logger, fe.ref)
		if err := w.spill(ctx); err != nil {
			return nil, err
		}
	}

	catalog := ast.PdfDict{
		"Type":  ast.PdfName("Catalog"),
		"Pages": w.pagesRef,
	}
	if len(doc.Outline) > 0 {
		catalog["Outlines"] = w.outlines(doc.Outline)
	}
	if doc.Metadata.Lang != "" {
		catalog["Lang"] = textString(doc.Metadata.Lang)
	}
	if w.st != nil {
		if err := w.st.write(ctx, w); err != nil {
			return nil, err
		}
		catalog["StructTreeRoot"] = w.st.rootRef
		// ISO 32000-1, 14.7.1, Table 321: /Marked true declares a tagged
		// PDF.
		catalog["MarkInfo"] = ast.PdfDict{"Marked": ast.PdfBool(true)}
		// ISO 14289-1, 7.1: a tagged document's viewer shows its title,
		// not its file name.
		catalog["ViewerPreferences"] = ast.PdfDict{"DisplayDocTitle": ast.PdfBool(true)}
	}
	catalogRef := w.alloc.Alloc(catalog)

	info := w.info(producer)
	infoRef := w.alloc.Alloc(info)

	// A file identifier is optional in ISO 32000-1, 14.4 but required by
	// PDF/A (ISO 19005-2, 6.1.3), and output bytes must depend on the input
	// alone, so it is derived from the content rather than drawn at random:
	// SHA-256 over every page's size and content stream and the Info
	// values, cut to 16 bytes. Both entries are equal: the file is an
	// original, not an update (14.4).
	sum := w.id.Sum(nil)[:16]
	id := ast.PdfString(sum)
	return ast.PdfDict{
		"Root": catalogRef,
		"Info": infoRef,
		"ID":   ast.PdfArray{id, id},
	}, nil
}

// info returns the Info dictionary (ISO 32000-1, 14.3.3, Table 317) and
// hashes its values into the file identifier. Dates are left out so the
// output does not depend on when it was written.
func (w *docWriter) info(producer string) ast.PdfDict {
	m := w.doc.Metadata
	info := ast.PdfDict{}
	for _, e := range []struct {
		key   ast.PdfName
		value string
	}{
		{"Title", m.Title},
		{"Author", m.Author},
		{"Subject", m.Subject},
		{"Keywords", m.Keywords},
		{"Creator", m.Creator},
		{"Producer", producer},
	} {
		w.hashBytes([]byte(e.value))
		if e.value != "" {
			info[e.key] = textString(e.value)
		}
	}
	return info
}

// hashBytes adds b to the file identifier's hash behind its length, so two
// sequences of values never hash alike by shifting bytes between values.
func (w *docWriter) hashBytes(b []byte) {
	var n [8]byte
	binary.BigEndian.PutUint64(n[:], uint64(len(b)))
	w.id.Write(n[:]) // hash.Hash writes never fail
	w.id.Write(b)
}

// page writes page i: its content stream, its annotations and the page
// object (ISO 32000-1, 7.7.3.3, Table 30).
func (w *docWriter) page(ctx context.Context, i int, page *Page) error {
	pw := newPageWriter(w, i, page)
	if err := pw.content(ctx, page.Content); err != nil {
		return err
	}
	content := pw.buf.Bytes()
	var size [16]byte
	binary.BigEndian.PutUint64(size[:8], math.Float64bits(page.Width))
	binary.BigEndian.PutUint64(size[8:], math.Float64bits(page.Height))
	w.hashBytes(size[:])
	for _, b := range []*Rect{page.TrimBox, page.BleedBox} {
		if b != nil {
			var box [32]byte
			for k, v := range []float64{b.X, b.Y, b.Width, b.Height} {
				binary.BigEndian.PutUint64(box[8*k:], math.Float64bits(v))
			}
			w.hashBytes(box[:])
		}
	}
	w.hashBytes(content)

	stream, err := flateStream(ast.PdfDict{}, content)
	if err != nil {
		return fmt.Errorf("writing page %d: %w", i+1, err)
	}
	dict := ast.PdfDict{
		"Type":      ast.PdfName("Page"),
		"Parent":    w.pagesRef,
		"MediaBox":  ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), pdfReal(page.Width), pdfReal(page.Height)},
		"Resources": pw.resources(),
		"Contents":  w.alloc.Alloc(stream),
	}
	for name, b := range map[ast.PdfName]*Rect{"TrimBox": page.TrimBox, "BleedBox": page.BleedBox} {
		if b != nil {
			// The page's coordinates run down from its top; PDF's up from
			// its bottom (ISO 32000-1, 14.11.2: [llx lly urx ury]).
			dict[name] = ast.PdfArray{pdfReal(b.X), pdfReal(page.Height - b.Y - b.Height), pdfReal(b.X + b.Width), pdfReal(page.Height - b.Y)}
		}
	}
	if w.st != nil {
		dict["StructParents"] = ast.PdfInt(i)
	}
	if len(page.Links) > 0 {
		annots := make(ast.PdfArray, len(page.Links))
		for j, link := range page.Links {
			annots[j] = w.link(i, page, link)
		}
		dict["Annots"] = annots
		if w.st != nil {
			// ISO 14289-1, 7.18.3: a page with annotations in a tagged
			// document orders them by the structure tree.
			dict["Tabs"] = ast.PdfName("S")
		}
	}
	w.alloc.AllocAt(w.pageRefs[i].ObjectNum, dict)
	return nil
}

// link writes a Link annotation (ISO 32000-1, 12.5.6.5, Table 173) with no
// border, the print flag PDF/A asks for (ISO 19005-2, 6.3.2), and a URI
// action (12.6.4.7) or an explicit destination. Its /Rect is the model
// rectangle in PDF coordinates, y flipped. A link with a parent element
// joins the structure tree through an object reference and /StructParent
// (14.7.4.4), and carries the element's Alt as /Contents (ISO 14289-1,
// 7.18.1).
func (w *docWriter) link(pageIndex int, page *Page, l Link) ast.PdfIndirectRef {
	r := l.Rect
	x0, x1 := math.Min(r.X, r.X+r.Width), math.Max(r.X, r.X+r.Width)
	y0, y1 := math.Min(r.Y, r.Y+r.Height), math.Max(r.Y, r.Y+r.Height)
	dict := ast.PdfDict{
		"Type":    ast.PdfName("Annot"),
		"Subtype": ast.PdfName("Link"),
		"Rect":    ast.PdfArray{pdfReal(x0), pdfReal(page.Height - y1), pdfReal(x1), pdfReal(page.Height - y0)},
		"Border":  ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(0)},
		"F":       ast.PdfInt(4),
	}
	if l.Dest != nil {
		dict["Dest"] = w.dest(*l.Dest)
	} else {
		dict["A"] = ast.PdfDict{
			"Type": ast.PdfName("Action"),
			"S":    ast.PdfName("URI"),
			"URI":  uriString(l.URI),
		}
	}
	ref := w.alloc.Alloc(ast.PdfNull{})
	if w.st != nil && l.Parent != nil {
		dict["StructParent"] = ast.PdfInt(w.st.addAnnot(pageIndex, ref, l.Parent))
		if l.Parent.Alt != "" {
			dict["Contents"] = textString(l.Parent.Alt)
		}
	}
	w.alloc.AllocAt(ref.ObjectNum, dict)
	return ref
}
