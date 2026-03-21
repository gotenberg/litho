package signature

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"

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

	acroFormDict, err := document.ResolveDict(ctx, doc, acroFormObj)
	if err != nil {
		return nil, err
	}

	if acroFormDict == nil {
		return nil, nil
	}

	fieldsObj, ok := acroFormDict["Fields"]
	if !ok {
		return nil, nil
	}

	fieldsArr, err := document.ResolveArray(ctx, doc, fieldsObj)
	if err != nil {
		return nil, err
	}

	if fieldsArr == nil {
		return nil, nil
	}

	var signatures []models.Signature
	extractSigsRecursive(ctx, doc, fieldsArr, "", &signatures)

	return signatures, nil
}

func extractSigsRecursive(ctx context.Context, doc *document.Document, fields ast.PdfArray, parentName string, sigs *[]models.Signature) {
	for _, fieldObj := range fields {
		fieldDict, err := document.ResolveDict(ctx, doc, fieldObj)
		if err != nil {
			continue
		}

		if fieldDict == nil {
			continue
		}

		// Extract partial name
		partialName := ""
		if tObj, ok := fieldDict["T"]; ok {
			tStr, err := document.ResolveString(ctx, doc, tObj)
			if err != nil {
				doc.Logger.Debug("resolving signature field name", slog.Any("err", err))
			}
			if tStr != "" {
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
			ftName, err := document.ResolveName(ctx, doc, ftObj)
			if err != nil {
				doc.Logger.Debug("resolving field type", slog.Any("err", err))
			}
			if ftName == "Sig" {
				if vObj, ok := fieldDict["V"]; ok {
					vDict, err := document.ResolveDict(ctx, doc, vObj)
					if err != nil {
						doc.Logger.Debug("resolving signature value dict", slog.Any("err", err))
					}
					if vDict != nil {
						sig := models.Signature{FieldName: fullName}

						// ByteRange
						if brObj, ok := vDict["ByteRange"]; ok {
							brArr, err := document.ResolveArray(ctx, doc, brObj)
							if err != nil {
								doc.Logger.Debug("resolving signature byte range", slog.Any("err", err))
							}
							if len(brArr) == 4 {
								for i := range 4 {
									if num, ok := brArr[i].(ast.PdfInt); ok {
										sig.ByteRange[i] = int64(num)
									}
								}
							}
						}

						// Contents (Hex string PKCS#7)
						if cObj, ok := vDict["Contents"]; ok {
							cStr, err := document.ResolveString(ctx, doc, cObj)
							if err != nil {
								doc.Logger.Debug("resolving signature contents", slog.Any("err", err))
							}
							if cStr != "" {
								sig.Contents = string(cStr)
							}
						}

						// /M signing time
						if mObj, ok := vDict["M"]; ok {
							mStr, err := document.ResolveString(ctx, doc, mObj)
							if err != nil {
								doc.Logger.Debug("resolving signature signing time", slog.Any("err", err))
							}
							if mStr != "" {
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
			kidsArr, err := document.ResolveArray(ctx, doc, kidsObj)
			if err != nil {
				doc.Logger.Debug("resolving signature field kids", slog.Any("err", err))
			}
			if kidsArr != nil {
				extractSigsRecursive(ctx, doc, kidsArr, fullName, sigs)
			}
		}
	}
}
