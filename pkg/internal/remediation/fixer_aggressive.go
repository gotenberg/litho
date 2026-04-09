package remediation

import (
	"bytes"
	"context"
	"fmt"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// validOperators is the set of PDF content stream operators defined in
// ISO 32000-2, Tables 50-77.
//
//nolint:gochecknoglobals // constant lookup table
var validOperators = map[string]bool{
	"w": true, "J": true, "j": true, "M": true, "d": true, "ri": true, "i": true, "gs": true,
	"q": true, "Q": true, "cm": true,
	"m": true, "l": true, "c": true, "v": true, "y": true, "h": true, "re": true,
	"S": true, "s": true, "f": true, "F": true, "f*": true, "B": true, "B*": true,
	"b": true, "b*": true, "n": true, "W": true, "W*": true,
	"BT": true, "ET": true,
	"Tc": true, "Tw": true, "Tz": true, "TL": true, "Tf": true, "Tr": true, "Ts": true,
	"Td": true, "TD": true, "Tm": true, "T*": true, "Tj": true, "TJ": true, "'": true, "\"": true,
	"d0": true, "d1": true,
	"CS": true, "cs": true, "SC": true, "SCN": true, "sc": true, "scn": true,
	"G": true, "g": true, "RG": true, "rg": true, "K": true, "k": true,
	"sh": true,
	"BI": true, "ID": true, "EI": true,
	"Do": true,
	"MP": true, "DP": true, "BMC": true, "BDC": true, "EMC": true, "BX": true, "EX": true,
}

// matchAggressive matches violations that require content modification to fix.
// These are only applied when PreserveContent is false.
func matchAggressive(v compliance.Violation) bool {
	switch v.Rule.Clause {
	case "6.3.3", "6.5.3", "6.9": // annotation/form appearances
		return true
	case "6.2.9", "6.2.6": // rendering intent in content streams
		return true
	case "6.2.2": // unknown content stream operators
		return true
	}
	return false
}

// fixAggressive applies fixes that modify content streams or generate visual
// content. Requires opts.PreserveContent == false.
func fixAggressive(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, opts Options, violations []compliance.Violation, _ string) ([]string, error) {
	if opts.PreserveContent {
		return nil, nil
	}

	clauseSet := make(map[string]bool)
	for _, v := range violations {
		clauseSet[v.Rule.Clause] = true
	}

	var fixes []string

	if clauseSet["6.3.3"] || clauseSet["6.5.3"] || clauseSet["6.9"] {
		desc, err := fixAnnotationAppearances(ctx, alloc, doc)
		if err != nil {
			return nil, fmt.Errorf("fixing annotation appearances: %w", err)
		}
		fixes = append(fixes, desc...)
	}

	if clauseSet["6.2.9"] || clauseSet["6.2.6"] || clauseSet["6.2.2"] {
		desc, err := fixContentStreams(ctx, alloc, doc,
			clauseSet["6.2.9"] || clauseSet["6.2.6"],
			clauseSet["6.2.2"])
		if err != nil {
			return nil, fmt.Errorf("fixing content streams: %w", err)
		}
		fixes = append(fixes, desc...)
	}

	return fixes, nil
}

// fixAnnotationAppearances generates minimal empty appearance streams for
// annotations missing /AP.
func fixAnnotationAppearances(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) ([]string, error) {
	catalog, err := document.GetCatalog(ctx, doc)
	if err != nil {
		return nil, err
	}
	pages, err := document.GetPages(ctx, doc, catalog)
	if err != nil {
		return nil, err
	}

	var count int
	for _, page := range pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		annotsObj, ok := page.Dict["Annots"]
		if !ok {
			continue
		}
		annots, err := document.ResolveArray(ctx, doc, annotsObj)
		if err != nil || annots == nil {
			continue
		}

		for _, annotObj := range annots {
			ref, isRef := annotObj.(ast.PdfIndirectRef)
			annotDict, err := document.ResolveDict(ctx, doc, annotObj)
			if err != nil || annotDict == nil {
				continue
			}
			if _, hasAP := annotDict["AP"]; hasAP {
				continue
			}

			emptyStream := ast.NewStream(ast.PdfDict{
				"Type":    ast.PdfName("XObject"),
				"Subtype": ast.PdfName("Form"),
				"BBox":    ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(0)},
			}, []byte("q Q"))
			streamRef := alloc.Alloc(emptyStream)

			newAnnot := make(ast.PdfDict, len(annotDict)+1)
			maps.Copy(newAnnot, annotDict)
			newAnnot["AP"] = ast.PdfDict{"N": streamRef}

			if isRef {
				alloc.AllocAt(ref.ObjectNum, newAnnot)
			}
			count++
		}
	}

	if count == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("generated empty appearance streams for %d annotations", count)}, nil
}

// fixContentStreams parses page content streams and fixes rendering intent
// operators and removes unknown operators between BX/EX markers.
func fixContentStreams(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, fixRI, fixUnknown bool) ([]string, error) {
	catalog, err := document.GetCatalog(ctx, doc)
	if err != nil {
		return nil, err
	}
	pages, err := document.GetPages(ctx, doc, catalog)
	if err != nil {
		return nil, err
	}

	var totalRI, totalUnknown int

	for pageNum, page := range pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		contentsObj, ok := page.Dict["Contents"]
		if !ok {
			continue
		}

		ops, err := content.ParsePageContent(ctx, doc, page.Dict)
		if err != nil {
			continue
		}

		modified := false
		inCompat := 0

		for i, op := range ops {
			if op.Operator == "BX" {
				inCompat++
			}
			if op.Operator == "EX" && inCompat > 0 {
				inCompat--
			}

			if fixRI && op.Operator == "ri" && len(op.Operands) == 1 {
				if name, ok := op.Operands[0].(ast.PdfName); ok {
					if !isValidRenderingIntent(string(name)) {
						ops[i].Operands[0] = ast.PdfName("RelativeColorimetric")
						totalRI++
						modified = true
					}
				}
			}

			if fixUnknown && inCompat > 0 && op.Operator != "BX" && op.Operator != "EX" {
				if !validOperators[op.Operator] {
					ops[i].Operator = ""
					totalUnknown++
					modified = true
				}
			}
		}

		if !modified {
			continue
		}

		filtered := filterOps(ops)
		newData := serializeOps(filtered)

		if err := replaceContentStream(ctx, alloc, doc, contentsObj, newData, pageNum+1); err != nil {
			return nil, err
		}
	}

	var fixes []string
	if totalRI > 0 {
		fixes = append(fixes, fmt.Sprintf("replaced %d invalid rendering intents in content streams", totalRI))
	}
	if totalUnknown > 0 {
		fixes = append(fixes, fmt.Sprintf("removed %d unknown operators from BX/EX sections", totalUnknown))
	}
	return fixes, nil
}

func isValidRenderingIntent(intent string) bool {
	switch intent {
	case "AbsoluteColorimetric", "RelativeColorimetric", "Saturation", "Perceptual":
		return true
	}
	return false
}

func filterOps(ops []content.Operation) []content.Operation {
	result := make([]content.Operation, 0, len(ops))
	for _, op := range ops {
		if op.Operator != "" {
			result = append(result, op)
		}
	}
	return result
}

func serializeOps(ops []content.Operation) []byte {
	var buf bytes.Buffer
	for _, op := range ops {
		for i, operand := range op.Operands {
			if i > 0 {
				buf.WriteByte(' ')
			}
			writeOperand(&buf, operand)
		}
		if len(op.Operands) > 0 {
			buf.WriteByte(' ')
		}
		buf.WriteString(op.Operator)
		buf.WriteByte('\n')
	}
	return buf.Bytes()
}

func writeOperand(buf *bytes.Buffer, obj ast.PdfObject) {
	switch v := obj.(type) {
	case ast.PdfInt:
		fmt.Fprintf(buf, "%d", int64(v))
	case ast.PdfReal:
		fmt.Fprintf(buf, "%g", float64(v))
	case ast.PdfName:
		buf.WriteByte('/')
		buf.WriteString(string(v))
	case ast.PdfString:
		buf.WriteByte('(')
		for _, b := range []byte(v) {
			if b == '(' || b == ')' || b == '\\' {
				buf.WriteByte('\\')
			}
			buf.WriteByte(b)
		}
		buf.WriteByte(')')
	case ast.PdfBool:
		if v {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case ast.PdfNull:
		buf.WriteString("null")
	case ast.PdfArray:
		buf.WriteByte('[')
		for i, elem := range v {
			if i > 0 {
				buf.WriteByte(' ')
			}
			writeOperand(buf, elem)
		}
		buf.WriteByte(']')
	case ast.PdfDict:
		buf.WriteString("<<")
		for k, val := range v {
			buf.WriteString(" /")
			buf.WriteString(string(k))
			buf.WriteByte(' ')
			writeOperand(buf, val)
		}
		buf.WriteString(">>")
	default:
		fmt.Fprintf(buf, "%v", obj)
	}
}

func replaceContentStream(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, contentsObj ast.PdfObject, newData []byte, pageNum int) error {
	ref, isRef := contentsObj.(ast.PdfIndirectRef)
	if !isRef {
		return fmt.Errorf("page %d /Contents is not an indirect reference", pageNum)
	}

	resolved, err := document.ResolveObject(ctx, doc, contentsObj)
	if err != nil {
		return fmt.Errorf("resolving /Contents for page %d: %w", pageNum, err)
	}

	switch resolved.(type) {
	case ast.PdfStream:
		alloc.AllocAt(ref.ObjectNum, ast.NewStream(ast.PdfDict{}, newData))
	case ast.PdfArray:
		newStream := ast.NewStream(ast.PdfDict{}, newData)
		newRef := alloc.Alloc(newStream)
		alloc.AllocAt(ref.ObjectNum, newRef)
	default:
		return fmt.Errorf("page %d /Contents has unexpected type %T", pageNum, resolved)
	}
	return nil
}

// DecodeStream is checked via interface to avoid import cycle.
// The document.Document type has a DecodeStream method we need.
func init() {
	// Verify at compile time that document.Document has the method we need.
	var _ interface {
		DecodeStream(context.Context, ast.PdfStream) ([]byte, error)
	} = (*document.Document)(nil)
}
