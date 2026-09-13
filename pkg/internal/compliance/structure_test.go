package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestActualTextPUA_UTF8BOMNotPUA covers the PDF 2.0 UTF-8 text-string form: a
// UTF-8 BOM (0xEF 0xBB 0xBF) prefix on an /ActualText or /Alt value must not be
// misread as the PUA code point U+EFBB. The check decodes per the PDF text-string
// rules before scanning for PUA, matching veraPDF's CosActualText.
func TestActualTextPUA_UTF8BOMNotPUA(t *testing.T) {
	t.Parallel()

	utf8Str := ast.PdfString(string([]byte{0xEF, 0xBB, 0xBF}) + "PDF 2.0 UTF-8")
	if containsPUARunes(document.DecodePdfString(utf8Str)) {
		t.Error("a UTF-8 BOM'd string must not be detected as PUA")
	}

	// Control: a genuine UTF-16BE PUA value is still detected.
	utf16PUA := ast.PdfString(string([]byte{0xFE, 0xFF, 0xE0, 0x00}))
	if !containsPUARunes(document.DecodePdfString(utf16PUA)) {
		t.Error("a UTF-16BE value containing PUA must still be detected")
	}
}

func TestContainsPUARunes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		s    string
		want bool
	}{
		{"empty", "", false},
		{"ascii", "Hello World", false},
		{"BMP PUA start", string(rune(0xE000)), true},
		{"BMP PUA end", string(rune(0xF8FF)), true},
		{"BMP PUA middle", string(rune(0xE500)), true},
		{"just below PUA", string(rune(0xDFFF)), false},
		{"just above PUA", string(rune(0xF900)), false},
		{"supplementary PUA-A start", string(rune(0xF0000)), true},
		{"supplementary PUA-A end", string(rune(0xFFFFF)), true},
		{"supplementary PUA-B start", string(rune(0x100000)), true},
		{"supplementary PUA-B end", string(rune(0x10FFFF)), true},
		{"mixed with PUA", "Hello" + string(rune(0xE000)) + "World", true},
		{"CJK character", "\u4e16", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := containsPUARunes(tc.s); got != tc.want {
				t.Errorf("containsPUARunes(%q) = %v, want %v", tc.s, got, tc.want)
			}
		})
	}
}

func TestHeadingLevel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		tag  ast.PdfName
		want int
	}{
		{"H1", 1},
		{"H2", 2},
		{"H3", 3},
		{"H4", 4},
		{"H5", 5},
		{"H6", 6},
		{"H", 0},
		{"H7", 0},
		{"P", 0},
		{"", 0},
	}
	for _, tc := range tests {
		t.Run(string(tc.tag), func(t *testing.T) {
			t.Parallel()
			if got := headingLevel(tc.tag); got != tc.want {
				t.Errorf("headingLevel(%q) = %d, want %d", tc.tag, got, tc.want)
			}
		})
	}
}

func TestIsStandardStructType(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		tag     ast.PdfName
		profile string
		want    bool
	}{
		{"Document is always standard", "Document", "PDF/A-1a", true},
		{"P is always standard", "P", "PDF/A-2b", true},
		{"Table is always standard", "Table", "PDF/UA-1", true},
		{"Title is PDF2 only - part 4", "Title", "PDF/A-4", true},
		{"Title is PDF2 only - UA2", "Title", "PDF/UA-2", true},
		{"Title is not standard for part 1", "Title", "PDF/A-1a", false},
		{"Title is not standard for UA1", "Title", "PDF/UA-1", false},
		{"Em is PDF2 only - part 4", "Em", "PDF/A-4", true},
		{"Em is not standard for part 2", "Em", "PDF/A-2a", false},
		{"nonexistent type", "FooBar", "PDF/A-4", false},
		{"empty name", "", "PDF/A-1a", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isStandardStructType(tc.tag, tc.profile); got != tc.want {
				t.Errorf("isStandardStructType(%q, %q) = %v, want %v", tc.tag, tc.profile, got, tc.want)
			}
		})
	}
}

func TestCheckMarkInfo(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("missing MarkInfo", func(t *testing.T) {
		t.Parallel()
		catalog := ast.PdfDict{}
		var results []Violation
		checkMarkInfo(ctx, nopDoc(), catalog, "PDF/A-1a", 1, &results)
		if len(results) == 0 {
			t.Error("expected violation for missing /MarkInfo")
		}
	})

	t.Run("MarkInfo missing Marked key", func(t *testing.T) {
		t.Parallel()
		catalog := ast.PdfDict{
			"MarkInfo": ast.PdfDict{},
		}
		var results []Violation
		checkMarkInfo(ctx, nopDoc(), catalog, "PDF/UA-1", 0, &results)
		if len(results) == 0 {
			t.Error("expected violation for /MarkInfo missing /Marked")
		}
	})

	t.Run("Marked is false", func(t *testing.T) {
		t.Parallel()
		catalog := ast.PdfDict{
			"MarkInfo": ast.PdfDict{
				"Marked": ast.PdfBool(false),
			},
		}
		var results []Violation
		checkMarkInfo(ctx, nopDoc(), catalog, "PDF/UA-2", 0, &results)
		if len(results) == 0 {
			t.Error("expected violation for /Marked=false")
		}
	})

	t.Run("Marked is true", func(t *testing.T) {
		t.Parallel()
		catalog := ast.PdfDict{
			"MarkInfo": ast.PdfDict{
				"Marked": ast.PdfBool(true),
			},
		}
		var results []Violation
		checkMarkInfo(ctx, nopDoc(), catalog, "PDF/A-1a", 1, &results)
		if len(results) != 0 {
			t.Errorf("expected no violations, got %d: %v", len(results), results)
		}
	})
}

func TestCheckStructTreeRoot(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("not required for non-a level", func(t *testing.T) {
		t.Parallel()
		catalog := ast.PdfDict{}
		var results []Violation
		st := checkStructTreeRoot(ctx, nopDoc(), catalog, "PDF/A-1b", 1, 'b', false, &results)
		if st != nil {
			t.Error("expected nil StructTreeRoot for level 'b'")
		}
		if len(results) != 0 {
			t.Errorf("expected no violations for level 'b', got %d", len(results))
		}
	})

	t.Run("missing for level a", func(t *testing.T) {
		t.Parallel()
		catalog := ast.PdfDict{}
		var results []Violation
		st := checkStructTreeRoot(ctx, nopDoc(), catalog, "PDF/A-1a", 1, 'a', false, &results)
		if st != nil {
			t.Error("expected nil StructTreeRoot when missing")
		}
		if len(results) == 0 {
			t.Error("expected violation for missing /StructTreeRoot")
		}
	})

	t.Run("missing for PDF/UA", func(t *testing.T) {
		t.Parallel()
		catalog := ast.PdfDict{}
		var results []Violation
		st := checkStructTreeRoot(ctx, nopDoc(), catalog, "PDF/UA-1", 0, 0, true, &results)
		if st != nil {
			t.Error("expected nil StructTreeRoot when missing")
		}
		if len(results) == 0 {
			t.Error("expected violation for missing /StructTreeRoot in PDF/UA")
		}
	})

	t.Run("present and valid", func(t *testing.T) {
		t.Parallel()
		stDict := ast.PdfDict{"Type": ast.PdfName("StructTreeRoot")}
		catalog := ast.PdfDict{
			"StructTreeRoot": stDict,
		}
		var results []Violation
		st := checkStructTreeRoot(ctx, nopDoc(), catalog, "PDF/A-2a", 2, 'a', false, &results)
		if st == nil {
			t.Error("expected non-nil StructTreeRoot")
		}
		if len(results) != 0 {
			t.Errorf("expected no violations, got %d: %v", len(results), results)
		}
	})
}

func TestCheckHeadingHierarchy(t *testing.T) {
	t.Parallel()

	t.Run("empty headings", func(t *testing.T) {
		t.Parallel()
		var results []Violation
		state := &walkState{results: &results, profile: "PDF/UA-1"}
		checkHeadingHierarchy(state)
		if len(results) != 0 {
			t.Errorf("expected no violations for empty headings, got %d", len(results))
		}
	})

	t.Run("valid H1 H2 H3", func(t *testing.T) {
		t.Parallel()
		var results []Violation
		state := &walkState{
			results:       &results,
			profile:       "PDF/UA-1",
			headingLevels: []int{1, 2, 3},
		}
		checkHeadingHierarchy(state)
		if len(results) != 0 {
			t.Errorf("expected no violations for H1-H2-H3, got %d: %v", len(results), results)
		}
	})

	t.Run("skipped level H1 to H3", func(t *testing.T) {
		t.Parallel()
		var results []Violation
		state := &walkState{
			results:       &results,
			profile:       "PDF/UA-1",
			headingLevels: []int{1, 3},
		}
		checkHeadingHierarchy(state)
		if len(results) == 0 {
			t.Error("expected violation for skipped heading level H1->H3")
		}
	})

	t.Run("first heading not H1", func(t *testing.T) {
		t.Parallel()
		var results []Violation
		state := &walkState{
			results:       &results,
			profile:       "PDF/UA-1",
			headingLevels: []int{2},
		}
		checkHeadingHierarchy(state)
		if len(results) == 0 {
			t.Error("expected violation when first heading is H2")
		}
	})

	t.Run("descending then ascending is ok", func(t *testing.T) {
		t.Parallel()
		var results []Violation
		state := &walkState{
			results:       &results,
			profile:       "PDF/UA-1",
			headingLevels: []int{1, 2, 3, 2, 1},
		}
		checkHeadingHierarchy(state)
		if len(results) != 0 {
			t.Errorf("expected no violations for H1-H2-H3-H2-H1, got %d: %v", len(results), results)
		}
	})

	t.Run("multiple skips", func(t *testing.T) {
		t.Parallel()
		var results []Violation
		state := &walkState{
			results:       &results,
			profile:       "PDF/UA-1",
			headingLevels: []int{1, 4, 6},
		}
		checkHeadingHierarchy(state)
		if len(results) < 2 {
			t.Errorf("expected at least 2 violations for H1->H4->H6, got %d", len(results))
		}
	})
}

func TestValidateTableContainment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		typeName   ast.PdfName
		parentType ast.PdfName
		wantFail   bool
	}{
		{"TR inside Table", "TR", "Table", false},
		{"TR inside THead", "TR", "THead", false},
		{"TR inside TBody", "TR", "TBody", false},
		{"TR inside TFoot", "TR", "TFoot", false},
		{"TR inside Div", "TR", "Div", true},
		{"THead inside Table", "THead", "Table", false},
		{"THead inside Div", "THead", "Div", true},
		{"TBody inside Table", "TBody", "Table", false},
		{"TBody inside Div", "TBody", "Div", true},
		{"TFoot inside Table", "TFoot", "Table", false},
		{"TFoot inside Div", "TFoot", "Div", true},
		{"TD inside TR", "TD", "TR", false},
		{"TD inside Table", "TD", "Table", true},
		{"TH inside TR", "TH", "TR", false},
		{"TH inside Div", "TH", "Div", true},
		{"P inside Div", "P", "Div", false},
		// Empty parent means root context, should not produce violations.
		{"TR at root", "TR", "", false},
		{"TH at root", "TH", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var results []Violation
			state := &walkState{results: &results, profile: "PDF/UA-1"}
			validateTableContainment(state, tc.typeName, tc.parentType)
			gotFail := len(results) > 0
			if gotFail != tc.wantFail {
				t.Errorf("validateTableContainment(%s, %s): got %d violations, wantFail=%v",
					tc.typeName, tc.parentType, len(results), tc.wantFail)
			}
		})
	}
}

func TestValidateTableChildren(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		typeName   ast.PdfName
		parentType ast.PdfName
		wantFail   bool
	}{
		{"TR in Table", "TR", "Table", false},
		{"THead in Table", "THead", "Table", false},
		{"Caption in Table", "Caption", "Table", false},
		{"P in Table", "P", "Table", true},
		// A Div is looked through: veraPDF's kidsStandardTypes lists its kids in
		// its place, and a Table holding a Div that wraps a TR passes §7.2 t3.
		{"Div in Table", "Div", "Table", false},
		{"TR in THead", "TR", "THead", false},
		{"Caption in TBody", "Caption", "TBody", false},
		{"P in THead", "P", "THead", true},
		{"TH in TR", "TH", "TR", false},
		{"TD in TR", "TD", "TR", false},
		{"P in TR", "P", "TR", true},
		// Non-table parent should not produce violations.
		{"P in Div", "P", "Div", false},
		{"TR in empty parent", "TR", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var results []Violation
			state := &walkState{results: &results, profile: "PDF/UA-1"}
			validateTableChildren(state, tc.typeName, tc.parentType)
			gotFail := len(results) > 0
			if gotFail != tc.wantFail {
				t.Errorf("validateTableChildren(%s, %s): got %d violations, wantFail=%v",
					tc.typeName, tc.parentType, len(results), tc.wantFail)
			}
		})
	}
}

func TestValidateListContainment(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name       string
		typeName   ast.PdfName
		parentType ast.PdfName
		wantFail   bool
	}{
		{"LI in L", "LI", "L", false},
		{"LI in Div", "LI", "Div", true},
		{"LBody in LI", "LBody", "LI", false},
		{"LBody in Div", "LBody", "Div", true},
		{"P in L", "P", "L", true},
		{"Caption in L", "Caption", "L", false},
		{"nested L in L", "L", "L", false},
		{"Lbl in LI", "Lbl", "LI", false},
		{"LBody in LI", "LBody", "LI", false},
		{"P in LI", "P", "LI", true},
		// Empty parent should not produce violations.
		{"LI at root", "LI", "", false},
		{"LBody at root", "LBody", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var results []Violation
			state := &walkState{results: &results, profile: "PDF/UA-1"}
			elem := ast.PdfDict{"S": ast.PdfName(tc.typeName)}
			validateListContainment(ctx, state, elem, tc.typeName, tc.parentType)
			gotFail := len(results) > 0
			if gotFail != tc.wantFail {
				t.Errorf("validateListContainment(%s, %s): got %d violations, wantFail=%v",
					tc.typeName, tc.parentType, len(results), tc.wantFail)
			}
		})
	}
}

func TestResolveToStandard(t *testing.T) {
	t.Parallel()

	t.Run("direct standard target", func(t *testing.T) {
		t.Parallel()
		mapping := map[ast.PdfName]ast.PdfName{
			"MyTag": "P",
		}
		var results []Violation
		ok := resolveToStandard("MyTag", mapping, &results, "PDF/UA-1")
		if !ok {
			t.Error("expected true for mapping to standard type")
		}
		if len(results) != 0 {
			t.Errorf("expected no violations, got %d", len(results))
		}
	})

	t.Run("chain to standard", func(t *testing.T) {
		t.Parallel()
		mapping := map[ast.PdfName]ast.PdfName{
			"Custom1": "Custom2",
			"Custom2": "Table",
		}
		var results []Violation
		ok := resolveToStandard("Custom1", mapping, &results, "PDF/UA-1")
		if !ok {
			t.Error("expected true for chain resolving to standard type")
		}
		if len(results) != 0 {
			t.Errorf("expected no violations, got %d", len(results))
		}
	})

	t.Run("circular mapping", func(t *testing.T) {
		t.Parallel()
		mapping := map[ast.PdfName]ast.PdfName{
			"A": "B",
			"B": "A",
		}
		var results []Violation
		ok := resolveToStandard("A", mapping, &results, "PDF/UA-1")
		if ok {
			t.Error("expected false for circular mapping")
		}
		if len(results) == 0 {
			t.Error("expected violation for circular mapping")
		}
	})

	// The non-standard rule is scoped to the structure element, so this
	// function only answers the question: checkUnmappedStructTypes reports it,
	// once per element rather than once per chain.
	t.Run("unmapped non-standard", func(t *testing.T) {
		t.Parallel()
		mapping := map[ast.PdfName]ast.PdfName{
			"Custom": "AlsoCustom",
		}
		var results []Violation
		ok := resolveToStandard("Custom", mapping, &results, "PDF/UA-1")
		if ok {
			t.Error("expected false for unmapped non-standard type")
		}
		if len(results) != 0 {
			t.Errorf("expected no violations from the chain walk, got %d", len(results))
		}
	})

	t.Run("self-mapping handled by caller", func(t *testing.T) {
		// resolveToStandard sees A→A as a circular mapping.
		t.Parallel()
		mapping := map[ast.PdfName]ast.PdfName{
			"Custom": "Custom",
		}
		var results []Violation
		ok := resolveToStandard("Custom", mapping, &results, "PDF/UA-1")
		if ok {
			t.Error("expected false for self-mapping")
		}
	})
}

// TestCheckRoleMap_StandardRemapTargetScope pins ISO 19005-2/3 §6.7.3.4 t3
// (veraPDF `remappedStandardType == null`): a standard structure type remapped
// to another STANDARD type (e.g. /Caption -> /P) conforms; only a remap to a
// NON-standard type fires. litho previously flagged any standard-type remap.
func TestCheckRoleMap_StandardRemapTargetScope(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc, _ := objsDocPDF(t, "<< /Type /Catalog >>")
	// The rule is scoped to the element, so the tree must hold one that uses
	// the remapped type; see TestRoleMapRulesAreElementScoped.
	mk := func(target ast.PdfName) []Violation {
		str := ast.PdfDict{
			"RoleMap": ast.PdfDict{"Caption": target},
			"K":       ast.PdfDict{"S": ast.PdfName("Caption")},
		}
		var results []Violation
		checkRoleMap(ctx, doc, str, "PDF/A-2a", &results)
		return results
	}
	if got := mk(ast.PdfName("P")); hasRule(got, Rule_19005_2_6_7_3_4_t03) {
		t.Errorf("standard->standard remap (/Caption->/P) must not fire §6.7.3.4 t3, got %v", got)
	}
	if got := mk(ast.PdfName("Foo")); !hasRule(got, Rule_19005_2_6_7_3_4_t03) {
		t.Errorf("standard->non-standard remap (/Caption->/Foo) must fire §6.7.3.4 t3, got %v", got)
	}
}

// TestCheckRoleMap_UA1SelfMapNotStandardRemap covers ISO 14289-1 §7.1: a standard
// type mapped to itself is an identity self-map, which veraPDF reports as a
// circular mapping (t6) with remappedStandardType == null, not a standard remap
// (t7). A standard type remapped to a DIFFERENT type still fires t7 under UA-1.
func TestCheckRoleMap_UA1SelfMapNotStandardRemap(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc, _ := objsDocPDF(t, "<< /Type /Catalog >>")
	// used names the type an element in the tree carries, which is what the
	// rule judges; see TestRoleMapRulesAreElementScoped.
	mk := func(rm ast.PdfDict, used ast.PdfName) []Violation {
		var results []Violation
		root := ast.PdfDict{"RoleMap": rm, "K": ast.PdfDict{"S": used}}
		checkRoleMap(ctx, doc, root, "PDF/UA-1", &results)
		return results
	}

	selfMap := mk(ast.PdfDict{"LI": ast.PdfName("LI")}, ast.PdfName("LI"))
	if !hasRule(selfMap, Rule_14289_1_7_1_t06) {
		t.Errorf("/LI->/LI self-map: want §7.1 t6 (circular), got %v", selfMap)
	}
	if hasRule(selfMap, Rule_14289_1_7_1_t07) {
		t.Errorf("/LI->/LI self-map: want no §7.1 t7 (standard remap), got %v", selfMap)
	}

	diffMap := mk(ast.PdfDict{"Caption": ast.PdfName("P")}, ast.PdfName("Caption"))
	if !hasRule(diffMap, Rule_14289_1_7_1_t07) {
		t.Errorf("/Caption->/P remap: want §7.1 t7, got %v", diffMap)
	}
}

// TestCheckRoleMap_StandardSelfMapCircularScope covers the profile-dependent scope
// of the circular-mapping rule: veraPDF keys it on PDStructElem under PDF/UA (fires
// on a standard type's self-map) but on SENonStandard under PDF/A (does not). A
// non-standard self-map is circular in every profile.
func TestCheckRoleMap_StandardSelfMapCircularScope(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc, _ := objsDocPDF(t, "<< /Type /Catalog >>")
	// used names the type an element in the tree carries, which is what the
	// rule judges; see TestRoleMapRulesAreElementScoped.
	mk := func(profile string, rm ast.PdfDict, used ast.PdfName) []Violation {
		var results []Violation
		root := ast.PdfDict{"RoleMap": rm, "K": ast.PdfDict{"S": used}}
		checkRoleMap(ctx, doc, root, profile, &results)
		return results
	}

	stdSelf := ast.PdfDict{"Document": ast.PdfName("Document")}
	if got := mk("PDF/A-2a", stdSelf, ast.PdfName("Document")); hasRule(got, Rule_19005_2_6_7_3_4_t02) {
		t.Errorf("PDF/A-2a: standard self-map /Document->/Document must NOT fire §6.7.3.4 t2, got %v", got)
	}
	if got := mk("PDF/UA-1", stdSelf, ast.PdfName("Document")); !hasRule(got, Rule_14289_1_7_1_t06) {
		t.Errorf("PDF/UA-1: standard self-map /Document->/Document must fire §7.1 t6, got %v", got)
	}

	nonStdSelf := ast.PdfDict{"Foo": ast.PdfName("Foo")}
	if got := mk("PDF/A-2a", nonStdSelf, ast.PdfName("Foo")); !hasRule(got, Rule_19005_2_6_7_3_4_t02) {
		t.Errorf("PDF/A-2a: non-standard self-map /Foo->/Foo must fire §6.7.3.4 t2, got %v", got)
	}
}
