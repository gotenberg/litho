package pptx

import (
	"context"
	"errors"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/convert/slides"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// The PresentationML namespaces (ECMA-376 Part 1, 19; the Strict ones
// read as these) and the diagram drawing namespace of MS-ODRAWXML, 2.4.
const (
	nsP   = "http://schemas.openxmlformats.org/presentationml/2006/main"
	nsDSP = "http://schemas.microsoft.com/office/drawing/2008/diagram"
	nsDgm = "http://schemas.openxmlformats.org/drawingml/2006/diagram"
	// nsP14 is PowerPoint 2010's extension namespace ([MS-PPTX] 2.3), the
	// sections' among them.
	nsP14 = "http://schemas.microsoft.com/office/powerpoint/2010/main"
)

// The relationship types the reader follows (ECMA-376 Part 1, 13.3;
// MS-ODRAWXML, 2.1.1 for the diagram drawing).
const (
	relSlide          = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide"
	relSlideLayout    = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout"
	relSlideMaster    = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideMaster"
	relNotesSlide     = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/notesSlide"
	relNotesMaster    = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/notesMaster"
	relHandoutMaster  = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/handoutMaster"
	relTableStyles    = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/tableStyles"
	relComments       = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/comments"
	relDiagramDrawing = "http://schemas.microsoft.com/office/2007/relationships/diagramDrawing"
)

// understood lists the namespaces whose mc:Choice the reader takes
// (ECMA-376 Part 3, 10.2): PresentationML, DrawingML and its picture and
// chart vocabularies; any other Choice, an OLE object's VML one or a
// PowerPoint 2010 extension, takes its Fallback.
var understood = append([]string{nsP, ooxml.NSA, ooxml.NSR, ooxml.NSPic, ooxml.NSChart}, chartExChoices...)

// maxSlides caps the slides of a presentation. Anti-DoS guard, not
// spec-mandated: every slide is a page, and real decks hold a few hundred
// at most.
const maxSlides = 10_000

// maxFirstSlideNumber caps the number of the first slide. Anti-DoS guard,
// not spec-mandated: firstSlideNum is an xsd:int (ECMA-376 Part 1,
// 19.2.1.26), and the cap keeps every later number within an int on any
// platform; PowerPoint takes at most 9999. A number below 1 reads as 1.
const maxFirstSlideNumber = 1_000_000

// maxShapes caps the shapes read from a presentation, every slide, group
// and notes page together. Anti-DoS guard, not spec-mandated: a dense
// slide holds a few hundred shapes.
const maxShapes = 200_000

// maxGroupDepth caps the nesting of shape groups at
// [limits.MaxSourceGroupDepth], which says why: the reader walks groups
// recursively, and each becomes a group of the page.
const maxGroupDepth = limits.MaxSourceGroupDepth

// maxParagraphs caps the paragraphs of one text body, and maxRuns the runs
// of one paragraph. Anti-DoS guards, not spec-mandated: a slide's text
// holds a few dozen paragraphs of a few runs.
const (
	maxParagraphs = 10_000
	maxRuns       = 10_000
)

// maxRunes caps the characters read from a presentation, all text bodies
// together. Anti-DoS guard, not spec-mandated: four million characters is
// some two thousand slides of dense text.
const maxRunes = 4 << 20

// The default slide and notes sizes when presentation.xml sets none
// (ECMA-376 Part 1, 19.2.1.36 sldSz and 19.2.1.22 notesSz are required;
// PowerPoint's 10 by 7.5 inch slide and 7.5 by 10 inch notes page).
const (
	defaultSlideWidth  = 720.0
	defaultSlideHeight = 540.0
)

// errNotPresentationPart reports a part whose root is not the
// PresentationML element its relationship promises.
var errNotPresentationPart = fmt.Errorf("%w: not the expected PresentationML part", office.ErrMalformed)

// errStop ends the reading of a presentation past a cap: the slides read
// so far are kept.
var errStop = errors.New("presentation cap reached")

// reader is one presentation's reading state.
type reader struct {
	ctx   context.Context
	pkg   *ooxml.Package
	fonts *text.Fonts
	// main is the presentation part, defaultText its p:defaultTextStyle.
	main        string
	defaultText *ooxml.Element
	// layers caches the masters, layouts and notes masters by part, and
	// themes the themes.
	layers map[string]*layer
	themes map[string]*theme
	// tableStyles are the presentation's table styles by ID, defTable the
	// default style's ID (ECMA-376 Part 1, 20.1.4.2.27 tblStyleLst).
	tableStyles map[string]*ooxml.Element
	defTable    string
	// shapes and runes count against the caps.
	shapes, runes int
	pictures      map[string]*layout.Picture
	// owner maps a placeholder shape or a text style of a layout, master
	// or notes master to its part, whose relationships its pictures name.
	owner map[*ooxml.Element]string
	// slideAt maps a slide part to its 1-based place in the show,
	// slideCount counts the slides and cur is the slide being read, for
	// the jumps of hyperlinks.
	slideAt         map[string]int
	slideCount, cur int
	// slideW and slideH are the slide size in points.
	slideW, slideH float64
	// shrinkGroupCount counts the shrink groups of the SmartArt
	// diagrams read, the last one's id.
	shrinkGroupCount int
	// notesRead marks the notes page layout read from the first notes
	// slide: notesSlideBox, notesTextBox and notesShapes.
	notesRead                   bool
	notesSlideBox, notesTextBox compose.Rect
	notesShapes                 []*slides.Shape
	warnings                    []compose.Warning
	warned                      map[string]bool
}

// warn records a warning, once per message.
func (r *reader) warn(code, msg string) {
	if r.warned[msg] {
		return
	}
	r.warned[msg] = true
	r.warnings = append(r.warnings, compose.Warning{Code: code, Message: msg})
}

// isMalformed reports a part that does not parse, which the reader skips
// with a warning rather than failing the conversion.
func isMalformed(err error) bool {
	return errors.Is(err, office.ErrMalformed) || ooxml.IsMissing(err)
}

// read reads the presentation pkg holds into the slide model, with the
// warnings of what it left out or approximated, and its handout master
// when handouts asks for it.
func read(ctx context.Context, pkg *ooxml.Package, fonts *text.Fonts, handouts bool) (*slides.Document, []compose.Warning, error) {
	main, err := pkg.MainPart()
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", office.ErrMalformed, err)
	}
	r := &reader{
		ctx: ctx, pkg: pkg, fonts: fonts, main: main,
		layers: map[string]*layer{}, themes: map[string]*theme{},
		pictures: map[string]*layout.Picture{}, warned: map[string]bool{}, owner: map[*ooxml.Element]string{},
	}
	root, err := pkg.ParseXML(ctx, main, understood)
	if err != nil {
		return nil, nil, err
	}
	if !root.Is(nsP, "presentation") {
		return nil, nil, fmt.Errorf("%w: the main part is not a PresentationML presentation", office.ErrMalformed)
	}
	doc := &slides.Document{Width: defaultSlideWidth, Height: defaultSlideHeight}
	if w, h, ok := size(root.Child(nsP, "sldSz")); ok {
		doc.Width, doc.Height = w, h
	}
	if w, h, ok := size(root.Child(nsP, "notesSz")); ok {
		doc.NotesWidth, doc.NotesHeight = w, h
	}
	r.slideW, r.slideH = doc.Width, doc.Height
	r.defaultText = root.Child(nsP, "defaultTextStyle")
	r.readTableStyles()
	cp := pkg.CoreProperties(ctx)
	subject := cp.Subject
	if subject == "" {
		subject = cp.Description
	}
	doc.Meta = compose.Metadata{Title: cp.Title, Author: cp.Creator, Subject: subject, Keywords: cp.Keywords, Lang: cp.Language}
	first := 1
	if n, ok := intAttr(root, "firstSlideNum"); ok {
		first = min(max(n, 1), maxFirstSlideNumber)
	}
	doc.FirstNumber = first
	sections := sectionsOf(root)
	prevSection := -1
	r.slideAt = map[string]int{}
	for _, id := range root.Child(nsP, "sldIdLst").Kids() {
		rid, _ := id.AttrNS(ooxml.NSR, "id")
		if rel, ok := pkg.RelationshipByID(main, rid); ok && id.Is(nsP, "sldId") && !rel.External && rel.Type == relSlide && pkg.Has(rel.Target) && r.slideCount < maxSlides {
			r.slideCount++
			if _, dup := r.slideAt[rel.Target]; !dup {
				r.slideAt[rel.Target] = r.slideCount
			}
		}
	}
	for _, id := range root.Child(nsP, "sldIdLst").Kids() {
		if !id.Is(nsP, "sldId") {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		if len(doc.Slides) >= maxSlides {
			r.warn("too-large", fmt.Sprintf("the presentation holds more than %d slides: the rest are left out", maxSlides))
			break
		}
		rid, _ := id.AttrNS(ooxml.NSR, "id")
		rel, ok := pkg.RelationshipByID(main, rid)
		if !ok || rel.External || rel.Type != relSlide || !pkg.Has(rel.Target) {
			r.warn("malformed-part", fmt.Sprintf("slide %s is missing and left out", rid))
			continue
		}
		s, err := r.slide(len(doc.Slides)+1, rel.Target)
		if errors.Is(err, errStop) {
			break
		}
		if err != nil {
			if isMalformed(err) {
				r.warn("malformed-part", fmt.Sprintf("slide %s does not parse and is left out: %v", rel.Target, err))
				continue
			}
			return nil, nil, err
		}
		s.Bookmark = bookmark(first+len(doc.Slides), s.Title)
		if sec, ok := sections[attr(id, "id")]; ok {
			s.Section, s.SectionStart = sec.name, sec.index != prevSection
			prevSection = sec.index
		} else {
			prevSection = -1
		}
		doc.Slides = append(doc.Slides, s)
	}
	uniqueNames(doc.Slides)
	doc.NotesSlideBox, doc.NotesTextBox, doc.NotesShapes = r.notesSlideBox, r.notesTextBox, r.notesShapes
	if handouts {
		if err := r.handoutMaster(root, doc); err != nil {
			return nil, nil, err
		}
	}
	return doc, r.warnings, nil
}

// bookmark returns the outline entry PowerPoint writes for the slide
// numbered n titled title: "Slide n: title", "Slide n" for a slide without
// a title. n counts hidden slides and starts at the presentation's
// firstSlideNum (ECMA-376 Part 1, 19.2.1.26 presentation). The names are
// unique by their numbers, so PowerPoint numbers no duplicate.
//
// # Corpus evidence
//
//   - litho/pptx: every reference names its entries so, minimal "Slide 1:
//     Hello", layouts-all's blank seventh slide "Slide 7", hidden-slides
//     "Slide 1: Slide 1" and "Slide 3: Slide 3" for the first and third of
//     four with the second hidden, first-slide-number "Slide 10:" to
//     "Slide 12:", whose slide number fields print 10, 11 and 12.
//   - libreoffice/pptx: sd/tdf103347 prints "Slide 1: Hello", "Slide 2:
//     Hello", "Slide 3: Hello" for three slides titled "Hello".
func bookmark(n int, title string) string {
	if title == "" {
		return fmt.Sprintf("Slide %d", n)
	}
	return fmt.Sprintf("Slide %d: %s", n, title)
}

// section is one section of a presentation: its name and its place in the
// section list.
type section struct {
	name  string
	index int
}

// sectionsOf maps every slide ID the presentation's section list names to
// its section (PowerPoint 2010's p14:sectionLst, [MS-PPTX] 2.3.20). A
// section that lists no slide has none to map.
//
// # Corpus evidence
//
//   - litho/pptx: sections-custom-shows prints the entries "Introduction",
//     "Body" and "Wrap-up", each over its slides' entries, and none for
//     its empty section.
//   - libreoffice/pptx: sd/slide-sections nests its seven slides under
//     "Default Section" and "section-demo".
func sectionsOf(root *ooxml.Element) map[string]section {
	var list *ooxml.Element
	for _, ext := range root.Child(nsP, "extLst").Kids() {
		if l := ext.Child(nsP14, "sectionLst"); l != nil {
			list = l
			break
		}
	}
	out := map[string]section{}
	for i, sec := range list.Kids() {
		if !sec.Is(nsP14, "section") {
			continue
		}
		name := attr(sec, "name")
		for _, id := range sec.Child(nsP14, "sldIdLst").Kids() {
			if v := attr(id, "id"); id.Is(nsP14, "sldId") && v != "" {
				if _, dup := out[v]; !dup {
					out[v] = section{name: name, index: i}
				}
			}
		}
	}
	return out
}

// uniqueNames makes the slides' names unique as Impress names the pages of
// an imported presentation, which its bookmarks show: a slide takes its
// title, else "Slide n" by its place in the show, and a name an earlier slide holds gets " (2)",
// " (3)"... appended.
//
// # Corpus evidence
//
//   - libreoffice/pptx: sd/tdf103347 prints the outline "Hello", "Hello (2)", "Hello
//     (3)" for three slides titled "Hello"; sd/slide-sections names its
//     untitled seventh slide "Slide 7".
func uniqueNames(list []*slides.Slide) {
	seen := map[string]int{}
	taken := map[string]bool{}
	for _, s := range list {
		taken[s.Name] = false
	}
	for _, s := range list {
		base := s.Name
		if !taken[base] {
			taken[base] = true
			seen[base] = 1
			continue
		}
		n := seen[base]
		for {
			n++
			name := fmt.Sprintf("%s (%d)", base, n)
			if !taken[name] {
				taken[name], seen[base], s.Name = true, n, name
				break
			}
		}
	}
}

// size reads the cx and cy of an sldSz or notesSz in points, false when
// either is missing or not positive.
func size(e *ooxml.Element) (w, h float64, ok bool) {
	w, okw := ooxml.EMU(attr(e, "cx"))
	h, okh := ooxml.EMU(attr(e, "cy"))
	return w, h, okw && okh && w > 0 && h > 0
}

// attr returns e's attribute local, "" when absent or e is nil.
func attr(e *ooxml.Element, local string) string {
	v, _ := e.Attr(local)
	return v
}

// boolAttr reads an xsd:boolean attribute, def when absent.
func boolAttr(e *ooxml.Element, local string, def bool) bool {
	v, ok := e.Attr(local)
	if !ok {
		return def
	}
	return ooxml.OnOff(v, true)
}

// emuAttr returns e's EMU attribute local in points, 0 when absent.
func emuAttr(e *ooxml.Element, local string) float64 {
	v, _ := ooxml.EMU(attr(e, local))
	return v
}

// intAttr returns e's integer attribute local and whether it parsed.
func intAttr(e *ooxml.Element, local string) (int, bool) {
	v, ok := e.Attr(local)
	if !ok {
		return 0, false
	}
	return ooxml.Int(v)
}

// partOf returns the part el belongs to: its owner when the reader
// registered one, else def.
func (r *reader) partOf(el *ooxml.Element, def string) string {
	if p, ok := r.owner[el]; ok {
		return p
	}
	return def
}

// own registers the placeholders and text styles of l as l's.
func (r *reader) own(l *layer) {
	for _, k := range l.phs {
		r.owner[k] = l.part
	}
	for _, s := range []*ooxml.Element{l.title, l.body, l.other} {
		if s != nil {
			r.owner[s] = l.part
		}
	}
}

// related returns the target of part's first internal relationship of
// type typ the package holds, "" for none.
func (r *reader) related(part, typ string) string {
	return r.pkg.RelatedPart(part, typ)
}
