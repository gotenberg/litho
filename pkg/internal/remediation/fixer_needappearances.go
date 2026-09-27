package remediation

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

func matchNeedAppearances(v compliance.Violation) bool {
	return descriptionContains(v, "NeedAppearances")
}

func fixNeedAppearances(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalogRef, ok := catalogRefForFixer(ctx, doc)
	if !ok {
		return nil, nil
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, nil
	}

	acroFormObj, ok := catalogDict["AcroForm"]
	if !ok {
		return nil, nil
	}

	var acroForm ast.PdfDict
	if ref, ok := acroFormObj.(ast.PdfIndirectRef); ok {
		acroForm = resolveFromAllocOrDoc(ctx, alloc, doc, ref)
	}
	if acroForm == nil {
		resolved, err := document.ResolveDict(ctx, doc, acroFormObj)
		if err != nil || resolved == nil {
			return nil, nil
		}
		acroForm = resolved
	}

	// Removing the flag hands authority to the stored appearances. When a field
	// has a value that only the viewer's own generation puts on the page, that
	// discards it, so the violation stays as a residual the caller can see.
	if needAppearancesShowsAValue(ctx, alloc, doc) {
		return nil, nil
	}

	newAcroForm := make(ast.PdfDict, len(acroForm))
	maps.Copy(newAcroForm, acroForm)
	// ISO 19005-2/3 §6.9/§6.4: the AcroForm /NeedAppearances flag shall not be true.
	delete(newAcroForm, "NeedAppearances")

	if ref, ok := acroFormObj.(ast.PdfIndirectRef); ok {
		alloc.AllocAt(ref.ObjectNum, newAcroForm)
	} else {
		// AcroForm is inline in catalog. Update catalog.
		newCatalog := make(ast.PdfDict, len(catalogDict))
		maps.Copy(newCatalog, catalogDict)
		newCatalog["AcroForm"] = newAcroForm
		alloc.AllocAt(catalogRef.ObjectNum, newCatalog)
	}

	fixes := []string{"removed /NeedAppearances from AcroForm"}
	if n := normaliseWidgetAppearances(ctx, alloc, doc); n > 0 {
		fixes = append(fixes, fmt.Sprintf("gave %d widget appearance stream(s) the form XObject entries they lacked", n))
	}
	return fixes, nil
}

// normaliseWidgetAppearances gives every widget appearance stream the /Subtype
// and /BBox a form XObject requires, returning how many it repaired.
//
// Removing /NeedAppearances hands authority back to the stored appearances: the
// viewer stops regenerating them and draws what the file holds. An appearance
// stream is a form XObject (ISO 32000-1, 12.5.5) and Table 95 makes /Subtype
// /Form and /BBox required, but a producer that expected regeneration can leave
// both out and never notice. Removing the flag to satisfy ISO 19005-2, 6.9 then
// turns a visible tick into nothing: the file becomes conformant and loses the
// mark that says the box is checked.
//
// Only missing entries are added. A stream that declares its own box is placed
// by it, and 12.5.5 maps that box onto /Rect, so overriding it would move the
// appearance.
//
// # Corpus evidence
//
//   - pdfjs checkbox-bad-appearance.pdf: its appearance streams leave out
//     /Subtype and /BBox, so removing the flag turned a visible tick into
//     nothing.
func normaliseWidgetAppearances(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) int {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return 0
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return 0
	}

	repaired := 0
	for _, page := range pages {
		if err := ctx.Err(); err != nil {
			return repaired
		}
		for _, annotObj := range liveArray(ctx, alloc, doc, livePageDict(ctx, alloc, doc, page)["Annots"]) {
			annot := liveAnnotation(ctx, alloc, doc, annotObj)
			if annot == nil {
				continue
			}
			ap, err := document.ResolveDict(ctx, doc, annot["AP"])
			if err != nil || ap == nil {
				continue
			}
			width, height := annotRectSize(ctx, doc, annot)
			for _, state := range ap {
				repaired += normaliseAppearanceEntry(ctx, alloc, doc, state, width, height)
			}
		}
	}
	return repaired
}

// normaliseAppearanceEntry repairs one /AP entry, which is either an appearance
// stream or a subdictionary of named states (ISO 32000-1, 12.5.5).
func normaliseAppearanceEntry(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, entry ast.PdfObject, width, height float64) int {
	if n := repairAppearanceStream(ctx, alloc, doc, entry, width, height); n > 0 {
		return n
	}
	states, err := document.ResolveDict(ctx, doc, entry)
	if err != nil || states == nil {
		return 0
	}
	repaired := 0
	for _, state := range states {
		repaired += repairAppearanceStream(ctx, alloc, doc, state, width, height)
	}
	return repaired
}

// repairAppearanceStream rewrites one appearance stream in place when it is
// missing form XObject entries. Reports 1 when it wrote something.
//
// Only an indirect reference is repaired: ISO 32000-1, 7.3.8 requires every
// stream to be an indirect object, so there is no direct one to rewrite, and a
// state that is a name (an /Off with no appearance) carries nothing to fix.
func repairAppearanceStream(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, obj ast.PdfObject, width, height float64) int {
	ref, ok := obj.(ast.PdfIndirectRef)
	if !ok || ref.ObjectNum <= 0 {
		return 0
	}
	resolved, err := doc.Resolve(ctx, ref)
	if err != nil {
		return 0
	}
	stream, ok := resolved.(ast.PdfStream)
	if !ok {
		return 0
	}
	_, hasSubtype := stream.Dict["Subtype"]
	_, hasBBox := stream.Dict["BBox"]
	if hasSubtype && hasBBox {
		return 0
	}

	dict := make(ast.PdfDict, len(stream.Dict)+3)
	maps.Copy(dict, stream.Dict)
	dict["Type"] = ast.PdfName("XObject")
	if !hasSubtype {
		dict["Subtype"] = ast.PdfName("Form")
	}
	if !hasBBox {
		dict["BBox"] = ast.PdfArray{
			ast.PdfInt(0), ast.PdfInt(0),
			ast.PdfReal(width), ast.PdfReal(height),
		}
	}
	if !alloc.AllocAt(ref.ObjectNum, ast.NewLazyStream(dict, stream.Bytes)) {
		return 0
	}
	return 1
}

// annotRectSize returns the annotation rectangle's width and height, normalised
// because its corners may be given in either order (ISO 32000-1, 7.9.5).
func annotRectSize(ctx context.Context, doc *document.Document, annot ast.PdfDict) (float64, float64) {
	rect, err := document.ResolveArray(ctx, doc, annot["Rect"])
	if err != nil || len(rect) < 4 {
		return 0, 0
	}
	return math.Abs(document.Float(rect[2]) - document.Float(rect[0])),
		math.Abs(document.Float(rect[3]) - document.Float(rect[1]))
}

// catalogRefForFixer returns the catalog's indirect reference, defined as the
// object the reader, the validator and the full-rewrite writer all treat as the
// catalog: [document.Document.CatalogRef]. That honours the trailer /Root when
// it resolves to a catalog and recovers by scanning the object table when it
// does not.
//
// Taking the trailer /Root at face value instead is wrong twice over when a
// damaged incremental update points it at a non-catalog. Per ISO 32000-1 §7.5.5
// Table 15 /Root shall be the catalog, so such a file is malformed, and the
// reader already recovers from it. Fixers wrote their repairs into whatever
// object /Root named, so a file whose final trailer points /Root at the /Info
// dictionary had its catalog repairs merged into its document information
// dictionary while the real catalog kept none of them. Remediation reported
// those repairs as applied and then listed the very same violations as
// residual, never converging.
//
// The raw trailer ref remains the fallback: when recovery finds no catalog at
// all, a reference the file at least names beats no reference. ok is false only
// when neither yields one.
//
// # Corpus evidence
//
//   - pdfjs issue9418.pdf: its final trailer points /Root at the /Info
//     dictionary, and /MarkInfo, /Lang, /Metadata, /StructTreeRoot and
//     /ViewerPreferences were merged into that dictionary while the real
//     catalog kept none of them.
func catalogRefForFixer(ctx context.Context, doc *document.Document) (ast.PdfIndirectRef, bool) {
	if ref, ok := doc.CatalogRef(ctx); ok {
		return ref, true
	}
	if ref, ok := doc.Index.Trailer["Root"].(ast.PdfIndirectRef); ok {
		return ref, true
	}
	return ast.PdfIndirectRef{}, false
}

// resolveFromAllocOrDoc checks the allocator for a pending version, falling
// back to the document. Shared by all fixers.
func resolveFromAllocOrDoc(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, ref ast.PdfIndirectRef) ast.PdfDict {
	if obj, ok := alloc.Pending()[ref.ObjectNum]; ok {
		if d, ok := obj.(ast.PdfDict); ok {
			return d
		}
	}
	d, err := document.ResolveDict(ctx, doc, ref)
	if err != nil {
		return nil
	}
	return d
}

// maxAppearanceNestDepth bounds how far the search for a text-showing operator
// follows nested form XObjects inside an appearance. This is an anti-DoS guard,
// not a spec limit: a crafted appearance can nest indefinitely, while a real one
// nests a level or two (the /FRM wrapper Acrobat writes around signature art).
const maxAppearanceNestDepth = 3

// needAppearancesShowsAValue reports whether any field on the page is showing
// its value only because /NeedAppearances asks the viewer to build the
// appearance.
//
// ISO 32000-1, 12.7.3.3 lets a producer store a placeholder appearance and set
// the flag, leaving the viewer to draw /V. Removing the flag to satisfy ISO
// 19005-2, 6.9 then makes that placeholder authoritative, and remediation
// silently drops the field values from the page.
//
// Only a field whose stored appearance shows no text counts. One that already
// draws its value loses nothing when the viewer stops regenerating.
//
// # Corpus evidence
//
//   - pdfjs issue12233.pdf: keeps 12-to-45 byte appearances that paint no
//     glyphs at all, and remediation silently dropped the field values "Man",
//     "150" and "Red" from the page.
func needAppearancesShowsAValue(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) bool {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return false
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return false
	}

	for _, page := range pages {
		if err := ctx.Err(); err != nil {
			return false
		}
		for _, annotObj := range liveArray(ctx, alloc, doc, livePageDict(ctx, alloc, doc, page)["Annots"]) {
			annot := liveAnnotation(ctx, alloc, doc, annotObj)
			if annot == nil {
				continue
			}
			subtype, _ := document.ResolveName(ctx, doc, annot["Subtype"])
			if !rendersOwnValue(ctx, doc, annot, subtype) {
				continue
			}
			if !appearanceShowsValue(ctx, doc, annot) {
				return true
			}
		}
	}
	return false
}

// firstInvocation returns the offset at which a content stream first names an
// XObject and invokes it with Do (ISO 32000-1, 8.8), or -1 when it never does.
func firstInvocation(data []byte, name ast.PdfName) int {
	needle := append([]byte{'/'}, []byte(name)...)
	for i := 0; ; {
		j := bytes.Index(data[i:], needle)
		if j < 0 {
			return -1
		}
		rest := data[i+j+len(needle):]
		// The name ends here only if a delimiter follows, so /Im4 does not
		// match /Im41.
		if len(rest) > 0 && !isPdfDelimiter(rest[0]) {
			i += j + len(needle)
			continue
		}
		if bytes.HasPrefix(bytes.TrimLeft(rest, " \t\r\n"), []byte("Do")) {
			return i + j
		}
		i += j + len(needle)
	}
}

// isPdfDelimiter reports whether c ends a name token (ISO 32000-1, 7.2.2).
func isPdfDelimiter(c byte) bool {
	switch c {
	case ' ', '\t', '\r', '\n', '\f', 0, '/', '[', ']', '<', '>', '(', ')', '{', '}', '%':
		return true
	}
	return false
}

// passwordFieldFlag is bit 14 of a text field's /Ff, which hides the value
// behind an echo character (ISO 32000-1, Table 231).
const passwordFieldFlag = 1 << 13

// appearanceShowsValue reports whether the widget's stored appearance already
// paints the value the field holds, so removing /NeedAppearances shows the same
// thing a regenerating viewer would.
//
// A stored appearance can be STALE, which is the whole reason a producer sets
// the flag (ISO 32000-1, 12.7.3.3). Dropping the flag over a stale appearance
// makes the document display data the field does not hold.
//
// A password field is exempt: Table 231 has it echo a masking character instead
// of the value, so its appearance is meant to differ.
//
// # Corpus evidence
//
//   - pdfjs bug1796741.pdf: holds the value "Dlrow SJ.fdp Olleh" behind an
//     appearance painting "Hello PDF.js World".
//   - pdfjs bug1844583.pdf: holds the reverse of bug1796741.pdf.
func appearanceShowsValue(ctx context.Context, doc *document.Document, annot ast.PdfDict) bool {
	if fieldFlags(ctx, doc, annot)&passwordFieldFlag != 0 {
		return true
	}
	value := fieldValueText(ctx, doc, annot)
	if value == "" {
		return true
	}
	shown := appearanceText(ctx, doc, annot["AP"], 0)
	return strings.Contains(stripSpaces(shown), stripSpaces(value))
}

// fieldFlags returns the field's /Ff, following /Parent because the entry is
// inheritable (ISO 32000-1, 12.7.3.1).
func fieldFlags(ctx context.Context, doc *document.Document, annot ast.PdfDict) int {
	dict := annot
	seen := make(map[int]bool)
	for depth := 0; depth < maxFieldParentDepth && dict != nil; depth++ {
		if flags, ok := dict["Ff"].(ast.PdfInt); ok {
			return int(flags)
		}
		_, parent, ok := document.ResolveFieldParent(ctx, doc, dict, seen)
		if !ok {
			return 0
		}
		dict = parent
	}
	return 0
}

// fieldValueText returns the field's /V as text, following /Parent.
func fieldValueText(ctx context.Context, doc *document.Document, annot ast.PdfDict) string {
	dict := annot
	seen := make(map[int]bool)
	for depth := 0; depth < maxFieldParentDepth && dict != nil; depth++ {
		if value, ok := dict["V"]; ok {
			if text, ok := value.(ast.PdfString); ok {
				return document.DecodePdfString(text)
			}
			return ""
		}
		_, parent, ok := document.ResolveFieldParent(ctx, doc, dict, seen)
		if !ok {
			return ""
		}
		dict = parent
	}
	return ""
}

// appearanceText concatenates the strings an appearance shows with Tj or TJ.
//
// The pieces join in a fixed order: invoked XObjects in the order the stream
// paints them, appearance states by name. Ranging the resource and state maps
// joined them in a different order on every run, so a value split across two
// XObjects matched on some runs only, and the same bytes kept or dropped
// /NeedAppearances at random (CONTRIBUTING "Deterministic output").
func appearanceText(ctx context.Context, doc *document.Document, obj ast.PdfObject, depth int) string {
	if depth > maxAppearanceNestDepth || obj == nil {
		return ""
	}
	resolved, err := document.ResolveObject(ctx, doc, obj)
	if err != nil {
		return ""
	}
	switch v := resolved.(type) {
	case ast.PdfStream:
		data, err := doc.DecodeStream(ctx, v)
		if err != nil {
			return ""
		}
		out := shownStrings(data)
		// Follow only the XObjects this stream invokes: a /Resources dictionary
		// lists more than its stream uses, so walking all of them reads text
		// from images the appearance never draws.
		nested, err := document.ResolveDict(ctx, doc, v.Dict["Resources"])
		if err != nil || nested == nil {
			return out
		}
		inner, err := document.ResolveDict(ctx, doc, nested["XObject"])
		if err != nil || inner == nil {
			return out
		}
		type invocation struct {
			at    int
			child ast.PdfObject
		}
		var invoked []invocation
		for name, child := range inner {
			if at := firstInvocation(data, name); at >= 0 {
				invoked = append(invoked, invocation{at: at, child: child})
			}
		}
		// Two names never share an offset: a name only matches where a
		// delimiter ends it, so the order is total.
		slices.SortFunc(invoked, func(a, b invocation) int { return cmp.Compare(a.at, b.at) })
		for _, inv := range invoked {
			out += appearanceText(ctx, doc, inv.child, depth+1)
		}
		return out
	case ast.PdfDict:
		var out strings.Builder
		for _, key := range slices.Sorted(maps.Keys(v)) {
			out.WriteString(appearanceText(ctx, doc, v[key], depth+1))
		}
		return out.String()
	}
	return ""
}

// shownStrings pulls the literal string operands out of a content stream. It
// reads the bytes as written, which matches the value for the simple encodings
// these placeholder appearances use; a font that reorders or remaps them yields
// no match, and the caller then keeps the flag, which is the safe direction.
func shownStrings(data []byte) string {
	var out strings.Builder
	for i := 0; i < len(data); i++ {
		if data[i] != '(' {
			continue
		}
		depth := 1
		i++
		for ; i < len(data) && depth > 0; i++ {
			switch data[i] {
			case '\\':
				i++
				continue
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					continue
				}
			}
			if depth > 0 {
				out.WriteByte(data[i])
			}
		}
		i--
	}
	return out.String()
}

// stripSpaces removes whitespace so a difference in spacing does not read as a
// difference in content.
func stripSpaces(s string) string {
	var out strings.Builder
	for _, r := range s {
		if r != ' ' && r != '\t' && r != '\r' && r != '\n' {
			out.WriteRune(r)
		}
	}
	return out.String()
}
