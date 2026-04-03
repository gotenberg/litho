package compliance

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// --- DisplayDocTitle ---

func checkDisplayDocTitle(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	rule := displayDocTitleRule(profile)

	vpObj, ok := catalog["ViewerPreferences"]
	if !ok {
		return fail(rule, "ViewerPreferences missing")
	}

	vp, err := document.ResolveDict(ctx, doc, vpObj)
	if err != nil || vp == nil {
		slog.Debug("compliance.accessibility: cannot resolve /ViewerPreferences", "error", err)
		return fail(rule, "ViewerPreferences unresolvable")
	}

	ddtObj, ok := vp["DisplayDocTitle"]
	if !ok {
		return fail(rule, "DisplayDocTitle missing")
	}

	ddt, err := document.ResolveBool(ctx, doc, ddtObj)
	if err != nil || !bool(ddt) {
		return fail(rule, "DisplayDocTitle=false")
	}

	return nil
}

// --- Suspects ---

func checkSuspects(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	rule := suspectsRule(profile)

	miObj, ok := catalog["MarkInfo"]
	if !ok {
		return nil
	}

	mi, err := document.ResolveDict(ctx, doc, miObj)
	if err != nil || mi == nil {
		slog.Debug("compliance.accessibility: cannot resolve /MarkInfo", "error", err)
		return nil
	}

	susObj, ok := mi["Suspects"]
	if !ok {
		return nil
	}

	sus, err := document.ResolveBool(ctx, doc, susObj)
	if err != nil {
		slog.Debug("compliance.accessibility: cannot resolve /Suspects", "error", err)
		return nil
	}

	if bool(sus) {
		return fail(rule, "Suspects=true")
	}

	return nil
}

// --- Tab order ---

func checkTabOrder(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	isPDFUA1 := profile == string("PDF/UA-1")
	isPDFUA2 := profile == string("PDF/UA-2")

	if !isPDFUA1 && !isPDFUA2 {
		return nil
	}

	rule := tabOrderRule(profile)

	pages := collectPages(ctx, doc, catalog)
	var results []Violation

	for pageIdx, page := range pages {
		if ctx.Err() != nil {
			return results
		}

		// Skip pages without annotations.
		annotsObj, hasAnnots := page["Annots"]
		if hasAnnots {
			annots, err := document.ResolveArray(ctx, doc, annotsObj)
			if err != nil || len(annots) == 0 {
				hasAnnots = false
			}
		}

		if !hasAnnots {
			// Both PDF/UA-1 and PDF/UA-2 only require /Tabs on pages with annotations.
			continue
		}

		tabsObj, ok := page["Tabs"]
		if !ok {
			if isPDFUA1 {
				// PDF/UA-1 §7.18.3: every page with annotations must have /Tabs = /S.
				results = append(results, Violation{Rule: rule, Context: fmt.Sprintf("page %d, Tabs missing", pageIdx)})
			} else if isPDFUA2 {
				results = append(results, Violation{Rule: rule, Context: fmt.Sprintf("page %d, Tabs missing", pageIdx)})
			}
			continue
		}

		tabsName, err := document.ResolveName(ctx, doc, tabsObj)
		if err != nil {
			slog.Debug("compliance.accessibility: cannot resolve /Tabs", "page", pageIdx, "error", err)
			continue
		}

		if isPDFUA1 && tabsName != "S" {
			results = append(results, Violation{Rule: rule, Context: fmt.Sprintf("page %d, Tabs=%s", pageIdx, tabsName)})
		} else if isPDFUA2 && hasAnnots && tabsName != "S" && tabsName != "A" && tabsName != "W" {
			results = append(results, Violation{Rule: rule, Context: fmt.Sprintf("page %d, Tabs=%s", pageIdx, tabsName)})
		}
	}

	return results
}

// --- XMP title ---

func checkXMPTitle(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	rule := xmpTitleRule(profile)

	xmpData, ok := readCatalogXMP(ctx, doc, catalog)
	if !ok {
		return fail(rule, "no XMP metadata")
	}

	if !containsDCTitle(xmpData) {
		return fail(rule, "")
	}

	return nil
}
