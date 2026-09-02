package signature

import (
	"context"
	"encoding/hex"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// maxFieldTreeDepth bounds the walk over a form field's /Kids. It is an
// anti-DoS guard, not spec-mandated: ISO 32000-1, 12.7.3 describes a field
// hierarchy only a few levels deep, and a crafted file whose /Kids points back
// at an ancestor would otherwise recurse until the stack is exhausted.
const maxFieldTreeDepth = 64

// ExtractSignatures locates signature fields, extracts the PKCS#7 payload,
// and computes the document hash based on the ByteRange.
//
// Fields are found in two places. The interactive form's /Fields array holds
// them for a conforming document (ISO 32000-1, 12.7.2), and the pages hold the
// widget annotations that present them (12.5.6.19). A producer that signs a
// document without registering the field in /AcroForm leaves the signature
// reachable only from the page, so both are walked.
func ExtractSignatures(ctx context.Context, doc *document.Document) ([]models.Signature, error) {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, err
	}

	var signatures []models.Signature
	seen := make(map[int]bool)

	if acroFormObj, ok := catalog.Dict["AcroForm"]; ok {
		acroFormDict, err := document.ResolveDict(ctx, doc, acroFormObj)
		if err != nil {
			return nil, err
		}
		if acroFormDict != nil {
			if fieldsObj, ok := acroFormDict["Fields"]; ok {
				fieldsArr, err := document.ResolveArray(ctx, doc, fieldsObj)
				if err != nil {
					return nil, err
				}
				if fieldsArr != nil {
					markSignatureValues(ctx, doc, fieldsArr, seen, 0)
					extractSigsRecursive(ctx, doc, fieldsArr, "", &signatures, 0)
				}
			}
		}
	}

	appendAnnotationSignatures(ctx, doc, catalog, seen, &signatures)

	return signatures, nil
}

// markSignatureValues records the object number of every /V signature
// dictionary reachable from fields, so the page walk can tell an unregistered
// signature from one it has already reported.
func markSignatureValues(ctx context.Context, doc *document.Document, fields ast.PdfArray, seen map[int]bool, depth int) {
	if depth > maxFieldTreeDepth {
		return
	}
	for _, fieldObj := range fields {
		fieldDict, err := document.ResolveDict(ctx, doc, fieldObj)
		if err != nil || fieldDict == nil {
			continue
		}
		if ref, ok := fieldDict["V"].(ast.PdfIndirectRef); ok {
			seen[ref.ObjectNum] = true
		}
		if kidsObj, ok := fieldDict["Kids"]; ok {
			kidsArr, err := document.ResolveArray(ctx, doc, kidsObj)
			if err == nil && kidsArr != nil {
				markSignatureValues(ctx, doc, kidsArr, seen, depth+1)
			}
		}
	}
}

// appendAnnotationSignatures reports the signature fields a page carries that
// the interactive form does not list. The annotations are handed to the same
// field walker, so a signature found this way is described exactly as a
// registered one would be.
func appendAnnotationSignatures(
	ctx context.Context,
	doc *document.Document,
	catalog *document.Catalog,
	seen map[int]bool,
	sigs *[]models.Signature,
) {
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		doc.Logger.Debug("resolving pages for signature annotations", slog.Any("err", err))
		return
	}

	var unregistered ast.PdfArray
	for _, page := range pages {
		if ctx.Err() != nil {
			return
		}
		annotsObj, ok := page.Dict["Annots"]
		if !ok {
			continue
		}
		annotsArr, err := document.ResolveArray(ctx, doc, annotsObj)
		if err != nil {
			doc.Logger.Debug("resolving /Annots for signatures", slog.Any("err", err))
			continue
		}
		for _, annotObj := range annotsArr {
			annotDict, err := document.ResolveDict(ctx, doc, annotObj)
			if err != nil || annotDict == nil {
				continue
			}
			ftName, err := document.ResolveName(ctx, doc, annotDict["FT"])
			if err != nil || ftName != "Sig" {
				continue
			}
			ref, ok := annotDict["V"].(ast.PdfIndirectRef)
			if !ok || seen[ref.ObjectNum] {
				continue
			}
			seen[ref.ObjectNum] = true
			unregistered = append(unregistered, annotObj)
		}
	}

	if len(unregistered) > 0 {
		extractSigsRecursive(ctx, doc, unregistered, "", sigs, 0)
	}
}

func extractSigsRecursive(ctx context.Context, doc *document.Document, fields ast.PdfArray, parentName string, sigs *[]models.Signature, depth int) {
	if depth > maxFieldTreeDepth {
		return
	}
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
							chunk1, err1 := doc.ReadFileBytes(ctx, sig.ByteRange[0], sig.ByteRange[1])
							chunk2, err2 := doc.ReadFileBytes(ctx, sig.ByteRange[2], sig.ByteRange[3])

							if err1 == nil && err2 == nil {
								// Use the digest algorithm the signature
								// actually declares rather than assuming
								// SHA-256, so DocumentHash is meaningful for
								// SHA-384/512 (and legacy) signatures.
								h := cmsDigestHash([]byte(sig.Contents)).New()
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
				extractSigsRecursive(ctx, doc, kidsArr, fullName, sigs, depth+1)
			}
		}
	}
}
