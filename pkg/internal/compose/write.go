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
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/xmp"
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
//     [Document.Metadata] (no dates), or its XMP metadata stream in a PDF
//     2.0 file, and /Lang when Metadata.Lang is set.
//   - With [Document.Structure], a structure tree, marked content with
//     MCIDs, a parent tree and /MarkInfo << /Marked true >>; content with no
//     parent is marked /Artifact.
//
// No output intent or PDF/A identification is written, nor an XMP packet
// before PDF 2.0: those belong to Writer.SetMetadata and the remediation
// stages.
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
//     A header cell has no Scope attribute there (Table 349).
//   - Before PDF 2.0, a list numbering PDF 2.0 added is written as the
//     nearest ISO 32000-1 value (see [ListNumbering]).
//   - PDF 2.0 writes each destination that names a structure element as a
//     structure destination (ISO 32000-2, 12.3.2.3), carries the document
//     information in an XMP metadata stream instead of the Info dictionary
//     it deprecates (14.3.3), and writes a structure type PDF 2.0 removed
//     as its replacement (14.8.6), every standard element naming the PDF
//     2.0 namespace.
//
// At every version, a role that is not a standard structure type of the
// version is mapped by /RoleMap to one that is.
//
// # Limitations
//
//   - A group's opacity is a constant alpha, not a transparency group:
//     overlapping children blend one by one (see [Group]).
//   - An element's own content precedes its child elements in its /K: a
//     producer that interleaves the two gives the content an element of
//     its own.
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
	s, err := newStream(ctx, opts, doc.Structure != nil, true)
	if err != nil {
		return err
	}
	defer s.Abort()
	for _, page := range doc.Pages {
		if err := s.Page(ctx, page); err != nil {
			return err
		}
	}
	tail := *doc
	tail.Pages = nil
	return s.Close(ctx, &tail, dst)
}

// docWriter holds the state of one document being written. It is used by
// one goroutine.
//
// The objects it makes are numbered in two spaces until the file is
// written: the page objects, the page tree's root, the StructTreeRoot and
// the structure elements, whose count is known only at the end, take the
// placeholder numbers of [pageRef], [pagesRef], [structRootRef] and
// [elemRef] and live in reserved; every other object is numbered from 1 by
// alloc as it is made. [docWriter.final] then gives the first group the
// numbers 1 to R and moves the second past R, so the file numbers its
// objects as one pass over a whole document would: the page tree, the
// pages, the structure tree, then everything else in the order it was
// made.
type docWriter struct {
	// version is the version the file is written for.
	version Version
	alloc   *writer.ObjAllocator
	logger  *slog.Logger
	// mem is the conversion's memory budget the streams it keeps in
	// memory, not the ones it spools, are charged to.
	mem *limits.Budget

	// reserved holds the objects at placeholder numbers.
	reserved map[int]ast.PdfObject
	// heights holds the height of every page written, in order: its
	// length is the page count, and a destination reads its page's height.
	heights []float64
	// links are the links of the pages written, whose annotations are
	// finished at the end, once every page they may go to is known.
	links []pendingLink

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

// pendingLink is a link of a page written whose annotation is finished at
// the end: its destination's page and element are resolved then.
type pendingLink struct {
	// page is the 0-based index of the page that holds the link.
	page int
	link Link
	// ref is the annotation's object, reserved when the page was written.
	ref ast.PdfIndirectRef
	// annot is the link's index among the annotations of the structure
	// tree, -1 for none.
	annot int
}

// The placeholder numbers of the objects whose final number depends on
// the page count and the size of the structure tree (see [docWriter]).
var (
	pagesRef      = ast.PdfIndirectRef{ObjectNum: -1}
	structRootRef = ast.PdfIndirectRef{ObjectNum: -2}
)

// pageRef returns the placeholder of the page object of the page at index
// i.
func pageRef(i int) ast.PdfIndirectRef {
	return ast.PdfIndirectRef{ObjectNum: -(3 + 2*i)}
}

// elemRef returns the placeholder of the structure element at index k in
// preorder.
func elemRef(k int) ast.PdfIndirectRef {
	return ast.PdfIndirectRef{ObjectNum: -(4 + 2*k)}
}

// newDocWriter returns the writer of one document of version, tagged when
// it will carry a structure tree.
func newDocWriter(ctx context.Context, opts Options, version Version, tagged bool) *docWriter {
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	held := opts.Spool
	if held == 0 {
		held = defaultSpool
	}
	w := &docWriter{
		version:  version,
		alloc:    writer.NewObjAllocator(0),
		logger:   logger,
		mem:      limits.BudgetFrom(ctx),
		reserved: make(map[int]ast.PdfObject),
		fonts:    make(map[string]*fontEntry),
		images:   make(map[*ImageSource]ast.PdfIndirectRef),
		shadings: make(map[string]shadingRefs),
		id:       sha256.New(),
		spool:    writer.NewSpool(opts.TempDir),
		hold:     held,
		mark:     1,
		shared:   make(map[int]bool),
	}
	if tagged {
		w.st = newStructState()
	}
	return w
}

// spill considers the objects queued since the last call: a stream the
// writer made stays in memory while the bytes held stay within w.hold, and
// goes to the spool file past it. Streams only ever move to the file, so
// the memory they hold is bounded by w.hold and one page's worth.
func (w *docWriter) spill(ctx context.Context) error {
	next := w.alloc.NextObjNum()
	pending := w.alloc.Pending()
	for num := w.mark; num < next; num++ {
		s, ok := pending[num].(ast.PdfStream)
		if !ok || w.shared[num] {
			continue
		}
		// Every stream queued since the mark is one the writer built in
		// memory, so Bytes reads nothing.
		data, err := s.Bytes(ctx)
		if err != nil {
			return fmt.Errorf("writing PDF: %w", err)
		}
		if w.hold < 0 || w.held+int64(len(data)) <= w.hold {
			// A stream kept in memory is held until the file is written.
			if err := w.mem.ChargeUnits(int64(len(data)), 1, "the output streams"); err != nil {
				return fmt.Errorf("writing PDF: %w", err)
			}
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

// finish queues the objects of the document's end, from tail's
// metadata, outline and structure tree, and the annotations of the links
// of the pages written, whose destinations dest maps to the ones written
// (see [Stream.Close]), and returns the trailer. Every page has been
// written, and tail has passed the checks of the end.
func (w *docWriter) finish(ctx context.Context, tail *Document, producer string, dest func(Dest) Dest) (ast.PdfDict, error) {
	n := len(w.heights)
	if w.st != nil {
		w.st.number(tail.Structure)
	}
	for _, pl := range w.links {
		w.alloc.AllocAt(pl.ref.ObjectNum, w.linkDict(pl, n, dest))
	}
	// ISO 32000-1, 7.7.3.2: a flat tree of one Pages node is a valid page
	// tree.
	kids := make(ast.PdfArray, n)
	for i := range kids {
		kids[i] = pageRef(i)
	}
	w.reserved[pagesRef.ObjectNum] = ast.PdfDict{
		"Type":  ast.PdfName("Pages"),
		"Kids":  kids,
		"Count": ast.PdfInt(n),
	}

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
		"Pages": pagesRef,
	}
	if len(tail.Outline) > 0 {
		catalog["Outlines"] = w.outlines(tail.Outline)
	}
	if tail.Metadata.Lang != "" {
		catalog["Lang"] = textString(tail.Metadata.Lang)
	}
	if w.st != nil {
		if err := w.st.write(ctx, w, w.version); err != nil {
			return nil, err
		}
		catalog["StructTreeRoot"] = structRootRef
		// ISO 32000-1, 14.7.1, Table 321: /Marked true declares a tagged
		// PDF.
		catalog["MarkInfo"] = ast.PdfDict{"Marked": ast.PdfBool(true)}
		// ISO 14289-1, 7.1: a tagged document's viewer shows its title,
		// not its file name.
		catalog["ViewerPreferences"] = ast.PdfDict{"DisplayDocTitle": ast.PdfBool(true)}
	}
	info := w.info(tail.Metadata, producer)
	var infoRef ast.PdfIndirectRef
	if w.version == Version20 {
		// ISO 32000-2, 14.3.3 deprecates the Info dictionary but for its
		// dates, which the model does not carry: the document information
		// goes to the catalog's metadata stream (14.3.2), uncompressed so
		// a reader that does not decode the file still finds it.
		catalog["Metadata"] = w.alloc.Alloc(ast.NewStream(ast.PdfDict{
			"Type":    ast.PdfName("Metadata"),
			"Subtype": ast.PdfName("XML"),
		}, xmp.Packet(xmpInfo(tail.Metadata, producer))))
	} else {
		infoRef = w.alloc.Alloc(info)
	}
	catalogRef := w.alloc.Alloc(catalog)

	// A file identifier is optional in ISO 32000-1, 14.4 but required by
	// PDF/A (ISO 19005-2, 6.1.3), and output bytes must depend on the input
	// alone, so it is derived from the content rather than drawn at random:
	// SHA-256 over every page's size and content stream and the Info
	// values, cut to 16 bytes. Both entries are equal: the file is an
	// original, not an update (14.4).
	sum := w.id.Sum(nil)[:16]
	id := ast.PdfString(sum)
	trailer := ast.PdfDict{
		"Root": catalogRef,
		"ID":   ast.PdfArray{id, id},
	}
	if infoRef.ObjectNum != 0 {
		trailer["Info"] = infoRef
	}
	return trailer, nil
}

// final returns the allocator the file is written from and the trailer,
// every placeholder and every provisional number replaced by the object's
// number in the file (see [docWriter]). The objects keep their values:
// only the references they hold change.
func (w *docWriter) final(trailer ast.PdfDict) (*writer.ObjAllocator, ast.PdfDict) {
	n := len(w.heights)
	reservedCount := 1 + n
	if w.st != nil {
		reservedCount += 1 + len(w.st.order)
	}
	number := func(num int) int {
		switch {
		case num > 0:
			return num + reservedCount
		case num == pagesRef.ObjectNum:
			return 1
		case num == structRootRef.ObjectNum:
			return n + 2
		}
		if k := -num - 3; k%2 == 0 {
			return 2 + k/2
		}
		return n + 3 + (-num-4)/2
	}
	// Each object leaves the writer's maps as its renumbered copy is made,
	// so the two sets are never held whole together.
	out := writer.NewObjAllocator(0)
	for _, objs := range []map[int]ast.PdfObject{w.reserved, w.alloc.Pending()} {
		for num, obj := range objs {
			out.AllocAt(number(num), relink(obj, number))
			delete(objs, num)
		}
	}
	t, _ := relink(trailer, number).(ast.PdfDict) //nolint:errcheck // relink returns a dictionary for a dictionary
	return out, t
}

// relink returns obj with every indirect reference renumbered by number.
// A value that holds no reference is returned as it is; a stream keeps its
// bytes, its dictionary renumbered.
func relink(obj ast.PdfObject, number func(int) int) ast.PdfObject {
	switch v := obj.(type) {
	case ast.PdfIndirectRef:
		return ast.PdfIndirectRef{ObjectNum: number(v.ObjectNum), GenNum: v.GenNum}
	case ast.PdfArray:
		if !holdsRef(v) {
			return v
		}
		out := make(ast.PdfArray, len(v))
		for i, e := range v {
			out[i] = relink(e, number)
		}
		return out
	case ast.PdfDict:
		if !holdsRef(v) {
			return v
		}
		out := make(ast.PdfDict, len(v))
		for k, e := range v {
			out[k] = relink(e, number)
		}
		return out
	case ast.PdfStream:
		if holdsRef(v.Dict) {
			v.Dict, _ = relink(v.Dict, number).(ast.PdfDict) //nolint:errcheck // relink returns a dictionary for a dictionary
		}
		return v
	}
	return obj
}

// holdsRef reports whether obj holds an indirect reference.
func holdsRef(obj ast.PdfObject) bool {
	switch v := obj.(type) {
	case ast.PdfIndirectRef:
		return true
	case ast.PdfArray:
		for _, e := range v {
			if holdsRef(e) {
				return true
			}
		}
	case ast.PdfDict:
		for _, e := range v {
			if holdsRef(e) {
				return true
			}
		}
	case ast.PdfStream:
		return holdsRef(v.Dict)
	}
	return false
}

// xmpInfo returns the document information the metadata stream of a PDF
// 2.0 file carries.
func xmpInfo(m Metadata, producer string) xmp.Info {
	return xmp.Info{
		Title: m.Title, Author: m.Author, Subject: m.Subject, Keywords: m.Keywords,
		Creator: m.Creator, Producer: producer, Lang: m.Lang,
	}
}

// info returns the Info dictionary (ISO 32000-1, 14.3.3, Table 317) and
// hashes its values into the file identifier. Dates are left out so the
// output does not depend on when it was written.
func (w *docWriter) info(m Metadata, producer string) ast.PdfDict {
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

// page writes the next page with links in place of its own: its content
// stream, the objects of its annotations and the page object (ISO
// 32000-1, 7.7.3.3, Table 30). The annotations are finished at the end.
func (w *docWriter) page(ctx context.Context, page *Page, links []Link) error {
	i := len(w.heights)
	if w.st != nil {
		w.st.addPage()
	}
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
		"Parent":    pagesRef,
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
	if len(links) > 0 {
		annots := make(ast.PdfArray, len(links))
		for j, link := range links {
			ref := w.alloc.Alloc(ast.PdfNull{})
			pl := pendingLink{page: i, link: link, ref: ref, annot: -1}
			if w.st != nil && link.Parent != nil {
				var to *StructElem
				if link.Dest != nil {
					to = link.Dest.Elem
				}
				pl.annot = w.st.addAnnot(i, ref, link.Parent, to)
			}
			w.links = append(w.links, pl)
			annots[j] = ref
		}
		dict["Annots"] = annots
		if w.st != nil && w.version != Version14 {
			// ISO 14289-1, 7.18.3: a page with annotations in a tagged
			// document orders them by the structure tree. /Tabs is a PDF
			// 1.5 entry (ISO 32000-1, Table 30).
			dict["Tabs"] = ast.PdfName("S")
		}
	}
	w.reserved[pageRef(i).ObjectNum] = dict
	w.heights = append(w.heights, page.Height)
	return nil
}

// linkDict returns the Link annotation of pl (ISO 32000-1, 12.5.6.5, Table
// 173) in a file of n pages, its destination resolved by dest: no border,
// the print flag PDF/A asks for (ISO 19005-2, 6.3.2), and a URI action
// (12.6.4.7) or an explicit destination. Its /Rect is the model rectangle
// in PDF coordinates, y flipped. A link with a parent element joins the
// structure tree through an object reference and /StructParent
// (14.7.4.4). Its /Contents is the link's description, else its element's
// Alt, else its target (ISO 14289-1, 7.18.5).
func (w *docWriter) linkDict(pl pendingLink, n int, dest func(Dest) Dest) ast.PdfDict {
	l := pl.link
	r := l.Rect
	height := w.heights[pl.page]
	x0, x1 := math.Min(r.X, r.X+r.Width), math.Max(r.X, r.X+r.Width)
	y0, y1 := math.Min(r.Y, r.Y+r.Height), math.Max(r.Y, r.Y+r.Height)
	dict := ast.PdfDict{
		"Type":    ast.PdfName("Annot"),
		"Subtype": ast.PdfName("Link"),
		"Rect":    ast.PdfArray{pdfReal(x0), pdfReal(height - y1), pdfReal(x1), pdfReal(height - y0)},
		"Border":  ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(0)},
		"F":       ast.PdfInt(4),
	}
	var d Dest
	if l.Dest != nil {
		d = dest(*l.Dest)
		key, target := w.target(d)
		dict[key] = target
	} else {
		dict["A"] = ast.PdfDict{
			"Type": ast.PdfName("Action"),
			"S":    ast.PdfName("URI"),
			"URI":  uriString(l.URI),
		}
	}
	if pl.annot >= 0 {
		dict["StructParent"] = ast.PdfInt(n + pl.annot)
	}
	// ISO 14289-1, 7.18.5 and ISO 14289-2, 8.9.4.4: every link carries a
	// description. With none from the model, the target stands in: the
	// URI, or the page an internal link goes to.
	switch {
	case l.Contents != "":
		dict["Contents"] = textString(l.Contents)
	case l.Parent != nil && l.Parent.Alt != "":
		dict["Contents"] = textString(l.Parent.Alt)
	case l.URI != "":
		dict["Contents"] = textString(l.URI)
	default:
		dict["Contents"] = textString(fmt.Sprintf("Page %d", d.Page))
	}
	return dict
}
