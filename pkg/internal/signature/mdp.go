package signature

import (
	"context"
	"fmt"
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

		if pObj, ok := tpDict["P"]; ok {
			if pInt, ok := pObj.(ast.PdfInt); ok {
				p := int(pInt)
				if p >= 1 && p <= 3 {
					return p
				}
			}
		}

		// DocMDP defaults to P=2 if not specified.
		if tmName == "DocMDP" {
			return 2
		}
	}

	return 0
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

	if len(appendedObjects) == 0 {
		result.MDPValid = true
		result.MDPInfo = fmt.Sprintf("no new objects after signing (P=%d)", sig.MDPPolicy)
		return
	}

	violation := classifyModifications(appendedObjects, sig.MDPPolicy)
	if violation != "" {
		result.MDPValid = false
		result.MDPInfo = fmt.Sprintf("P=%d violation: %s", sig.MDPPolicy, violation)
	} else {
		result.MDPValid = true
		result.MDPInfo = fmt.Sprintf("P=%d: %d appended objects are within policy", sig.MDPPolicy, len(appendedObjects))
	}
}

// appendedObject holds a parsed object from the post-signature region.
type appendedObject struct {
	ObjNum int
	Type   string // /Type value if present, empty otherwise
	FT     string // /FT value for form fields, empty otherwise
	IsXRef bool   // true if this is an xref table/stream (not a content object)
}

// objectHeaderRe matches "N G obj" headers in the appended data.
var objectHeaderRe = regexp.MustCompile(`(\d+)\s+\d+\s+obj\b`)

// parseAppendedObjects scans the bytes between signedEnd and fileEnd for
// indirect object definitions and extracts their /Type values.
func parseAppendedObjects(ctx context.Context, doc *document.Document, signedEnd, fileEnd int64) ([]appendedObject, error) {
	data, err := doc.ReadFileBytes(ctx, signedEnd, fileEnd-signedEnd)
	if err != nil {
		return nil, err
	}

	var objects []appendedObject

	// Find all object headers.
	matches := objectHeaderRe.FindAllIndex(data, -1)
	for _, loc := range matches {
		// Parse the object starting at the header position.
		offset := signedEnd + int64(loc[0])
		lex := lexer.NewLexer(ioat.NewReader(doc.RawReader(), fileEnd, offset))
		p := parser.NewParser(lex)
		obj, err := p.ParseObject(ctx)
		if err != nil {
			continue
		}

		ao := appendedObject{}

		// Extract object number from the match.
		header := string(data[loc[0]:loc[1]])
		var objNum int
		if _, err := fmt.Sscanf(header, "%d", &objNum); err == nil {
			ao.ObjNum = objNum
		}

		switch v := obj.(type) {
		case ast.PdfDict:
			ao.Type = dictName(v, "Type")
			ao.FT = dictName(v, "FT")
		case ast.PdfStream:
			ao.Type = dictName(v.Dict, "Type")
			if ao.Type == "XRef" {
				ao.IsXRef = true
			}
		}

		objects = append(objects, ao)
	}

	return objects, nil
}

// classifyModifications checks whether the appended objects violate the MDP policy.
// Returns an empty string if compliant, or a violation description.
func classifyModifications(objects []appendedObject, policy int) string {
	for _, obj := range objects {
		if obj.IsXRef {
			// XRef tables/streams are structural overhead, always allowed.
			continue
		}

		switch policy {
		case 1:
			// No changes allowed at all.
			return fmt.Sprintf("object %d (%s) added after signing", obj.ObjNum, objDescription(obj))

		case 2:
			// Only form filling and additional signatures allowed.
			if !isAllowedP2(obj) {
				return fmt.Sprintf("object %d (%s) is not a form field or signature", obj.ObjNum, objDescription(obj))
			}

		case 3:
			// Form filling, signing, and annotations allowed.
			if !isAllowedP3(obj) {
				return fmt.Sprintf("object %d (%s) is not a form field, signature, or annotation", obj.ObjNum, objDescription(obj))
			}
		}
	}

	return ""
}

// isAllowedP2 returns true if the object is allowed under DocMDP P=2
// (form filling and signing only).
func isAllowedP2(obj appendedObject) bool {
	switch obj.Type {
	case "Sig", "SigRef":
		return true
	case "Annot":
		// Widget annotations (form field appearances) are allowed.
		return true
	case "Catalog", "Pages", "Page":
		// Structural updates for adding signatures are allowed.
		return true
	case "":
		// Objects without /Type include form field values, appearance streams,
		// and other signature-related metadata.
		if obj.FT == "Sig" || obj.FT == "Tx" || obj.FT == "Btn" || obj.FT == "Ch" {
			return true
		}
		// Typeless objects are conservatively allowed. They may be
		// appearance streams or font resources needed for form rendering.
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
	// P=3 additionally allows annotations.
	return obj.Type == "Annot"
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
	if obj.Type != "" {
		return obj.Type
	}
	if obj.FT != "" {
		return "field:" + obj.FT
	}
	return "untyped"
}
