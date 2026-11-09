// Package pdfgen builds the litho/pdf corpus fixtures: a small PDF object
// model and a writer that reproduces, byte for byte, what pikepdf 9.11 over
// qpdf 12.2 writes for the corpus builders' save options (deterministic /ID,
// uncompressed streams, stream decode level none).
//
// The package is independent of litho's own code on purpose: a fixture that
// tests litho must not be produced by the code under test. It uses the
// standard library only.
//
// # Model
//
// Objects mirror pikepdf's: [Name], [String], [Int], [Real], [Bool], [Null]
// are values; [*Array], [*Dict] and [*Stream] are shared handles, so mutating
// one after placing it in a container is seen everywhere it is placed.
// [Doc.Indirect] and [Doc.MakeIndirect] make an object indirect in place, the
// way pikepdf's make_indirect does, and [Doc.Stream] creates a stream, which
// is always indirect.
//
// # Output order
//
// Output never depends on insertion order: like qpdf, the writer sorts
// dictionary keys bytewise, drops null dictionary values, and numbers
// indirect objects in the order a breadth-first walk from the trailer meets
// them (/Root first). Original object numbers matter only when object streams
// are generated, where qpdf orders each stream's members by them.
//
// Not safe for concurrent use: build one Doc per goroutine.
package pdfgen

// Doc is a PDF under construction, the counterpart of pikepdf.new(): a
// catalog (object 1) whose /Pages (object 2) is an empty page tree, and a
// trailer holding /Root and /Size.
type Doc struct {
	// Root is the document catalog.
	Root *Dict
	// Pages is the root of the page tree.
	Pages *Dict
	// Trailer is the trailer dictionary. The writer replaces /Size and
	// /ID and drops /Encrypt, /Prev and the cross-reference stream keys.
	Trailer *Dict

	lastID int

	// pushedInherited is qpdf's pushed_inherited_attributes_to_pages: set
	// once page tree attributes have been pushed down to the pages, which
	// qpdf does on the first page insertion.
	pushedInherited bool
}

// New returns an empty document, as pikepdf.new() does.
func New() *Doc {
	d := &Doc{}
	d.Pages = D("Type", N("Pages"), "Kids", A(), "Count", 0)
	d.Root = D("Type", N("Catalog"), "Pages", d.Pages)
	d.Root.ind = &indirect{id: 1}
	d.Pages.ind = &indirect{id: 2}
	d.lastID = 2
	d.Trailer = D("Root", d.Root, "Size", 3)
	return d
}

func (d *Doc) next() int {
	d.lastID++
	return d.lastID
}

// Indirect makes dict indirect in place and returns it. Calling it on an
// already indirect dictionary gives it a new source object number, as qpdf
// does.
func (d *Doc) Indirect(dict *Dict) *Dict {
	d.MakeIndirect(dict)
	return dict
}

// MakeIndirect makes o indirect, like pikepdf's make_indirect. A dictionary,
// array or stream becomes indirect in place and is returned as is; any other
// value is wrapped in a new [*Ref].
func (d *Doc) MakeIndirect(o Object) Object {
	switch x := o.(type) {
	case *Dict:
		x.relabel(d)
		return x
	case *Array:
		if x.ind == nil {
			x.ind = &indirect{}
		}
		x.ind.id = d.next()
		return x
	case *Stream:
		x.relabel(d)
		return x
	case *Ref:
		x.ind.id = d.next()
		return x
	default:
		return &Ref{Obj: o, ind: &indirect{id: d.next()}}
	}
}

func (x *Dict) relabel(d *Doc) {
	if x.ind == nil {
		x.ind = &indirect{}
	}
	x.ind.id = d.next()
}

// Stream returns a new indirect stream holding data, with dictionary
// entries from kv as in [D]. data is not copied.
func (d *Doc) Stream(data []byte, kv ...any) *Stream {
	s := &Stream{Dict: *D(kv...), Data: data}
	s.ind = &indirect{id: d.next()}
	return s
}

// AddPage appends page to the page tree, as pikepdf's pdf.pages.append does:
// it makes page indirect if it is not, sets its /Parent, appends it to /Kids
// and updates /Count. It returns page. The first call also moves the
// inheritable attributes (/MediaBox, /CropBox, /Resources, /Rotate) of the
// page tree nodes down to the pages already in the tree, as qpdf does when
// it first edits the page list: set on /Pages before the first AddPage,
// they reach no page.
func (d *Doc) AddPage(page *Dict) *Dict {
	if !d.pushedInherited {
		// qpdf's addPage flattens the page tree first, which pushes the
		// inheritable attributes of /Pages down to the existing pages.
		d.pushInheritedAttributes()
	}
	if page.ind == nil {
		d.Indirect(page)
	}
	page.Set("Parent", d.Pages)
	kids, ok := d.Pages.Get("Kids").(*Array)
	if !ok {
		kids = A()
		d.Pages.Set("Kids", kids)
	}
	kids.Items = append(kids.Items, page)
	d.Pages.Set("Count", len(kids.Items))
	return page
}

// Page returns the i-th page of a flat page tree, as pikepdf's pdf.pages[i].
func (d *Doc) Page(i int) *Dict {
	kids, ok := d.Pages.Get("Kids").(*Array)
	if !ok || i < 0 || i >= len(kids.Items) {
		return nil
	}
	p, _ := kids.Items[i].(*Dict)
	return p
}

// AddBlankPage is pikepdf's add_blank_page: a page with an empty content
// stream and empty resources, appended to the page tree. width and height
// are the page_size pikepdf takes, 612 by 792 by default there.
func (d *Doc) AddBlankPage(width, height any) *Dict {
	page := D(
		"Type", N("Page"),
		"MediaBox", A(0, 0, width, height),
		"Contents", d.Stream([]byte{}),
		"Resources", D(),
	)
	return d.AddPage(d.Indirect(page))
}
