package compliance

import (
	"context"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// checkEncryptionPermissions validates PDF/UA-1 §7.16: if encrypted, the
// /Encrypt dictionary must contain a /P key whose 10th bit is true
// (accessibility content extraction permitted).
func checkEncryptionPermissions(doc *document.Document, profile string) []Violation {
	if profile != "PDF/UA-1" {
		return nil
	}

	if !doc.IsEncrypted {
		return nil
	}

	trailer := doc.Index.Trailer
	encObj, ok := trailer["Encrypt"]
	if !ok {
		return fail(Rule_14289_1_7_16_t01, "file is encrypted but no /Encrypt dictionary in trailer")
	}

	// Resolve the Encrypt dict (may be an indirect ref).
	var encDict ast.PdfDict
	switch v := encObj.(type) {
	case ast.PdfDict:
		encDict = v
	case ast.PdfIndirectRef:
		obj, err := doc.Resolve(context.Background(), v)
		if err != nil {
			return fail(Rule_14289_1_7_16_t01, "cannot resolve /Encrypt dictionary")
		}
		d, ok := obj.(ast.PdfDict)
		if !ok {
			return fail(Rule_14289_1_7_16_t01, "resolved /Encrypt is not a dictionary")
		}
		encDict = d
	default:
		return fail(Rule_14289_1_7_16_t01, "/Encrypt is not a dictionary")
	}

	pObj, hasP := encDict["P"]
	if !hasP {
		return fail(Rule_14289_1_7_16_t01, "encrypted file has no /P key in /Encrypt dictionary")
	}

	pVal, ok := pObj.(ast.PdfInt)
	if !ok {
		return fail(Rule_14289_1_7_16_t01, "/P is not an integer")
	}

	// Bit 10 (1-indexed) = bit 9 (0-indexed) = 0x200.
	const accessibilityBit = 1 << 9
	if int(pVal)&accessibilityBit == 0 {
		return failf(Rule_14289_1_7_16_t01,
			"10th bit of /P (%d) is false, accessibility content extraction not permitted", int(pVal))
	}

	return nil
}

// markerKind tracks whether a BMC/BDC span is Artifact or tagged content.
type markerKind int

const (
	markerTagged     markerKind = iota // BMC/BDC with a structure tag, no MCID
	markerTaggedMCID                   // BDC with MCID (real structure element reference)
	markerArtifact                     // BMC /Artifact or BDC /Artifact
)

// checkContentMarking validates PDF/UA-1 §7.1:
//   - t1: Artifact content must not appear inside tagged content
//   - t2: Tagged content must not appear inside Artifact
//   - t3: All painting operators must be inside a marked content span
func checkContentMarking(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if !IsPDFUA(profile) {
		return nil
	}

	pages := collectPages(ctx, doc, catalog)
	var results []Violation

	for _, page := range pages {
		if ctx.Err() != nil {
			return results
		}

		data, err := resolvePageContentBytes(ctx, doc, page)
		if err != nil || len(data) == 0 {
			continue
		}
		ops, err := content.ParseStreamData(ctx, data)
		if err != nil {
			doc.Logger.Debug("compliance.accessibility: cannot parse content stream", "error", err)
			continue
		}

		// Track BMC/BDC/EMC nesting with marker kind (Artifact vs tagged).
		var markerStack []markerKind
		hasUntaggedPainting := false
		allUntaggedIsTaggedDo := true
		reportedT1 := false
		reportedT2 := false
		resources := resolveResources(ctx, doc, page)

		classifyMarker := func(op content.Operation) markerKind {
			if len(op.Operands) < 1 {
				return markerTagged
			}
			tag, ok := op.Operands[0].(ast.PdfName)
			if !ok {
				return markerTagged
			}
			if tag == "Artifact" {
				return markerArtifact
			}
			// Check for MCID in the properties dict (second operand).
			if len(op.Operands) >= 2 {
				if d, ok := op.Operands[1].(ast.PdfDict); ok {
					if _, hasMCID := d["MCID"]; hasMCID {
						return markerTaggedMCID
					}
				}
			}
			return markerTagged
		}

		inArtifact := func() bool {
			return slices.Contains(markerStack, markerArtifact)
		}
		inTaggedMCID := func() bool {
			return slices.Contains(markerStack, markerTaggedMCID)
		}

		for _, op := range ops {
			switch op.Operator {
			case "BMC", "BDC":
				kind := classifyMarker(op)
				// t1/t2 (Artifact↔tagged nesting) are PDF/UA-1 §7.1 concepts.
				// veraPDF's PDFUA-2 profile has no analogue (its §8.2.2 covers
				// only unmarked content, handled as t3 below), so firing the
				// UA-1 ids under PDF/UA-2 is a litho-over (verified live on
				// 7.1-t01-fail-a / 7.1-t02-fail-a under PDF/UA-2: veraPDF reports
				// no artifact-nesting rule).
				if profile == "PDF/UA-1" {
					// t1: Artifact inside tagged content with MCID.
					if kind == markerArtifact && !reportedT1 && inTaggedMCID() {
						results = append(results, failf(Rule_14289_1_7_1_t01,
							"content marked as Artifact inside tagged content")...)
						reportedT1 = true
					}
					// t2: Tagged content with MCID inside Artifact.
					if kind == markerTaggedMCID && !reportedT2 && inArtifact() {
						results = append(results, failf(Rule_14289_1_7_1_t02,
							"tagged content inside content marked as Artifact")...)
						reportedT2 = true
					}
				}
				markerStack = append(markerStack, kind)
			case "EMC":
				if len(markerStack) > 0 {
					markerStack = markerStack[:len(markerStack)-1]
				}
			case "Do":
				if len(markerStack) == 0 {
					hasUntaggedPainting = true
					if !doTargetIsTagged(ctx, doc, resources, op) {
						allUntaggedIsTaggedDo = false
					}
				}
			case "f", "F", "f*", "S", "s", "B", "B*", "b", "b*",
				"Tj", "TJ", "'", "\"", "sh":
				if len(markerStack) == 0 {
					hasUntaggedPainting = true
					allUntaggedIsTaggedDo = false
				}
			}
		}

		// t3: painting operators outside any marked content span.
		if hasUntaggedPainting && !allUntaggedIsTaggedDo {
			rule := Rule_14289_1_7_1_t03
			if profile == "PDF/UA-2" {
				rule = Rule_14289_2_8_2_2_t01
			}
			results = append(results, failf(rule,
				"page content not marked as Artifact or tagged")...)
			return results
		}
	}
	return results
}

// doTargetIsTagged checks if a Do operator's target XObject is a Form XObject
// with its own structure tagging (StructParents or BMC/BDC markers).
func doTargetIsTagged(ctx context.Context, doc *document.Document, resources ast.PdfDict, op content.Operation) bool {
	if resources == nil || len(op.Operands) < 1 {
		return false
	}
	name, ok := op.Operands[0].(ast.PdfName)
	if !ok {
		return false
	}
	xoDict, _ := document.ResolveDict(ctx, doc, resources["XObject"])
	if xoDict == nil {
		return false
	}
	stream, err := document.ResolveStream(ctx, doc, xoDict[name])
	if err != nil || stream.Dict == nil {
		return false
	}
	st, _ := document.ResolveName(ctx, doc, stream.Dict["Subtype"])
	if st != "Form" {
		return false
	}
	// Form XObject with StructParents is tagged via structure tree.
	if _, hasSP := stream.Dict["StructParents"]; hasSP {
		return true
	}
	if _, hasSP := stream.Dict["StructParent"]; hasSP {
		return true
	}
	return false
}

func ocConfigNameRule(profile string) Rule {
	if profile == "PDF/UA-2" {
		return Rule_14289_2_8_7_t01
	}
	return Rule_14289_1_7_10_t01
}
