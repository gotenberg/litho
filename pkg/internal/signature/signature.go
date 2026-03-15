package signature

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// ExtractSignatures locates signature fields, extracts the PKCS#7 payload,
// and computes the document hash based on the ByteRange.
func ExtractSignatures(ctx context.Context, doc *document.Document) ([]models.Signature, error) {
	catalog, err := document.GetCatalog(ctx, doc)
	if err != nil {
		return nil, err
	}

	acroFormObj, ok := catalog.Dict["AcroForm"]
	if !ok {
		return nil, nil // No forms/signatures
	}

	acroFormResolved, err := document.ResolveObject(ctx, doc, acroFormObj)
	if err != nil {
		return nil, err
	}

	acroFormDict, ok := acroFormResolved.(ast.PdfDict)
	if !ok {
		return nil, nil
	}

	fieldsObj, ok := acroFormDict["Fields"]
	if !ok {
		return nil, nil
	}

	fieldsResolved, err := document.ResolveObject(ctx, doc, fieldsObj)
	if err != nil {
		return nil, err
	}

	fieldsArr, ok := fieldsResolved.(ast.PdfArray)
	if !ok {
		return nil, nil
	}

	var signatures []models.Signature
	extractSigsRecursive(ctx, doc, fieldsArr, "", &signatures)

	return signatures, nil
}

func extractSigsRecursive(ctx context.Context, doc *document.Document, fields ast.PdfArray, parentName string, sigs *[]models.Signature) {
	for _, fieldObj := range fields {
		fieldResolved, err := document.ResolveObject(ctx, doc, fieldObj)
		if err != nil {
			continue
		}

		fieldDict, ok := fieldResolved.(ast.PdfDict)
		if !ok {
			continue
		}

		// Extract partial name
		partialName := ""
		if tObj, ok := fieldDict["T"]; ok {
			tRes, _ := document.ResolveObject(ctx, doc, tObj)
			if tStr, ok := tRes.(ast.PdfString); ok {
				partialName = document.DecodePdfString(tStr)
			}
		}

		fullName := parentName
		if partialName != "" {
			if fullName != "" {
				fullName = fullName + "." + partialName
			} else {
				fullName = partialName
			}
		}

		// Field Type (/FT)
		if ftObj, ok := fieldDict["FT"]; ok {
			ftRes, _ := document.ResolveObject(ctx, doc, ftObj)
			if ftName, ok := ftRes.(ast.PdfName); ok && ftName == "Sig" {
				if vObj, ok := fieldDict["V"]; ok {
					vRes, _ := document.ResolveObject(ctx, doc, vObj)
					if vDict, ok := vRes.(ast.PdfDict); ok {
						sig := models.Signature{FieldName: fullName}

						// ByteRange
						if brObj, ok := vDict["ByteRange"]; ok {
							brRes, _ := document.ResolveObject(ctx, doc, brObj)
							if brArr, ok := brRes.(ast.PdfArray); ok && len(brArr) == 4 {
								for i := range 4 {
									if num, ok := brArr[i].(ast.PdfInt); ok {
										sig.ByteRange[i] = int64(num)
									}
								}
							}
						}

						// Contents (Hex string PKCS#7)
						if cObj, ok := vDict["Contents"]; ok {
							cRes, _ := document.ResolveObject(ctx, doc, cObj)
							if cStr, ok := cRes.(ast.PdfString); ok {
								sig.Contents = string(cStr)
							}
						}

						// /M signing time
						if mObj, ok := vDict["M"]; ok {
							mRes, _ := document.ResolveObject(ctx, doc, mObj)
							if mStr, ok := mRes.(ast.PdfString); ok {
								sig.SigningTime = document.DecodePdfString(mStr)
							}
						}

						// Extract MDP policy from /Reference array.
						sig.MDPPolicy = extractMDPPolicy(ctx, doc, vDict)

						// Calculate hash of the physical bytes defined by ByteRange
						if sig.ByteRange[1] > 0 && sig.ByteRange[3] > 0 {
							chunk1, err1 := doc.ReadFileBytes(sig.ByteRange[0], sig.ByteRange[1])
							chunk2, err2 := doc.ReadFileBytes(sig.ByteRange[2], sig.ByteRange[3])

							if err1 == nil && err2 == nil {
								h := sha256.New()
								h.Write(chunk1)
								h.Write(chunk2)
								sig.DocumentHash = hex.EncodeToString(h.Sum(nil))
							}
						}

						*sigs = append(*sigs, sig)
					}
				}
			}
		}

		// Traverse Kids
		if kidsObj, ok := fieldDict["Kids"]; ok {
			kidsRes, _ := document.ResolveObject(ctx, doc, kidsObj)
			if kidsArr, ok := kidsRes.(ast.PdfArray); ok {
				extractSigsRecursive(ctx, doc, kidsArr, fullName, sigs)
			}
		}
	}
}
