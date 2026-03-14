package semantics

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/filter"
	"github.com/gotenberg/litho/pkg/models"
)

// Extract3DModels scans a page's annotations for /3D subtype annotations,
// extracts the /3DD (3D Data) stream, and returns the decoded model data.
// If outputDir is non-empty, the models are also saved to disk.
func Extract3DModels(doc *document.Document, page *document.Page, outputDir string) ([]models.Model3D, error) {
	annotsObj, ok := page.Dict["Annots"]
	if !ok {
		return nil, nil
	}

	annotsResolved, err := document.ResolveObject(doc, annotsObj)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve /Annots: %w", err)
	}

	annotsArr, ok := annotsResolved.(ast.PdfArray)
	if !ok {
		return nil, nil
	}

	var results []models.Model3D
	modelIdx := 0

	for _, annotRef := range annotsArr {
		annotResolved, err := document.ResolveObject(doc, annotRef)
		if err != nil {
			continue
		}

		annotDict, ok := annotResolved.(ast.PdfDict)
		if !ok {
			continue
		}

		// Check /Subtype is /3D.
		subtypeObj, ok := annotDict["Subtype"]
		if !ok {
			continue
		}
		subtypeRes, _ := document.ResolveObject(doc, subtypeObj)
		subtypeName, ok := subtypeRes.(ast.PdfName)
		if !ok || subtypeName != "3D" {
			continue
		}

		// Extract the /3DD (3D Data) stream.
		ddObj, ok := annotDict["3DD"]
		if !ok {
			continue
		}

		ddResolved, err := document.ResolveObject(doc, ddObj)
		if err != nil {
			continue
		}

		stream, ok := ddResolved.(ast.PdfStream)
		if !ok {
			continue
		}

		// Determine the 3D subtype: U3D or PRC.
		subtype := determine3DSubtype(doc, stream)

		// Decode the stream data.
		data, err := filter.DecodeStream(stream)
		if err != nil {
			continue
		}

		// Extract a name from the annotation if available.
		name := fmt.Sprintf("model_%d", modelIdx)
		if nmObj, ok := annotDict["NM"]; ok {
			nmRes, _ := document.ResolveObject(doc, nmObj)
			if nmStr, ok := nmRes.(ast.PdfString); ok {
				decoded := document.DecodePdfString(nmStr)
				if decoded != "" {
					name = decoded
				}
			}
		}

		model := models.Model3D{
			Name:    name,
			Subtype: subtype,
			Data:    data,
		}

		// Save to disk if outputDir is specified.
		if outputDir != "" {
			ext := ".u3d"
			if subtype == "PRC" {
				ext = ".prc"
			}

			if err := os.MkdirAll(outputDir, 0o755); err != nil {
				return nil, fmt.Errorf("failed to create output directory: %w", err)
			}

			outPath := filepath.Join(outputDir, name+ext)
			if err := os.WriteFile(outPath, data, 0o600); err == nil {
				model.FilePath = outPath
			}
		}

		results = append(results, model)
		modelIdx++
	}

	if len(results) == 0 {
		return nil, nil
	}
	return results, nil
}

// determine3DSubtype checks the stream dictionary to determine whether the
// 3D data is U3D or PRC format.
func determine3DSubtype(doc *document.Document, stream ast.PdfStream) string {
	// Check /Subtype on the 3D stream dictionary.
	if stObj, ok := stream.Dict["Subtype"]; ok {
		stRes, _ := document.ResolveObject(doc, stObj)
		if name, ok := stRes.(ast.PdfName); ok {
			switch name {
			case "U3D":
				return "U3D"
			case "PRC":
				return "PRC"
			}
		}
	}

	// Check /Type as a fallback (some producers use /Type instead of /Subtype).
	if typeObj, ok := stream.Dict["Type"]; ok {
		typeRes, _ := document.ResolveObject(doc, typeObj)
		if name, ok := typeRes.(ast.PdfName); ok {
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
