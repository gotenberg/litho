package remediation

import (
	"context"
	_ "embed"
	"fmt"
	"maps"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

//go:embed srgb.icc
var sRGBICCProfile []byte

//go:embed cmyk.icc
var cmykICCProfile []byte

func matchOutputIntent(v compliance.Violation) bool {
	return clausePrefix(v, "6.2.3.2", "6.2.3.3", "6.2.4.3") // device CS without OutputIntent
}

func fixOutputIntent(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, violations []compliance.Violation, profile string) ([]string, error) {
	catalogRef, ok := doc.Index.Trailer["Root"].(ast.PdfIndirectRef)
	if !ok {
		return nil, fmt.Errorf("trailer missing /Root")
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, fmt.Errorf("resolving catalog")
	}

	// Check if OutputIntents already exists with a valid entry.
	if oiObj, ok := catalogDict["OutputIntents"]; ok {
		if arr, err := document.ResolveArray(ctx, doc, oiObj); err == nil && len(arr) > 0 {
			return nil, nil // already has OutputIntents
		}
	}

	// Detect whether the document uses CMYK based on violations.
	needsCMYK := false
	for _, v := range violations {
		if descriptionContains(v, "DeviceCMYK") {
			needsCMYK = true
			break
		}
	}

	// Select ICC profile and parameters.
	var iccData []byte
	var nComponents int
	var conditionID string
	if needsCMYK {
		iccData = cmykICCProfile
		nComponents = 4
		conditionID = "US Web Coated (SWOP) v2"
	} else {
		iccData = sRGBICCProfile
		nComponents = 3
		conditionID = "sRGB IEC61966-2.1"
	}

	// Create the ICC profile stream.
	iccStream := ast.NewStream(ast.PdfDict{
		"N": ast.PdfInt(nComponents),
	}, iccData)
	iccRef := alloc.Alloc(iccStream)

	// Determine the /S value based on profile.
	sValue := "GTS_PDFA1"
	if strings.HasPrefix(profile, "PDF/A-4") {
		sValue = "GTS_PDFA"
	}

	// Create the OutputIntent dict.
	outputIntent := ast.PdfDict{
		"Type":                      ast.PdfName("OutputIntent"),
		"S":                         ast.PdfName(sValue),
		"OutputConditionIdentifier": ast.PdfString(conditionID),
		"DestOutputProfile":         iccRef,
	}
	oiRef := alloc.Alloc(outputIntent)

	// Add to catalog.
	newCatalog := make(ast.PdfDict, len(catalogDict)+1)
	maps.Copy(newCatalog, catalogDict)
	newCatalog["OutputIntents"] = ast.PdfArray{oiRef}
	alloc.AllocAt(catalogRef.ObjectNum, newCatalog)

	if needsCMYK {
		return []string{"added CMYK OutputIntent to catalog"}, nil
	}
	return []string{"added sRGB OutputIntent to catalog"}, nil
}
