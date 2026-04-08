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

func matchOutputIntent(v compliance.Violation) bool {
	return descriptionContains(v, "OutputIntent") ||
		(descriptionContains(v, "DeviceRGB") && descriptionContains(v, "without")) ||
		(descriptionContains(v, "DeviceCMYK") && descriptionContains(v, "without")) ||
		(descriptionContains(v, "DeviceGray") && descriptionContains(v, "without"))
}

func fixOutputIntent(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, profile string) ([]string, error) {
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

	// Create the ICC profile stream.
	iccStream := ast.NewStream(ast.PdfDict{
		"N": ast.PdfInt(3), // RGB = 3 components
	}, sRGBICCProfile)
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
		"OutputConditionIdentifier": ast.PdfString("sRGB IEC61966-2.1"),
		"DestOutputProfile":         iccRef,
	}
	oiRef := alloc.Alloc(outputIntent)

	// Add to catalog.
	newCatalog := make(ast.PdfDict, len(catalogDict)+1)
	maps.Copy(newCatalog, catalogDict)
	newCatalog["OutputIntents"] = ast.PdfArray{oiRef}
	alloc.AllocAt(catalogRef.ObjectNum, newCatalog)

	return []string{"added sRGB OutputIntent to catalog"}, nil
}
