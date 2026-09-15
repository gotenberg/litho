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
func checkEncryptionPermissions(ctx context.Context, doc *document.Document, profile string) []Violation {
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
		obj, err := doc.Resolve(ctx, v)
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

	// Inherited resources from the Pages tree root: a page that declares no
	// /Resources of its own inherits them (ISO 32000-1 §7.7.3.4), so a Do whose
	// XObject is defined only on the Pages node must still resolve.
	pagesDict, _ := document.ResolveDict(ctx, doc, catalog["Pages"])
	var inheritedRes ast.PdfDict
	if pagesDict != nil {
		inheritedRes = resolveResources(ctx, doc, pagesDict)
	}

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
		// A page with no own /Resources inherits the Pages-node resources; a page
		// with its own (even empty) /Resources does not inherit.
		resources := resolveResources(ctx, doc, page)
		if resources == nil {
			resources = inheritedRes
		}

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
			// Check for an integer MCID in the properties dict (second operand):
			// any other /MCID tags nothing ([mcidValue]).
			if len(op.Operands) >= 2 {
				if d, ok := op.Operands[1].(ast.PdfDict); ok {
					if _, isMCID := mcidValue(ctx, doc, d["MCID"]); isMCID {
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
		// An /MCID tags content only through the structure element that owns it:
		// veraPDF (GFSEGroupedContent.isTaggedContent) walks that element's parent
		// chain and calls the content tagged only on reaching the StructTreeRoot.
		// With no structure tree there is no owner, so MCID-marked content is
		// untagged. pdfjs bug1019475_1.pdf wraps its text in /H1 <</MCID 0>> BDC
		// under a catalog with no StructTreeRoot, and veraPDF reports it.
		structTree, _ := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
		// An /MCID the ParentTree maps tags its content only when the structure
		// element it names reaches the structure tree root through /P. veraPDF
		// climbs the /P chain of the innermost MCID's owner and calls the content
		// tagged only on reaching a /Type /StructTreeRoot node, so an owner with no
		// /P, a non-dictionary /P or a /P loop leaves its content untagged; ISO
		// 32000-1, 14.7.2, Table 355 makes /P required. Measured against veraPDF on
		// each of those shapes. An MCID the ParentTree does not map is
		// [checkOrphanMCIDs]'s business, and stays tagged here.
		var mcidReach map[int]bool
		if structTree != nil {
			mcidReach = parentTreeMCIDReach(ctx, doc, structTree, page)
		}
		// mcidStack runs parallel to markerStack and holds each marker's MCID, or
		// -1 for a marker that carries none.
		var mcidStack []int
		innermostMCIDTagged := func() bool {
			for i := len(markerStack) - 1; i >= 0; i-- {
				if markerStack[i] != markerTaggedMCID {
					continue
				}
				reach, mapped := mcidReach[mcidStack[i]]
				return !mapped || reach
			}
			return false
		}
		// untaggedScope reports whether painting at the current marker depth is
		// real content that is neither an Artifact nor tagged. Per ISO 14289-1
		// §7.1 / -2 §8.2.2 (veraPDF SESimpleContentItem isTaggedContent), content
		// is tagged only when an enclosing BDC carries an /MCID whose owner
		// reaches the root: a marked-content sequence with a structure tag but no
		// /MCID (or a bare BMC) does not tag its content, so painting inside one
		// with no such MCID-bearing ancestor and no Artifact ancestor is untagged
		// real content.
		untaggedScope := func() bool {
			return (structTree == nil || !innermostMCIDTagged()) && !inArtifact()
		}
		// flagTaggedInArtifact reports §7.1 t2 when real content is painted while
		// marked BOTH as tagged (an enclosing MCID) AND as an Artifact. veraPDF
		// reports such a contradictory content item as tagged-content-inside-an-
		// artifact even when the Artifact sequence is nested inside the tagged one
		// (so no tagged BDC opens inside the artifact and the BDC-open check above
		// does not fire). PDF/UA-1 only; reported once per page.
		flagTaggedInArtifact := func() {
			if profile == "PDF/UA-1" && !reportedT2 && inTaggedMCID() && inArtifact() {
				results = append(results, failf(Rule_14289_1_7_1_t02,
					"tagged content inside content marked as Artifact")...)
				reportedT2 = true
			}
		}

		for _, op := range ops {
			switch op.Operator {
			case "BMC", "BDC":
				kind := classifyMarker(op)
				// t1/t2 (Artifact-tagged nesting) are PDF/UA-1 §7.1 concepts.
				// PDF/UA-2 has no equivalent rule: its §8.2.2 covers only
				// unmarked content, which litho reports as t3 below. Firing the
				// UA-1 ids under PDF/UA-2 would assert a requirement that part 2
				// does not impose, so the t1/t2 checks stay gated to UA-1.
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
					// An /Artifact sequence that carries an /MCID the ParentTree
					// maps to an element reaching the root is itself tagged
					// content (veraPDF isTaggedContent climbs the MCID's owner),
					// so it is both an Artifact inside tagged content and tagged
					// content inside an Artifact. govdocs thread_000_000632.pdf
					// wraps an image in /Artifact <</MCID 31>> BDC that its /S
					// /Artifact element owns, and veraPDF reports t1 and t2; with
					// the MCID left out of the ParentTree it reports neither.
					// Measured with an owner of /S /Artifact and of /S /P.
					if kind == markerArtifact && structTree != nil {
						if mcid := inlineBDCMCID(ctx, doc, op); mcid >= 0 {
							if reach, mapped := mcidReach[mcid]; mapped && reach {
								if !reportedT1 {
									results = append(results, failf(Rule_14289_1_7_1_t01,
										"content marked as Artifact carries an MCID its structure element tags")...)
									reportedT1 = true
								}
								if !reportedT2 {
									results = append(results, failf(Rule_14289_1_7_1_t02,
										"tagged content marked as Artifact")...)
									reportedT2 = true
								}
							}
						}
					}
				}
				markerStack = append(markerStack, kind)
				mcidStack = append(mcidStack, inlineBDCMCID(ctx, doc, op))
			case "EMC":
				if len(markerStack) > 0 {
					markerStack = markerStack[:len(markerStack)-1]
					mcidStack = mcidStack[:len(mcidStack)-1]
				}
			case "Do":
				flagTaggedInArtifact()
				// A Do whose XObject is absent from the page's effective
				// resources (own or inherited) paints nothing (the operator is a
				// no-op per ISO 32000-1 §8.10.1), so it is not untagged real
				// content and veraPDF does not flag it.
				if untaggedScope() && doTargetResolves(ctx, doc, resources, op) {
					hasUntaggedPainting = true
					if !doTargetIsTagged(ctx, doc, resources, op) {
						allUntaggedIsTaggedDo = false
					}
				}
			// BI is an inline image (ISO 32000-1, 8.9.7), which paints an image
			// exactly as a Do naming an image XObject does; veraPDF builds a
			// content item for it like any other.
			case "f", "F", "f*", "S", "s", "B", "B*", "b", "b*",
				"Tj", "TJ", "'", "\"", "sh", "BI":
				flagTaggedInArtifact()
				if untaggedScope() {
					hasUntaggedPainting = true
					allUntaggedIsTaggedDo = false
				}
			}
		}

		// t3: real content that is not marked as Artifact and not tagged.
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

// doTargetResolves reports whether the XObject named by a Do operator is present
// and resolvable in the given (effective) resources. A Do naming an absent
// XObject is a no-op (ISO 32000-1 §8.10.1) and paints nothing.
func doTargetResolves(ctx context.Context, doc *document.Document, resources ast.PdfDict, op content.Operation) bool {
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
	_, err := document.ResolveStream(ctx, doc, xoDict[name])
	return err == nil
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
