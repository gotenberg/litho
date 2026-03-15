package structure

import (
	"context"
	"fmt"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/typography"
	"github.com/gotenberg/litho/pkg/models"
)

// StructElem represents a logical structure element in a Tagged PDF.
type StructElem struct {
	Type string
	MCID int
	Kids []*StructElem
}

// resolve is a helper to resolve indirect references.
func resolve(ctx context.Context, doc *document.Document, obj ast.PdfObject) (ast.PdfObject, error) {
	if ref, ok := obj.(ast.PdfIndirectRef); ok {
		return doc.Resolve(ctx, ref)
	}
	return obj, nil
}

// GetStructTree extracts the root of the logical structure tree.
func GetStructTree(ctx context.Context, doc *document.Document) (*StructElem, error) {
	catalogObj, ok := doc.Index.Trailer["Root"]
	if !ok {
		return nil, fmt.Errorf("trailer missing /Root")
	}

	catRes, err := resolve(ctx, doc, catalogObj)
	if err != nil {
		return nil, err
	}

	catalog, ok := catRes.(ast.PdfDict)
	if !ok {
		return nil, fmt.Errorf("catalog is not a dictionary")
	}

	strTreeObj, ok := catalog["StructTreeRoot"]
	if !ok {
		return nil, nil // Not a tagged PDF
	}

	strTreeRes, err := resolve(ctx, doc, strTreeObj)
	if err != nil {
		return nil, err
	}

	strTree, ok := strTreeRes.(ast.PdfDict)
	if !ok {
		return nil, fmt.Errorf("StructTreeRoot is not a dictionary")
	}

	rootElem := &StructElem{Type: "Root", MCID: -1}

	if kObj, ok := strTree["K"]; ok {
		kRes, _ := resolve(ctx, doc, kObj)
		if kArr, ok := kRes.(ast.PdfArray); ok {
			for _, kid := range kArr {
				if child := parseStructElem(ctx, doc, kid); child != nil {
					rootElem.Kids = append(rootElem.Kids, child)
				}
			}
		} else if kDict, ok := kRes.(ast.PdfDict); ok {
			if child := parseStructElem(ctx, doc, kDict); child != nil {
				rootElem.Kids = append(rootElem.Kids, child)
			}
		}
	}

	return rootElem, nil
}

func parseStructElem(ctx context.Context, doc *document.Document, obj ast.PdfObject) *StructElem {
	res, err := resolve(ctx, doc, obj)
	if err != nil {
		return nil
	}

	switch v := res.(type) {
	case ast.PdfInt:
		// Direct MCID reference
		return &StructElem{Type: "MCID", MCID: int(v)}
	case ast.PdfDict:
		elem := &StructElem{MCID: -1}

		// Check if it's an MCR (Marked Content Reference) dictionary
		if tObj, ok := v["Type"]; ok {
			tRes, _ := resolve(ctx, doc, tObj)
			if tName, ok := tRes.(ast.PdfName); ok && tName == "MCR" {
				if mcidObj, ok := v["MCID"]; ok {
					mcidRes, _ := resolve(ctx, doc, mcidObj)
					if mcidInt, ok := mcidRes.(ast.PdfInt); ok {
						return &StructElem{Type: "MCID", MCID: int(mcidInt)}
					}
				}
			}
		}

		// Some PDFs just drop the MCID in a generic dict
		if mcidObj, ok := v["MCID"]; ok {
			mcidRes, _ := resolve(ctx, doc, mcidObj)
			if mcidInt, ok := mcidRes.(ast.PdfInt); ok {
				return &StructElem{Type: "MCID", MCID: int(mcidInt)}
			}
		}

		// It is a standard StructElem. Extract its Structure Type (/S)
		if sObj, ok := v["S"]; ok {
			sRes, _ := resolve(ctx, doc, sObj)
			if sName, ok := sRes.(ast.PdfName); ok {
				elem.Type = string(sName)
			}
		} else {
			elem.Type = "Unknown"
		}

		// Recursively parse children (/K)
		if kObj, ok := v["K"]; ok {
			kRes, _ := resolve(ctx, doc, kObj)
			if kArr, ok := kRes.(ast.PdfArray); ok {
				for _, kid := range kArr {
					if child := parseStructElem(ctx, doc, kid); child != nil {
						elem.Kids = append(elem.Kids, child)
					}
				}
			} else {
				if child := parseStructElem(ctx, doc, kRes); child != nil {
					elem.Kids = append(elem.Kids, child)
				}
			}
		}

		return elem
	}
	return nil
}

// ExtractSemanticData marries the spatial TextElements with the logical Structure Tree
// to produce structurally valid HTML output.
func ExtractSemanticData(root *StructElem, elements []models.TextElement) string {
	// Group elements by MCID
	mcidGroups := make(map[int][]models.TextElement)
	for _, el := range elements {
		if el.MCID != -1 {
			mcidGroups[el.MCID] = append(mcidGroups[el.MCID], el)
		}
	}

	// Reconstruct the text block for each MCID using the spatial heuristic
	mcidTextMap := make(map[int]string)
	for mcid, group := range mcidGroups {
		mcidTextMap[mcid] = typography.ReconstructText(group, nil)
	}

	var sb strings.Builder
	renderHTML(root, mcidTextMap, &sb)
	return sb.String()
}

func renderHTML(node *StructElem, mcidMap map[int]string, sb *strings.Builder) {
	if node == nil {
		return
	}

	// If this node is a leaf mapping directly to an MCID, output the aggregated text
	if node.Type == "MCID" {
		if text, ok := mcidMap[node.MCID]; ok {
			// Escape basic HTML entities for safety
			safeText := strings.ReplaceAll(text, "&", "&amp;")
			safeText = strings.ReplaceAll(safeText, "<", "&lt;")
			safeText = strings.ReplaceAll(safeText, ">", "&gt;")
			sb.WriteString(safeText)
		}
		return
	}

	// Map PDF structure tags to HTML tags
	tag := "div"
	switch node.Type {
	case "P":
		tag = "p"
	case "H1", "H2", "H3", "H4", "H5", "H6":
		tag = strings.ToLower(node.Type)
	case "Table":
		tag = "table"
	case "TR":
		tag = "tr"
	case "TH", "TD":
		tag = strings.ToLower(node.Type)
	case "L":
		tag = "ul"
	case "LI":
		tag = "li"
	case "Sect", "Art", "Document", "Root":
		tag = "div"
	case "Span":
		tag = "span"
	}

	fmt.Fprintf(sb, "<%s>", tag)
	for _, kid := range node.Kids {
		renderHTML(kid, mcidMap, sb)
	}
	fmt.Fprintf(sb, "</%s>\n", tag)
}
