package signature

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// maxWrapperContentBytes bounds the decoded content of an added page that
// [templateMatch.paintedForm] parses to tell whether it only paints a form
// XObject. It is an anti-DoS guard, not spec-mandated: a content stream that
// paints one form XObject holds a q, a cm, a Do and a Q, a few dozen bytes,
// so 4 KiB leaves room for whatever whitespace and comments a writer adds,
// and a longer stream draws more than that.
const maxWrapperContentBytes = 1 << 12

// streamEncodingEntries are the entries of a stream dictionary that say how
// its data is stored rather than what it holds (ISO 32000-1, 7.3.8.2, Table
// 5): its length, the filters that decode it with their parameters, and the
// decoded length a writer may state as a hint.
var streamEncodingEntries = map[ast.PdfName]bool{"Length": true, "Filter": true, "DecodeParms": true, "DL": true}

// streamFileEntries are the entries of a stream dictionary that place its data
// in an external file (ISO 32000-1, 7.3.8.2, Table 5), in which case the bytes
// between stream and endstream are ignored (7.3.8.1).
var streamFileEntries = []ast.PdfName{"F", "FFilter", "FDecodeParms"}

// formWrapperEntries are the entries a form XObject that paints a template's
// content may carry (ISO 32000-1, 8.10.2, Table 95; 7.3.8.2, Table 5): its
// type, bounding box, matrix and resources, how its data is stored, and the
// entries that describe it without changing what it paints or naming another
// object: its obsolete name, its modification date and its place in the
// structure tree. Of the others, a /Group composites its content as one group
// (8.10.3), a /Ref imports a page of another file (8.10.4), an /OPI replaces
// it when printing (14.11.7), an /OC skips it when its content is hidden
// (8.11.3.3), and /Metadata and /PieceInfo name objects nothing compares with
// the template's.
var formWrapperEntries = map[ast.PdfName]bool{
	"Type": true, "Subtype": true, "FormType": true, "BBox": true, "Matrix": true, "Resources": true,
	"Length": true, "Filter": true, "DecodeParms": true, "DL": true,
	"Name": true, "LastModified": true, "StructParent": true, "StructParents": true,
}

// decodedStreams compares streams by the data they hold once decoded, so that
// a copy that stores the same data through other filters, or compresses it
// differently, holds the same data. It keeps a digest of what each stream
// object of each document decodes to, so each is decoded once however many
// comparisons reach it. Each decode runs under the document's stream budget
// (Limits.MaxStreamBytes), so the comparisons decode no more than a pass over
// the document's streams does.
type decodedStreams struct {
	digests map[*document.Document]map[int]streamDigest
}

// streamDigest is the SHA-256 digest of the decoded data of a stream and the
// length of that data. ok is false when the data could not be decoded whole.
type streamDigest struct {
	sum  [sha256.Size]byte
	size int
	ok   bool
}

// digest returns the digest of the decoded data of stream, the object num of
// doc, or 0 for a direct stream, which is not kept. A stream whose data sits
// in an external file ([streamFileEntries]), a filter litho cannot decode, or
// data a filter cuts short, has none.
func (d *decodedStreams) digest(ctx context.Context, doc *document.Document, num int, stream ast.PdfStream) streamDigest {
	if dg, ok := d.digests[doc][num]; ok && num > 0 {
		return dg
	}
	var dg streamDigest
	if !inExternalFile(stream.Dict) {
		data, report, err := doc.DecodeStreamReport(ctx, stream)
		switch {
		case err != nil:
			doc.Logger.Debug("decoding a stream to compare with a template's", slog.Int("object", num), slog.Any("err", err))
		case !report.Incomplete():
			dg = streamDigest{sum: sha256.Sum256(data), size: len(data), ok: true}
		}
	}
	if num > 0 {
		if d.digests == nil {
			d.digests = make(map[*document.Document]map[int]streamDigest)
		}
		if d.digests[doc] == nil {
			d.digests[doc] = make(map[int]streamDigest)
		}
		d.digests[doc][num] = dg
	}
	return dg
}

// same reports whether the stream x of a and the stream y of b, the objects
// xNum and yNum, hold the same decoded data ([decodedStreams.digest]).
func (d *decodedStreams) same(ctx context.Context, a *document.Document, xNum int, x ast.PdfStream, b *document.Document, yNum int, y ast.PdfStream) bool {
	dx := d.digest(ctx, a, xNum, x)
	if !dx.ok {
		return false
	}
	dy := d.digest(ctx, b, yNum, y)
	return dy.ok && dx.size == dy.size && dx.sum == dy.sum
}

// withoutInstantiatedPages returns objects without the definitions that the
// pages a template instantiation added draw with and that the type-based
// classifier refuses, and a note for MDPInfo when they could not be told
// apart. policy is 2 or 3.
//
// ISO 32000-1, 12.8.2.2, Table 254 permits "instantiating page templates" from
// P=2 up, and 12.7.6 has an instantiation add a template page to the document
// as a regular page. That page may draw with objects of its own: a spawn may
// copy the template's fonts and images rather than share them, and Acrobat's
// Template.spawn paints a form XObject that holds the template's content
// ([templateMatch.wraps]). Such an object is exempt when it is one of
// [appearanceTypes], new ([signedRevision.held]), and reached from a page the
// update added that copies a template page of the signed revision
// ([instantiatedObjects]). The copy was compared with the template through
// everything it reaches that way, so the object draws what the template's own
// does.
func withoutInstantiatedPages(ctx context.Context, doc *document.Document, signed *signedRevision, objects []appendedObject, policy int) (_ []appendedObject, note string) {
	// A page the update added that draws with a new object is itself new,
	// since the signed revision refers to no new object, so an update that
	// writes no page adds no instantiation to tell apart.
	drawn, page := false, false
	for _, obj := range objects {
		drawn = drawn || (!obj.IsAnnot && appearanceTypes[obj.Type])
		page = page || obj.Type == "Page"
	}
	if !drawn || !page {
		return objects, ""
	}
	rev, err := signed.revision(ctx)
	if err != nil {
		return objects, fmt.Sprintf("; template instantiations not recognized: %v", err)
	}
	reached, err := instantiatedObjects(ctx, doc, rev, policy)
	if err != nil {
		return objects, fmt.Sprintf("; template instantiations not recognized: %v", err)
	}
	kept := make([]appendedObject, 0, len(objects))
	for _, obj := range objects {
		if !obj.IsAnnot && appearanceTypes[obj.Type] && reached[obj.ObjNum] && !signed.held(ctx, rev, obj.ObjNum) {
			continue
		}
		kept = append(kept, obj)
	}
	return kept, ""
}

// instantiatedObjects returns the objects doc reaches ([drawnFrom]) from the
// pages it adds to the page tree of the signed revision rev that copy a
// template page of rev ([templateMatch.instantiates]), through every entry of
// such a page but /Annots and /Parent, and through the resources it inherits.
// The widgets /Annots lists belong to fields, which [changedField] and
// [withoutFormData] judge, and /Parent places the page in the page tree.
func instantiatedObjects(ctx context.Context, doc, rev *document.Document, policy int) (map[int]bool, error) {
	signedPages, err := pagesOf(ctx, rev)
	if err != nil {
		return nil, fmt.Errorf("reading the signed revision's pages: %w", err)
	}
	currentPages, err := pagesOf(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("reading the current pages: %w", err)
	}
	signed, _ := byObjectNumber(signedPages)
	m := newTemplateMatch(doc, rev, signed, policy)
	var roots []ast.PdfObject
	seen := make(map[int]bool)
	for i, p := range currentPages {
		if i%100 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		num := p.Ref.ObjectNum
		if num <= 0 || signed[num] != nil || seen[num] {
			continue
		}
		seen[num] = true
		if !m.instantiates(ctx, p) {
			continue
		}
		for key, value := range p.Dict {
			if key != "Annots" && key != "Parent" {
				roots = append(roots, value)
			}
		}
		if p.Resources != nil {
			roots = append(roots, p.Resources)
		}
	}
	if len(roots) == 0 {
		return nil, nil
	}
	return drawnFrom(ctx, doc, roots)
}

// inExternalFile reports whether a stream dictionary places the stream's data
// in an external file ([streamFileEntries]).
func inExternalFile(dict ast.PdfDict) bool {
	for _, key := range streamFileEntries {
		if _, ok := dict[key]; ok {
			return true
		}
	}
	return false
}

// withoutEncoding returns dict without its [streamEncodingEntries].
func withoutEncoding(dict ast.PdfDict) ast.PdfDict {
	out := make(ast.PdfDict, len(dict))
	for key, value := range dict {
		if !streamEncodingEntries[key] {
			out[key] = value
		}
	}
	return out
}

// wraps reports whether page paints what the template page t, drawing with
// attrs, paints, by painting a form XObject that holds t's content. Acrobat's
// Template.spawn writes a spawned page that way: its JavaScript API reference
// has spawn return "an object representing the page contents of the page
// spawned", which a later spawn of the same template paints again rather than
// copy the content once more.
//
// Painting a form XObject paints its content stream with its resources, in
// form space mapped to user space by its matrix, clipped to its bounding box,
// in the graphics state in effect, which is saved and restored around it (ISO
// 32000-1, 8.10.1). The page paints what t paints when:
//
//   - its one content stream paints one form XObject its resources name and
//     nothing else ([templateMatch.paintedForm]), in the initial graphics
//     state and the default user space t's content is painted in, and its
//     resources name nothing else ([templateMatch.namesOnly]);
//   - the form XObject holds the decoded data of t's one content stream, draws
//     with t's resources, maps form space to user space unchanged, and clips
//     to a box that holds t's media box, outside which a page shows nothing
//     (14.11.2.1) ([templateMatch.paintsUnchanged]).
//
// Both sides have one content stream because 7.7.3.3, Table 30 has an array of
// them read as if they were concatenated, divided only between lexical tokens,
// which a reader may or may not separate when it joins them, so how an array
// is divided could change what one reader paints.
func (m *templateMatch) wraps(ctx context.Context, page *document.Page, t *templatePage, attrs pageAttributes) bool {
	if attrs.resources == nil || m.budget.decoded == nil {
		return false
	}
	templateNum, template, ok := soleContentStream(ctx, m.rev, t.dict["Contents"])
	if !ok {
		return false
	}
	wrapperNum, wrapper, ok := soleContentStream(ctx, m.doc, page.Dict["Contents"])
	if !ok {
		return false
	}
	name, ok := m.paintedForm(ctx, wrapperNum, wrapper)
	if !ok || !m.namesOnly(ctx, page.Resources, name) {
		return false
	}
	xobjects, err := document.ResolveDict(ctx, m.doc, page.Resources["XObject"])
	if err != nil || xobjects == nil {
		return false
	}
	form, err := document.ResolveStream(ctx, m.doc, xobjects[name])
	if err != nil || form.Dict == nil || !m.paintsUnchanged(ctx, form.Dict, attrs.mediaBox) {
		return false
	}
	return m.budget.same(ctx, m.doc, form.Dict["Resources"], m.rev, attrs.resources) &&
		m.budget.decoded.same(ctx, m.doc, refNum(xobjects[name]), form, m.rev, templateNum, template)
}

// namesOnly reports whether the resource dictionary resources of the current
// document names the XObject name and nothing else, bar a procedure set, an
// array of names (ISO 32000-1, 7.8.3, Table 33; 14.2), so that nothing a page
// painting only that XObject reaches through its resources goes uncompared.
func (m *templateMatch) namesOnly(ctx context.Context, resources ast.PdfDict, name ast.PdfName) bool {
	for key, value := range resources {
		switch key {
		case "XObject":
			xobjects, err := document.ResolveDict(ctx, m.doc, value)
			if err != nil || len(xobjects) != 1 || xobjects[name] == nil {
				return false
			}
		case "ProcSet":
			sets, err := document.ResolveArray(ctx, m.doc, value)
			if err != nil {
				return false
			}
			for _, set := range sets {
				if _, ok := set.(ast.PdfName); !ok {
					return false
				}
			}
		default:
			return false
		}
	}
	return true
}

// soleContentStream returns the one content stream a /Contents value of doc
// names, as a stream or as an array holding only it, with its object number.
func soleContentStream(ctx context.Context, doc *document.Document, contents ast.PdfObject) (int, ast.PdfStream, bool) {
	ref := contents
	resolved, err := resolvedIn(ctx, doc, ref)
	if err != nil {
		return 0, ast.PdfStream{}, false
	}
	if arr, ok := resolved.(ast.PdfArray); ok {
		if len(arr) != 1 {
			return 0, ast.PdfStream{}, false
		}
		ref = arr[0]
		if resolved, err = resolvedIn(ctx, doc, ref); err != nil {
			return 0, ast.PdfStream{}, false
		}
	}
	stream, ok := resolved.(ast.PdfStream)
	return refNum(ref), stream, ok
}

// paintedForm returns the name of the XObject the content stream num of the
// current document paints, when it paints one and nothing else, in the
// default user space: its operators are one Do and, around it, q and Q (ISO
// 32000-1, 8.4.4, Table 57), a Q never restoring more than the q before it
// saved, and cm with the identity matrix. The stream is parsed only when its
// decoded data is at most [maxWrapperContentBytes].
func (m *templateMatch) paintedForm(ctx context.Context, num int, stream ast.PdfStream) (ast.PdfName, bool) {
	if dg := m.budget.decoded.digest(ctx, m.doc, num, stream); !dg.ok || dg.size > maxWrapperContentBytes {
		return "", false
	}
	data, err := m.doc.DecodeStream(ctx, stream)
	if err != nil {
		return "", false
	}
	ops, err := content.ParseStreamData(ctx, data)
	if err != nil {
		return "", false
	}
	var name ast.PdfName
	saved := 0
	for _, op := range ops {
		ok := false
		switch op.Operator {
		case "q":
			saved++
			ok = len(op.Operands) == 0
		case "Q":
			saved--
			ok = len(op.Operands) == 0 && saved >= 0
		case "cm":
			ok = identityMatrix(numbersIn(ctx, m.doc, op.Operands))
		case "Do":
			// 8.8, Table 87: Do takes the name of the XObject it paints.
			var painted ast.PdfName
			painted, ok = soleName(op.Operands)
			ok = ok && name == ""
			name = painted
		}
		if !ok {
			return "", false
		}
	}
	return name, name != ""
}

// soleName returns the operand of an operator that takes one name.
func soleName(operands []ast.PdfObject) (ast.PdfName, bool) {
	if len(operands) != 1 {
		return "", false
	}
	name, ok := operands[0].(ast.PdfName)
	return name, ok && name != ""
}

// paintsUnchanged reports whether the form XObject form of the current
// document paints its content as the page that paints it would paint it
// itself: it is a form XObject (ISO 32000-1, 8.10.2, Table 95) that carries
// only [formWrapperEntries], maps form space to user space by the identity,
// its /Matrix absent or the identity, and clips to a bounding box that holds
// mediaBox, the template's media box.
func (m *templateMatch) paintsUnchanged(ctx context.Context, form ast.PdfDict, mediaBox ast.PdfArray) bool {
	for key := range form {
		if !formWrapperEntries[key] {
			return false
		}
	}
	if dictName(form, "Subtype") != "Form" {
		return false
	}
	if _, ok := form["Type"]; ok && dictName(form, "Type") != "XObject" {
		return false
	}
	if formType, ok := form["FormType"]; ok {
		if n := numbersIn(ctx, m.doc, []ast.PdfObject{formType}); len(n) != 1 || n[0] != 1 {
			return false
		}
	}
	if matrix, ok := form["Matrix"]; ok && !identityMatrix(numbersOf(ctx, m.doc, matrix)) {
		return false
	}
	bbox, ok := normalizedRect(numbersOf(ctx, m.doc, form["BBox"]))
	if !ok {
		return false
	}
	media, ok := normalizedRect(numbersIn(ctx, m.rev, mediaBox))
	return ok && bbox[0] <= media[0] && bbox[1] <= media[1] && bbox[2] >= media[2] && bbox[3] >= media[3]
}

// identityMatrix reports whether nums is the identity matrix [1 0 0 1 0 0]
// (ISO 32000-1, 8.3.4).
func identityMatrix(nums []float64) bool {
	return len(nums) == 6 && nums[0] == 1 && nums[1] == 0 && nums[2] == 0 && nums[3] == 1 && nums[4] == 0 && nums[5] == 0
}

// normalizedRect returns the rectangle nums describes with its lower-left
// corner first. ISO 32000-1, 7.9.5 lets a rectangle name any two diagonally
// opposite corners.
func normalizedRect(nums []float64) ([4]float64, bool) {
	if len(nums) != 4 {
		return [4]float64{}, false
	}
	return [4]float64{min(nums[0], nums[2]), min(nums[1], nums[3]), max(nums[0], nums[2]), max(nums[1], nums[3])}, true
}

// numbersOf returns the array obj of doc as numbers, or nil when it is no
// array of numbers.
func numbersOf(ctx context.Context, doc *document.Document, obj ast.PdfObject) []float64 {
	arr, err := document.ResolveArray(ctx, doc, obj)
	if err != nil || arr == nil {
		return nil
	}
	return numbersIn(ctx, doc, arr)
}

// numbersIn returns the objects of doc as numbers, resolving references, or
// nil when one is no number.
func numbersIn(ctx context.Context, doc *document.Document, objs []ast.PdfObject) []float64 {
	nums := make([]float64, 0, len(objs))
	for _, obj := range objs {
		resolved, err := resolvedIn(ctx, doc, obj)
		if err != nil {
			return nil
		}
		v, ok := document.Number(resolved)
		if !ok {
			return nil
		}
		nums = append(nums, v)
	}
	return nums
}
