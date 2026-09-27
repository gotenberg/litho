package mutations

import (
	"context"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// maxFieldParentDepth bounds the walk from a widget up to its root field.
// Anti-DoS guard, not a spec limit: a field tree is a handful of levels deep in
// practice (ISO 32000-1, 12.7.3.1 nests fields only to group them), but a
// crafted file can chain /Parent far deeper, and a cycle must not hang the
// writer.
//
// It does not track any other cap. This comment used to say it matched
// maxPageTreeDepth in catalog.go, which bounds the page tree at 256 and has
// nothing to do with how deep a widget's /Parent chain runs; the two numbers
// were never equal and nothing breaks when they differ. 32 is chosen for the
// field tree on its own terms.
//
// Five other constants bound this same /Parent ascent, at values that differ:
// maxFieldTreeDepth (64) and maxFieldAncestorDepth (32) in forms.go,
// maxFieldParentDepth in annotation/acroform.go (64), in
// remediation/fixer_annotappearance.go (32) and in compliance/annotations.go
// (100). They stay separate and cross-referenced rather than merged into one,
// because the difference cannot be reached: real acyclic /Parent chains stay
// far shallower than the smallest of them.
//
// This constant is borrowed for one walk that is not an ascent at all:
// pruneFieldKidsAt in dest_prune.go descends /Kids under it. The limit is fine
// there, the name is not, and that site says so.
//
// The number is not what makes any of these walks safe. A bound stops a cycle
// without detecting one, so the walk runs to the bound and then reports
// whichever node it halted on as though that were the answer. Cycles are real
// in shipped files. Every one of these walks therefore carries a visited set.
// Most reach it through one shared hop, [document.ResolveFieldParent], which
// owns the set and the detection together. [rootFieldOf] below is the
// exception: it reads the allocator's pending objects rather than the document,
// so it cannot use that hop and keeps a visited set of its own.
//
// # Corpus evidence
//
//   - All corpora, 5171 files: the deepest ACYCLIC /Parent chain is 7, so the
//     differing caps are never reached.
//   - All corpora: 16 /Parent chains in a single file are cyclic.
const maxFieldParentDepth = 32

// rebuildAcroForm writes an /AcroForm onto dst describing the form fields that
// survived into pageRefs, or leaves dst untouched when no widget did.
//
// /AcroForm lives in the catalog (ISO 32000-1, 12.7.2), and split and merge
// build a fresh one, so it used to be dropped along with the interactive form,
// leaving widgets that render but no longer do anything and breaking the
// field/widget association PDF/UA requires.
//
// The field objects themselves are already in the output: a widget carries
// /Parent (ISO 32000-1, 12.5.6.19) and the copier follows that chain, so the
// tree arrives with the pages. Only the catalog entry naming its roots is
// missing. Collecting those roots FROM THE COPIED WIDGETS is what keeps this
// correct by construction: a field whose widgets all landed on dropped pages is
// never named, so /Fields can never dangle.
//
// A merge that puts two copies of one form together yields two fields with the
// same fully qualified name. That is the linked-field behaviour of ISO 32000-1,
// 12.7.3.1, where fields sharing a name share a value, and it is what a viewer
// shows when the same form is appended twice. Renaming instead would be a
// policy choice that breaks callers keying on field names, so it is not made
// here.
//
// # Corpus evidence
//
//   - pdfjs 160F-2019.pdf: before the rebuild, the output kept all 76 widget
//     annotations on the page and lost every one of its 9 fields.
func rebuildAcroForm(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	pageRefs []ast.PdfObject,
	sources []acroFormSource,
	dst ast.PdfDict,
) {
	fields := collectRootFields(alloc, pageRefs, true)
	if len(fields) == 0 {
		return
	}

	acroForm := ast.PdfDict{"Fields": fields}

	// Carry the appearance defaults from the first source that has them: /DR
	// holds the resources a field's /DA names, so dropping it leaves a viewer
	// unable to regenerate an appearance (ISO 32000-1, 12.7.2, Table 218).
	for _, src := range sources {
		srcForm, err := document.ResolveDict(ctx, src.Doc, src.Catalog["AcroForm"])
		if err != nil || srcForm == nil {
			continue
		}
		// /SigFlags is deliberately absent: it advertises signatures the output
		// cannot carry (see dropsSignature).
		for _, key := range []ast.PdfName{"DR", "DA", "Q", "NeedAppearances"} {
			if _, taken := acroForm[key]; taken {
				continue
			}
			val, ok := srcForm[key]
			if !ok {
				continue
			}
			copied, err := src.Copier.CopyObject(ctx, val)
			if err != nil {
				src.Doc.Logger.Debug("copying AcroForm entry failed",
					slog.String("key", string(key)),
					slog.Any("err", err),
				)
				continue
			}
			acroForm[key] = copied
		}
	}

	dst["AcroForm"] = alloc.Alloc(acroForm)
}

// acroFormSource pairs a source document with the copier that brought its
// objects into the output, so [rebuildAcroForm] can copy the form's defaults.
type acroFormSource struct {
	Doc     *document.Document
	Catalog ast.PdfDict
	Copier  *writer.Copier
}

// collectRootFields returns the root field of every widget annotation on
// pageRefs, first-seen order, without duplicates, and without signature fields
// when dropSignatures is set (see [dropsSignature]).
//
// Order follows the pages and their /Annots arrays rather than a map, so the
// same input always produces the same /Fields array.
func collectRootFields(alloc *writer.ObjAllocator, pageRefs []ast.PdfObject, dropSignatures bool) ast.PdfArray {
	seen := make(map[int]bool)
	out := ast.PdfArray{}

	for _, pageObj := range pageRefs {
		pageRef, ok := pageObj.(ast.PdfIndirectRef)
		if !ok {
			continue
		}
		pageDict, ok := alloc.Pending()[pageRef.ObjectNum].(ast.PdfDict)
		if !ok {
			continue
		}
		annots, ok := pendingArray(alloc, pageDict["Annots"])
		if !ok {
			continue
		}
		for _, annotObj := range annots {
			annotRef, ok := annotObj.(ast.PdfIndirectRef)
			if !ok {
				continue
			}
			rootRef, ok := rootFieldOf(alloc, annotRef)
			if !ok || seen[rootRef.ObjectNum] {
				continue
			}
			if dropSignatures && dropsSignature(alloc, rootRef) {
				continue
			}
			seen[rootRef.ObjectNum] = true
			out = append(out, rootRef)
		}
	}
	return out
}

// rootFieldOf walks a widget's /Parent chain to the field that has no parent,
// reporting whether the annotation belongs to a form field at all.
//
// A widget merged with its field carries /FT or /T itself and is its own root
// (ISO 32000-1, 12.5.6.19).
func rootFieldOf(alloc *writer.ObjAllocator, annotRef ast.PdfIndirectRef) (ast.PdfIndirectRef, bool) {
	current := annotRef
	visited := make(map[int]bool)

	for depth := 0; depth <= maxFieldParentDepth; depth++ {
		if visited[current.ObjectNum] {
			return ast.PdfIndirectRef{}, false
		}
		visited[current.ObjectNum] = true

		dict, ok := alloc.Pending()[current.ObjectNum].(ast.PdfDict)
		if !ok {
			return ast.PdfIndirectRef{}, false
		}
		parent, hasParent := dict["Parent"].(ast.PdfIndirectRef)
		if !hasParent {
			if !isFieldRoot(dict) {
				return ast.PdfIndirectRef{}, false
			}
			return current, true
		}
		current = parent
	}
	return ast.PdfIndirectRef{}, false
}

// isFieldRoot reports whether a parentless dictionary reached from a page's
// /Annots is a form field.
//
// /FT or /T names a field outright (ISO 32000-1, Table 220). A parentless
// /Widget with neither is the merged field-and-annotation of 12.5.6.19 written
// without its required /FT: malformed, but it is still the field its page
// paints, and dropping it would strand the widget. Requiring the subtype keeps
// every other annotation, a link or a popup, out of the rebuilt field list.
//
// An annotation subtype other than /Widget settles it before /T is read,
// because /T means two unrelated things. On a field it is the partial field
// name; on a markup annotation it is the title of the pop-up window,
// conventionally the author (Table 170). Without this, a /Polygon carrying
// /T (author) is collected as a field root and split and merge write an
// /AcroForm onto a document that has no form at all. A non-terminal field node
// carries no /Subtype, so it still qualifies through /T.
//
// # Corpus evidence
//
//   - pdfjs issue20062.pdf: the only annotation is such a polygon, and a merge
//     turned nothing into a field named "author".
func isFieldRoot(dict ast.PdfDict) bool {
	if subtype, ok := dict["Subtype"].(ast.PdfName); ok && subtype != "Widget" {
		return false
	}
	if _, ok := dict["FT"]; ok {
		return true
	}
	if _, ok := dict["T"]; ok {
		return true
	}
	subtype, _ := dict["Subtype"].(ast.PdfName)
	return subtype == "Widget"
}

// pendingArray resolves an array that may itself be a reference into the
// allocator's pending objects.
func pendingArray(alloc *writer.ObjAllocator, obj ast.PdfObject) (ast.PdfArray, bool) {
	switch v := obj.(type) {
	case ast.PdfArray:
		return v, true
	case ast.PdfIndirectRef:
		arr, ok := alloc.Pending()[v.ObjectNum].(ast.PdfArray)
		return arr, ok
	}
	return nil, false
}

// dropsSignature reports whether a root field is a signed signature field,
// which split, merge and a page insertion leave behind.
//
// A signature's digest is computed over the entire file and its /ByteRange
// names the byte offsets it covers (ISO 32000-1, 12.8.1). Each of these writes
// a new file, so a carried signature can never verify again, and keeping it
// leaves a document that ADVERTISES a signature it cannot honour: veraPDF
// reports ISO 19005-2, 6.4.3 against the output. Dropping the field is the
// honest answer, and it is why /SigFlags is not carried either. Its widget does
// not stay behind: [bakeSignedSignatures] draws it into the page first.
//
// An unsigned signature field carries no digest, so it is kept as a field the
// output can still be signed in. Every signature field used to be dropped, and
// an unsigned one's widget stayed on the page outside the form.
func dropsSignature(alloc *writer.ObjAllocator, ref ast.PdfIndirectRef) bool {
	dict, ok := alloc.Pending()[ref.ObjectNum].(ast.PdfDict)
	if !ok {
		return false
	}
	return signedSignatureField(dict, func(value ast.PdfObject) bool {
		switch v := value.(type) {
		case ast.PdfDict:
			return true
		case ast.PdfIndirectRef:
			_, isDict := alloc.Pending()[v.ObjectNum].(ast.PdfDict)
			return isDict
		}
		return false
	})
}

// signedSignatureField reports whether field, a root field, is a signature
// field that is signed: its /V, which isDict must find a dictionary for, is
// the signature dictionary that holds the signature (ISO 32000-1, 12.7.4.5,
// Table 232). An unsigned signature field has no /V.
func signedSignatureField(field ast.PdfDict, isDict func(ast.PdfObject) bool) bool {
	if ft, _ := field["FT"].(ast.PdfName); ft != "Sig" {
		return false
	}
	value, has := field["V"]
	return has && isDict(value)
}
