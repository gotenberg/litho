package docx

import (
	"context"
	"fmt"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// understood lists the namespaces whose mc:Choice the converter reads
// (ECMA-376 Part 3, 10.2): the Word 2010 shapes, groups and drawing
// extensions, and DrawingML; any other Choice takes its Fallback.
var understood = []string{ooxml.NSW, ooxml.NSWPS, ooxml.NSWPG, ooxml.NSWP14, ooxml.NSW14, ooxml.NSA, ooxml.NSPic, ooxml.NSWP}

// settings are the document settings the converter reads (ECMA-376 Part
// 1, 17.15).
type settings struct {
	// defaultTab is the default tab stop interval in points (17.15.1.25).
	defaultTab float64
	// evenAndOdd selects distinct even page headers and footers
	// (17.10.1).
	evenAndOdd bool
	lang       string
	// compat is the Word version the document asks to be laid out as
	// (MS-DOCX, 2.3.4 compatibilityMode): 15 for Word 2013 and later, 0
	// when unset, which reads as an earlier version.
	compat int
}

// document is one conversion's state: the package, its resolved parts
// and what the build collects.
type document struct {
	ctx       context.Context
	pkg       *ooxml.Package
	main      string
	fonts     *text.Fonts
	opts      office.Options
	theme     *ooxml.Theme
	styles    *styleSheet
	numbering *numbering
	settings  settings
	footnotes map[string]*ooxml.Element
	endnotes  map[string]*ooxml.Element
	// footnotesPart and endnotesPart name the parts the notes come from,
	// whose relationships their hyperlinks and pictures resolve against.
	footnotesPart, endnotesPart string
	// linePitch is the line pitch in points of the document grid of the
	// section whose body is being built (17.6.5 docGrid of type lines or
	// linesAndChars), 0 for none.
	linePitch float64

	warnings []compose.Warning
	warned   map[string]bool
	headings []heading
	// nextID numbers the anchors the converter makes up.
	nextID int
	// pictures caches decoded pictures by part name.
	pictures map[string]*layout.Picture
	// lineFactors caches the ascent, descent and line gap of a font, as
	// factors of its size.
	lineFactors map[string][3]float64
	// fontClass maps a lower-cased family to its fallback face.
	fontClass map[string]string
	// noteNumber counts the footnotes and endnotes numbered so far.
	footnoteNumber, endnoteNumber int
	endnoteBodies                 []*layout.Node
	// elements counts the layout nodes built, against [maxNodes].
	nodes int
	// pending collects the drawings of the section being read whose
	// position needs its page; inBody is set while the body is read and
	// nested counts the table cells, text boxes and notes being built,
	// whose drawings are not anchored to the page.
	pending []pendingAnchor
	// columnBreaks collects the paragraphs of the section being read
	// that a column break starts.
	columnBreaks []*css.Style
	inBody       bool
	nested       int
	// inCell counts the table cells being built, whose paragraphs'
	// frame properties are ignored.
	inCell int
	// vmlTypes maps a VML shape type's id to its shape type number
	// (o:spt, Part 4, 14.1.2.20), for the shapes that name it.
	vmlTypes map[string]int
}

// heading is a paragraph with an outline level, for the outline.
type heading struct {
	node  *layout.Node
	level int
}

// maxNodes caps the layout nodes the converter builds. Anti-DoS guard, not
// spec-mandated: it bounds the tree a document of [ooxml.MaxElements]
// elements expands to through fields, numbering and repeated headers.
const maxNodes = 4_000_000

// load opens the package's main part and the parts it relates to.
func load(ctx context.Context, pkg *ooxml.Package, fonts *text.Fonts, opts office.Options) (*document, error) {
	main, err := pkg.MainPart()
	if err != nil {
		return nil, err
	}
	if ct := pkg.ContentType(main); ct != "" && !strings.Contains(ct, "wordprocessingml") {
		return nil, fmt.Errorf("%w: the main part is %s, not WordprocessingML", office.ErrMalformed, ct)
	}
	d := &document{
		ctx: ctx, pkg: pkg, main: main, fonts: fonts, opts: opts,
		warned: map[string]bool{}, pictures: map[string]*layout.Picture{}, lineFactors: map[string][3]float64{}, vmlTypes: map[string]int{},
		settings: settings{defaultTab: 36},
	}
	d.theme = &ooxml.Theme{}
	d.readFontTable(nil)
	if name := pkg.RelatedPart(main, ooxml.RelTheme); name != "" {
		if root, err := d.part(name); err == nil {
			d.theme = ooxml.ParseTheme(root)
		} else if !isSoft(err) {
			return nil, err
		}
	}
	if name := pkg.RelatedPart(main, ooxml.RelSettings); name != "" {
		root, err := d.part(name)
		if err != nil && !isSoft(err) {
			return nil, err
		}
		d.readSettings(root)
	}
	var stylesRoot *ooxml.Element
	if name := pkg.RelatedPart(main, ooxml.RelStyles); name != "" {
		if stylesRoot, err = d.part(name); err != nil && !isSoft(err) {
			return nil, err
		}
	}
	d.styles = d.parseStyles(stylesRoot)
	var numRoot *ooxml.Element
	if name := pkg.RelatedPart(main, ooxml.RelNumbering); name != "" {
		if numRoot, err = d.part(name); err != nil && !isSoft(err) {
			return nil, err
		}
	}
	d.numbering = d.parseNumbering(numRoot)
	var fontRoot *ooxml.Element
	if name := pkg.RelatedPart(main, ooxml.RelFontTable); name != "" {
		if fontRoot, err = d.part(name); err != nil && !isSoft(err) {
			return nil, err
		}
	}
	d.readFontTable(fontRoot)
	d.footnotesPart = pkg.RelatedPart(main, ooxml.RelFootnotes)
	if d.footnotes, err = d.readNotes(d.footnotesPart, "footnote"); err != nil {
		return nil, err
	}
	d.endnotesPart = pkg.RelatedPart(main, ooxml.RelEndnotes)
	if d.endnotes, err = d.readNotes(d.endnotesPart, "endnote"); err != nil {
		return nil, err
	}
	return d, nil
}

// part parses the part name with the converter's markup compatibility.
func (d *document) part(name string) (*ooxml.Element, error) {
	return d.pkg.ParseXML(d.ctx, name, understood)
}

// isSoft reports an error a secondary part may fail with and the
// conversion survive: the part is malformed or missing. A cap or a
// cancelled context stops the conversion.
func isSoft(err error) bool {
	return err != nil && (ooxml.IsMissing(err) || isMalformed(err))
}

// readSettings reads the settings part's root, w:settings.
func (d *document) readSettings(root *ooxml.Element) {
	if root == nil {
		return
	}
	if v, ok := ooxml.Twips(attrOr(root.Child(w, "defaultTabStop"), "val")); ok && v > 0 {
		d.settings.defaultTab = v
	}
	if e := root.Child(w, "evenAndOddHeaders"); e != nil {
		d.settings.evenAndOdd = ooxml.Toggle(e)
	}
	if t := root.Child(w, "themeFontLang"); t != nil {
		d.settings.lang = attrOr(t, "val")
	}
	for _, c := range root.Child(w, "compat").Kids() {
		if c.Is(w, "compatSetting") && attrOr(c, "name") == "compatibilityMode" {
			if v, ok := ooxml.Int(attrOr(c, "val")); ok {
				d.settings.compat = v
			}
		}
	}
}

// readNotes reads the footnotes or endnotes part name into its notes by
// ID; the separators (types separator, continuationSeparator) are left
// out.
func (d *document) readNotes(name, kind string) (map[string]*ooxml.Element, error) {
	notes := map[string]*ooxml.Element{}
	if name == "" {
		return notes, nil
	}
	root, err := d.part(name)
	if err != nil {
		if isSoft(err) {
			d.warn("malformed-part", fmt.Sprintf("the %s part does not parse; its notes are left out", kind))
			return notes, nil
		}
		return nil, err
	}
	for _, n := range root.Kids() {
		if !n.Is(w, kind) {
			continue
		}
		if t := attrOr(n, "type"); t != "" && t != "normal" {
			continue
		}
		notes[attrOr(n, "id")] = n
	}
	return notes, nil
}

// warn records a warning, once per message.
func (d *document) warn(code, msg string) {
	key := code + "\x00" + msg
	if d.warned[key] {
		return
	}
	d.warned[key] = true
	d.warnings = append(d.warnings, compose.Warning{Code: code, Message: msg})
}

// newID returns an anchor ID no document name can take.
func (d *document) newID(prefix string) string {
	d.nextID++
	return fmt.Sprintf("\x00%s%d", prefix, d.nextID)
}
