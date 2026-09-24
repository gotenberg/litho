package signature

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/ioat"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
	"github.com/gotenberg/litho/pkg/internal/core/parser"
	"github.com/gotenberg/litho/pkg/models"
)

// extractMDPPolicy reads the /Reference array from a signature dictionary and
// returns the DocMDP or FieldMDP /P (permissions) value. Returns 0 if no MDP
// transform is specified.
func extractMDPPolicy(ctx context.Context, doc *document.Document, sigDict ast.PdfDict) int {
	refObj, ok := sigDict["Reference"]
	if !ok {
		return 0
	}

	refArr, err := document.ResolveArray(ctx, doc, refObj)
	if err != nil {
		return 0
	}

	if refArr == nil {
		return 0
	}

	for _, item := range refArr {
		itemDict, err := document.ResolveDict(ctx, doc, item)
		if err != nil {
			continue
		}
		if itemDict == nil {
			continue
		}

		tmObj, ok := itemDict["TransformMethod"]
		if !ok {
			continue
		}
		tmName, err := document.ResolveName(ctx, doc, tmObj)
		if err != nil {
			continue
		}
		if tmName == "" {
			continue
		}

		if tmName != "DocMDP" && tmName != "FieldMDP" {
			continue
		}

		// Extract /TransformParams -> /P.
		tpObj, ok := itemDict["TransformParams"]
		if !ok {
			continue
		}
		tpDict, err := document.ResolveDict(ctx, doc, tpObj)
		if err != nil {
			continue
		}
		if tpDict == nil {
			continue
		}

		// ISO 32000-1, 12.8.2.2, Table 254 gives P the values 1 to 3 and a
		// default of 2. Table 256 gives the FieldMDP transform parameters no
		// P entry, so only DocMDP falls back to the default. A FieldMDP /P is
		// still read when a producer writes one.
		if pObj, ok := tpDict["P"]; ok {
			if p, ok := mdpPermission(ctx, doc, pObj); ok {
				return p
			}
		}

		if tmName == "DocMDP" {
			return 2
		}
	}

	return 0
}

// mdpPermission reads a transform's /P entry as one of the three DocMDP access
// permissions, and reports false when it names none of them.
//
// ISO 32000-1, 12.8.2.2, Table 254 types P as a number, so a real whose value
// is exactly 1, 2 or 3 names that permission as the integer does: /P 1.0 is
// P=1. The table defines no other value and says nothing of one, so a value
// outside {1, 2, 3}, a fraction such as 2.5, or a non-number reports false and
// the caller reads it as an absent P, which for DocMDP is the table's default
// of 2.
func mdpPermission(ctx context.Context, doc *document.Document, obj ast.PdfObject) (int, bool) {
	resolved, err := document.ResolveObject(ctx, doc, obj)
	if err != nil {
		// An unresolvable /P names no permission, and Table 254 gives an
		// absent P its default, so the caller applies it.
		doc.Logger.Debug("resolving MDP transform /P", slog.Any("err", err))
		return 0, false
	}
	v, ok := document.Number(resolved)
	if !ok {
		return 0, false
	}
	switch v {
	case 1:
		return 1, true
	case 2:
		return 2, true
	case 3:
		return 3, true
	}
	return 0, false
}

// evaluateMDP checks whether the document was modified after the signature was
// applied and whether those modifications comply with the MDP policy.
func evaluateMDP(ctx context.Context, doc *document.Document, sig models.Signature, result *models.SignatureValidation) {
	if sig.MDPPolicy == 0 {
		return
	}

	result.MDPPolicy = sig.MDPPolicy

	fileSize := doc.FileSize()

	// A /ByteRange naming bytes the file does not contain describes no signed
	// revision. Adding such a pair can also overflow int64 into a negative
	// signedEnd, which passes the comparison below and then reads the whole
	// file as appended data.
	if !byteRangeReadable(sig.ByteRange, fileSize) {
		result.MDPInfo = "signature /ByteRange does not describe bytes this file contains"
		return
	}

	// The signature's ByteRange defines which bytes were signed.
	// ByteRange = [off1, len1, off2, len2]
	// The signed range ends at off2 + len2. Any bytes after that are
	// incremental updates appended after signing.
	signedEnd := sig.ByteRange[2] + sig.ByteRange[3]

	if signedEnd >= fileSize {
		// No modifications after signing.
		result.MDPValid = true
		result.MDPInfo = fmt.Sprintf("no modifications after signing (P=%d)", sig.MDPPolicy)
		return
	}

	// There are bytes after the signed range. Parse the appended objects
	// to determine if they violate the MDP policy.
	appendedObjects, err := parseAppendedObjects(ctx, doc, signedEnd, fileSize)
	if err != nil {
		result.MDPInfo = fmt.Sprintf("unable to parse appended data: %v", err)
		return
	}

	violation := classifyModifications(appendedObjects, sig.MDPPolicy)

	// Table 254 permits annotation deletion only at P=3, and P=1 already
	// refuses the page rewrite a deletion needs. At P=2 a deletion appends
	// nothing the classifier forbids, so the signed revision's /Annots are
	// compared with the current ones. That runs even when nothing was
	// appended: a cross-reference section alone can free an annotation.
	var unchecked string
	if violation == "" && sig.MDPPolicy == 2 {
		rev, err := doc.Revision(ctx, signedEnd)
		if err == nil {
			violation, err = removedAnnotation(ctx, doc, rev)
		}
		if err != nil {
			unchecked = fmt.Sprintf("; annotation removals not checked: %v", err)
		}
	}

	switch {
	case violation != "":
		result.MDPValid = false
		result.MDPInfo = fmt.Sprintf("P=%d violation: %s", sig.MDPPolicy, violation)
	case len(appendedObjects) == 0:
		result.MDPValid = true
		result.MDPInfo = fmt.Sprintf("no new objects after signing (P=%d)%s", sig.MDPPolicy, unchecked)
	default:
		result.MDPValid = true
		result.MDPInfo = fmt.Sprintf("P=%d: %d appended objects are within policy%s", sig.MDPPolicy, len(appendedObjects), unchecked)
	}
}

// appendedObject holds a parsed object from the post-signature region.
type appendedObject struct {
	ObjNum  int
	Type    string // /Type value if present, empty otherwise
	Subtype string // /Subtype value of an annotation, empty otherwise
	FT      string // /FT value for form fields, empty otherwise
	IsXRef  bool   // true if this is an xref table/stream (not a content object)
	IsAnnot bool   // true if this is an annotation dictionary

	// Unread is set on an object stream whose objects could not be read, and
	// says why. The objects it holds are then unknown.
	Unread string
}

// objectHeaderRe matches "N G obj" headers in the appended data.
var objectHeaderRe = regexp.MustCompile(`(\d+)\s+\d+\s+obj\b`)

// parseAppendedObjects scans the bytes between signedEnd and fileEnd for
// indirect object definitions and records the entries classifyModifications
// reads.
//
// An object stream is not recorded itself: the objects it holds are, each
// described exactly as an object written outside a stream would be. ISO
// 32000-1, 7.5.7 lets a writer store any object other than a stream in one, so
// an incremental update that compresses its objects (7.5.8 introduced both
// together) changes the document through the objects inside and never through
// the container.
func parseAppendedObjects(ctx context.Context, doc *document.Document, signedEnd, fileEnd int64) ([]appendedObject, error) {
	data, err := doc.ReadFileBytes(ctx, signedEnd, fileEnd-signedEnd)
	if err != nil {
		return nil, err
	}

	var objects []appendedObject

	// Find all object headers.
	matches := objectHeaderRe.FindAllIndex(data, -1)
	for i, loc := range matches {
		if i%100 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		// Parse the object starting at the header position.
		offset := signedEnd + int64(loc[0])
		lex := lexer.NewLexer(ioat.NewReader(doc.RawReader(), fileEnd, offset))
		p := parser.NewParser(lex)
		obj, err := p.ParseObject(ctx)
		if err != nil {
			continue
		}

		// Extract the object and generation numbers from the match.
		var objNum, genNum int
		if _, err := fmt.Sscanf(string(data[loc[0]:loc[1]]), "%d %d", &objNum, &genNum); err != nil {
			doc.Logger.Debug("reading appended object header", slog.Any("err", err))
		}

		if stream, ok := obj.(ast.PdfStream); ok && dictName(stream.Dict, "Type") == "ObjStm" {
			objects = append(objects, appendedObjectStream(ctx, doc, stream, offset, fileEnd, objNum, genNum)...)
			continue
		}
		objects = append(objects, describeAppended(objNum, obj))
	}

	return objects, nil
}

// appendedObjectStream describes the objects an appended object stream holds.
// When they cannot be read, it returns the stream alone, marked Unread, so the
// classifier reports objects it could not check rather than passing them.
func appendedObjectStream(ctx context.Context, doc *document.Document, stream ast.PdfStream, offset, fileEnd int64, objNum, genNum int) []appendedObject {
	if doc.Crypto != nil {
		// ISO 32000-1, 7.5.7: in an encrypted file the object stream is
		// encrypted as a whole, under its own object's key (7.6.2), and the
		// strings inside it are not encrypted again. Only the stream bytes
		// need the key, so the object is parsed again with it.
		lex := lexer.NewLexer(ioat.NewReader(doc.RawReader(), fileEnd, offset))
		p := parser.NewParser(lex).WithDecryption(doc.Crypto.DecryptFunc(objNum, genNum))
		obj, err := p.ParseObject(ctx)
		if s, ok := obj.(ast.PdfStream); err == nil && ok {
			stream = s
		}
	}

	contained, err := doc.DecodeObjectStream(ctx, objNum, stream)
	if err != nil {
		return []appendedObject{{ObjNum: objNum, Type: "ObjStm", Unread: err.Error()}}
	}
	objects := make([]appendedObject, 0, len(contained))
	for _, c := range contained {
		objects = append(objects, describeAppended(c.Num, c.Object))
	}
	return objects
}

// describeAppended records the entries classifyModifications reads from an
// appended object.
func describeAppended(objNum int, obj ast.PdfObject) appendedObject {
	ao := appendedObject{ObjNum: objNum}
	switch v := obj.(type) {
	case ast.PdfDict:
		ao.Type = dictName(v, "Type")
		ao.FT = dictName(v, "FT")
		if isAnnotationDict(v) {
			ao.IsAnnot = true
			ao.Subtype = dictName(v, "Subtype")
		}
	case ast.PdfStream:
		ao.Type = dictName(v.Dict, "Type")
		if ao.Type == "XRef" {
			ao.IsXRef = true
		}
	}
	return ao
}

// classifyModifications checks whether the appended objects violate the MDP policy.
// Returns an empty string if compliant, or a violation description.
//
// The policies are those of ISO 32000-1, 12.8.2.2, Table 254. P=1 permits no
// change, P=2 permits filling in forms, instantiating page templates and
// signing, and P=3 adds annotation creation, deletion and modification.
// 12.8.2.2.2 lets a reader "compare the signed and current versions of the
// document". This classifier sees only the /Type, the /FT and, on an
// annotation, the /Subtype of each appended object, so it approximates that
// comparison: it cannot tell a filled-in field from another change to an
// object of the same type.
func classifyModifications(objects []appendedObject, policy int) string {
	for _, obj := range objects {
		if obj.IsXRef {
			// Every incremental update appends a cross-reference section
			// (ISO 32000-1, 7.5.6), which changes no object by itself. A free
			// entry in that section deletes an object, and this classifier
			// does not read the entries.
			continue
		}
		if obj.Unread != "" {
			return fmt.Sprintf("object %d is an object stream whose objects could not be read to check them: %s", obj.ObjNum, obj.Unread)
		}

		switch policy {
		case 1:
			// Table 254, P=1: any change invalidates the signature.
			return fmt.Sprintf("object %d (%s) added after signing", obj.ObjNum, objDescription(obj))

		case 2:
			// Table 254, P=2: form filling, page templates and signing.
			if !isAllowedP2(obj) {
				if obj.IsAnnot {
					return fmt.Sprintf("object %d (%s) is an annotation, which P=2 permits only as a form field widget", obj.ObjNum, objDescription(obj))
				}
				return fmt.Sprintf("object %d (%s) is not a form field or signature", obj.ObjNum, objDescription(obj))
			}

		case 3:
			// Table 254, P=3: P=2 plus annotation changes.
			if !isAllowedP3(obj) {
				return fmt.Sprintf("object %d (%s) is not a form field, signature, or annotation", obj.ObjNum, objDescription(obj))
			}
		}
	}

	return ""
}

// isAllowedP2 returns true if the object is allowed under DocMDP P=2
// (form filling and signing only).
//
// ISO 32000-1, 12.8.2.2, Table 254 permits, at P=2, "filling in forms,
// instantiating page templates, and signing". A signature adds a /Sig value
// and its /SigRef, a filled field rewrites its widget and field dictionaries
// (12.7.3.1, Table 220 names the field types Btn, Tx, Ch and Sig), and adding
// either touches the catalog's form and the page's /Annots, while a template
// adds a page.
func isAllowedP2(obj appendedObject) bool {
	if obj.IsAnnot {
		// Table 254 permits annotation creation, deletion and modification
		// only at P=3. Filling in forms and signing touch only widget
		// annotations, which carry a field's appearance (12.5.6.19), a
		// signature field's included (12.7.4.5). Any other annotation added or
		// rewritten after signing is a change P=2 forbids.
		return obj.Subtype == "Widget"
	}
	switch obj.Type {
	case "Sig", "SigRef":
		return true
	case "Catalog", "Pages", "Page":
		// Adding a field or a signature rewrites the catalog and the page that
		// lists its widget, and instantiating a template adds a page.
		// Deleting an annotation rewrites the page too, which Table 254
		// forbids at P=2: removedAnnotation finds it by comparing the page's
		// /Annots with the signed revision's.
		return true
	case "":
		// Objects without /Type include form field values, appearance streams,
		// and other signature-related metadata.
		if obj.FT == "Sig" || obj.FT == "Tx" || obj.FT == "Btn" || obj.FT == "Ch" {
			return true
		}
		// Deliberate leniency: Table 254 invalidates every change outside the
		// permitted ones, but a typeless object may be an appearance stream or
		// a font a filled field needs, or a rewritten page content stream,
		// and the /Type alone cannot tell them apart. The classifier reports
		// such a file as within policy rather than flag every form fill.
		return true
	}
	return false
}

// isAllowedP3 returns true if the object is allowed under DocMDP P=3
// (form filling, signing, and annotations).
func isAllowedP3(obj appendedObject) bool {
	if isAllowedP2(obj) {
		return true
	}
	// ISO 32000-1, Table 254, P=3 adds "annotation creation, deletion, and
	// modification" to P=2.
	return obj.IsAnnot
}

// isAnnotationDict reports whether a dictionary is an annotation.
//
// ISO 32000-1, 12.5.2, Table 164 makes /Type optional on an annotation and
// /Subtype and /Rect required, so an annotation whose producer omits /Type
// still carries both. /Subtype is not matched against the annotation types of
// Table 169: a producer may write a type the table does not list, and that
// annotation changes the document all the same.
//
// # Corpus evidence
//
//   - All corpora: 4176 dictionaries in 35 files carry /Subtype and /Rect
//     without /Type, and all but one name a Table 169 annotation type.
//   - pdfjs issue18444.pdf: the exception has /Subtype /Arrow and the /AP, /P
//     and /NM entries of an annotation.
func isAnnotationDict(dict ast.PdfDict) bool {
	switch dictName(dict, "Type") {
	case "Annot":
		return true
	case "":
		_, hasRect := dict["Rect"]
		return hasRect && dictName(dict, "Subtype") != ""
	}
	return false
}

// dictName extracts a named key from a dictionary, or returns "".
func dictName(dict ast.PdfDict, key ast.PdfName) string {
	if obj, ok := dict[key]; ok {
		if name, ok := obj.(ast.PdfName); ok {
			return string(name)
		}
	}
	return ""
}

// objDescription returns a human-readable description of an appended object.
func objDescription(obj appendedObject) string {
	if obj.IsAnnot {
		if obj.Subtype != "" {
			return "Annot:" + obj.Subtype
		}
		return "Annot"
	}
	if obj.Type != "" {
		return obj.Type
	}
	if obj.FT != "" {
		return "field:" + obj.FT
	}
	return "untyped"
}
