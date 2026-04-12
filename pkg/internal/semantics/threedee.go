package semantics

import (
	"context"
	"fmt"
	"log/slog"

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
		subtypeName, err := document.ResolveName(ctx, doc, subtypeObj)
		if err != nil {
			doc.Logger.Debug("resolving 3D annotation subtype", slog.Any("err", err))
		}
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
		subtype, err := determine3DSubtype(ctx, doc, stream)
		if err != nil {
			continue
		}

		// Extract a name from the annotation if available.
		name := fmt.Sprintf("model_%d", modelIdx)
		if nmObj, ok := annotDict["NM"]; ok {
			nmStr, err := document.ResolveString(ctx, doc, nmObj)
			if err != nil {
				doc.Logger.Debug("resolving 3D annotation name", slog.Any("err", err))
			}
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
			Open:    openStreamFunc(doc, stream),
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
func determine3DSubtype(ctx context.Context, doc *document.Document, stream ast.PdfStream) (string, error) {
	// Check /Subtype on the 3D stream dictionary.
	if stObj, ok := stream.Dict["Subtype"]; ok {
		name, err := document.ResolveName(ctx, doc, stObj)
		if err != nil {
			doc.Logger.Debug("resolving 3D stream subtype", slog.Any("err", err))
		}
		switch name {
		case "U3D":
			return "U3D", nil
		case "PRC":
			return "PRC", nil
		}
	}

	// Check /Type as a fallback (some producers use /Type instead of /Subtype).
	if typeObj, ok := stream.Dict["Type"]; ok {
		name, err := document.ResolveName(ctx, doc, typeObj)
		if err != nil {
			doc.Logger.Debug("resolving 3D stream type", slog.Any("err", err))
		}
		switch name {
		case "U3D":
			return "U3D", nil
		case "PRC":
			return "PRC", nil
		case "3D":
			// Generic /Type /3D. Try to detect from magic bytes.
			data, err := stream.Bytes()
			if err != nil {
				return "", err
			}
			return detect3DFromMagic(data), nil
		}
	}

	// Last resort: check magic bytes of the raw stream data.
	data, err := stream.Bytes()
	if err != nil {
		return "", err
	}
	return detect3DFromMagic(data), nil
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
