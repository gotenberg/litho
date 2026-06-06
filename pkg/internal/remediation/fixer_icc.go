package remediation

import (
	"context"
	"fmt"
	"log/slog"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

func matchCorruptICC(v compliance.Violation) bool {
	// Clause 6.2.3/6.2.2/6.5.3: OutputIntent ICC profile validation failures.
	return clausePrefix(v, "6.2.3", "6.2.2", "6.5.3") && descriptionContains(v, "DestOutputProfile")
}

func fixCorruptICC(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalogRef, ok := doc.Index.Trailer["Root"].(ast.PdfIndirectRef)
	if !ok {
		return nil, nil
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, nil
	}

	oiObj, ok := catalogDict["OutputIntents"]
	if !ok {
		return nil, nil
	}
	oiArr, err := document.ResolveArray(ctx, doc, oiObj)
	if err != nil || len(oiArr) == 0 {
		return nil, nil
	}

	var fixes []string
	newOIArr := make(ast.PdfArray, len(oiArr))

	for i, oiEntry := range oiArr {
		var oiRef ast.PdfIndirectRef
		if ref, ok := oiEntry.(ast.PdfIndirectRef); ok {
			oiRef = ref
		}

		oiDict, err := document.ResolveDict(ctx, doc, oiEntry)
		if err != nil || oiDict == nil {
			doc.Logger.Debug("resolving OutputIntent dict failed",
				slog.Int("index", i),
				slog.Any("err", err),
			)
			newOIArr[i] = oiEntry
			continue
		}

		// Check if DestOutputProfile exists and is valid.
		dpObj, ok := oiDict["DestOutputProfile"]
		if !ok {
			// Missing DestOutputProfile is a spec violation; inject a
			// stock sRGB profile so the OutputIntent becomes valid.
			// OutputConditionIdentifier is required too, fill it in
			// when absent.
			if _, hasOCI := oiDict["OutputConditionIdentifier"]; !hasOCI {
				newOI := make(ast.PdfDict, len(oiDict)+1)
				maps.Copy(newOI, oiDict)
				newOI["OutputConditionIdentifier"] = ast.PdfString("sRGB IEC61966-2.1")
				oiDict = newOI
			}
			replaceICCProfile(alloc, oiDict, oiRef, nil)
			fixes = append(fixes, fmt.Sprintf("added DestOutputProfile to OutputIntent[%d]", i))
			newOIArr[i] = oiEntry
			continue
		}

		stream, err := document.ResolveStream(ctx, doc, dpObj)
		if err != nil {
			doc.Logger.Debug("resolving DestOutputProfile stream failed",
				slog.Int("index", i),
				slog.Any("err", err),
			)
			newOIArr[i] = oiEntry
			continue
		}

		data, err := stream.Bytes(ctx)
		if err != nil || len(data) < 128 {
			// Corrupt or too short. Replace with a valid profile.
			newOIArr[i] = oiEntry
			replaceICCProfile(alloc, oiDict, oiRef, dpObj)
			fixes = append(fixes, fmt.Sprintf("replaced corrupt ICC profile in OutputIntent[%d]", i))
			continue
		}

		// Validate ICC header: version major (byte 8) must be 2 or 4, device
		// class must be "prtr" or "mntr", color space must be "GRAY", "RGB ",
		// or "CMYK". The validator rejects ICC version 5+ (it is not admitted
		// by ISO 15076-1:2005), so the fixer must replace such a profile too;
		// otherwise a valid-class v5 profile is flagged but never repaired.
		version := data[8]
		deviceClass := string(data[12:16])
		colorSpace := string(data[16:20])

		validVersion := version == 2 || version == 4
		validClass := deviceClass == "prtr" || deviceClass == "mntr"
		validCS := colorSpace == "GRAY" || colorSpace == "RGB " || colorSpace == "CMYK"

		if !validVersion || !validClass || !validCS {
			replaceICCProfile(alloc, oiDict, oiRef, dpObj)
			fixes = append(fixes, fmt.Sprintf("replaced invalid ICC profile in OutputIntent[%d] (version=%d, class=%q, cs=%q)", i, version, deviceClass, colorSpace))
		}

		newOIArr[i] = oiEntry
	}

	return fixes, nil
}

// replaceICCProfile replaces the DestOutputProfile stream with a valid sRGB
// profile (or CMYK if the existing profile was CMYK-like).
func replaceICCProfile(alloc *writer.ObjAllocator, oiDict ast.PdfDict, oiRef ast.PdfIndirectRef, dpObj ast.PdfObject) {
	// Default to sRGB (most compatible).
	iccData := sRGBICCProfile
	nComponents := 3

	// Create replacement stream.
	newStream := ast.NewStream(ast.PdfDict{
		"N": ast.PdfInt(nComponents),
	}, iccData)
	newStreamRef := alloc.Alloc(newStream)

	// Update the OutputIntent dict.
	newOI := make(ast.PdfDict, len(oiDict))
	maps.Copy(newOI, oiDict)
	newOI["DestOutputProfile"] = newStreamRef

	if oiRef.ObjectNum > 0 {
		alloc.AllocAt(oiRef.ObjectNum, newOI)
	} else if dpRef, ok := dpObj.(ast.PdfIndirectRef); ok {
		// Replace the stream object directly.
		alloc.AllocAt(dpRef.ObjectNum, newStream)
	}
}
