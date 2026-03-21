package semantics

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// Extract3DModels scans a page's annotations for /3D subtype annotations and
// returns metadata with a lazy Open accessor for each 3D data stream. The
// stream data is not decoded until the caller invokes Open.
func Extract3DModels(ctx context.Context, doc *document.Document, page *document.Page) ([]models.Model3D, error) {
	annotsObj, ok := page.Dict["Annots"]
	if !ok {
		return nil, nil
	}

	annotsArr, err := document.ResolveArray(ctx, doc, annotsObj)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve /Annots: %w", err)
	}

	if annotsArr == nil {
		return nil, nil
	}

	var results []models.Model3D
	modelIdx := 0

	for _, annotRef := range annotsArr {
		annotDict, err := document.ResolveDict(ctx, doc, annotRef)
		if err != nil {
			continue
		}

		if annotDict == nil {
			continue
		}

		// Check /Subtype is /3D.
		subtypeObj, ok := annotDict["Subtype"]
		if !ok {
			continue
		}
		subtypeName, _ := document.ResolveName(ctx, doc, subtypeObj)
		if subtypeName != "3D" {
			continue
		}

		// Extract the /3DD (3D Data) stream.
		ddObj, ok := annotDict["3DD"]
		if !ok {
			continue
		}

		stream, err := document.ResolveStream(ctx, doc, ddObj)
		if err != nil {
			continue
		}

		if stream.Dict == nil {
			continue
		}

		// Determine the 3D subtype: U3D or PRC.
		subtype := determine3DSubtype(ctx, doc, stream)

		// Extract a name from the annotation if available.
		name := fmt.Sprintf("model_%d", modelIdx)
		if nmObj, ok := annotDict["NM"]; ok {
			nmStr, _ := document.ResolveString(ctx, doc, nmObj)
			if nmStr != "" {
				decoded := document.DecodePdfString(nmStr)
				if decoded != "" {
					name = decoded
				}
			}
		}

		results = append(results, models.Model3D{
			Name:    name,
			Subtype: subtype,
			Open:    openStreamFunc(stream),
		})
		modelIdx++
	}

	if len(results) == 0 {
		return nil, nil
	}
	return results, nil
}

// determine3DSubtype checks the stream dictionary to determine whether the
// 3D data is U3D or PRC format.
func determine3DSubtype(ctx context.Context, doc *document.Document, stream ast.PdfStream) string {
	// Check /Subtype on the 3D stream dictionary.
	if stObj, ok := stream.Dict["Subtype"]; ok {
		name, _ := document.ResolveName(ctx, doc, stObj)
		switch name {
		case "U3D":
			return "U3D"
		case "PRC":
			return "PRC"
		}
	}

	// Check /Type as a fallback (some producers use /Type instead of /Subtype).
	if typeObj, ok := stream.Dict["Type"]; ok {
		name, _ := document.ResolveName(ctx, doc, typeObj)
		switch name {
		case "U3D":
			return "U3D"
		case "PRC":
			return "PRC"
		case "3D":
			// Generic /Type /3D — try to detect from magic bytes.
			return detect3DFromMagic(stream.Data)
		}
	}

	// Last resort: check magic bytes of the raw stream data.
	return detect3DFromMagic(stream.Data)
}

// detect3DFromMagic inspects the first few bytes of 3D data to determine format.
func detect3DFromMagic(data []byte) string {
	if len(data) < 4 {
		return "U3D"
	}

	// PRC magic: 0x50 0x52 0x43 (ASCII "PRC")
	if data[0] == 'P' && data[1] == 'R' && data[2] == 'C' {
		return "PRC"
	}

	// U3D is the default/legacy format.
	return "U3D"
}
