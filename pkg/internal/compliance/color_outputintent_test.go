package compliance

import (
	"context"
	"fmt"
	"testing"
)

// iccStreamObj renders an /OutputIntent /DestOutputProfile stream object whose
// ICC header carries the given device class, colour space, and major version.
func iccStreamObj(deviceClass, colorSpace string, version byte) string {
	data := make([]byte, 132)
	data[8] = version
	copy(data[12:16], deviceClass)
	copy(data[16:20], colorSpace)
	copy(data[36:40], "acsp")
	return fmt.Sprintf("<< /N 3 /Length %d >>\nstream\n%s\nendstream", len(data), data)
}

// countRule reports how many violations carry rule r.
func countRule(vs []Violation, r Rule) int {
	n := 0
	for _, v := range vs {
		if v.Rule == r {
			n++
		}
	}
	return n
}

// TestPageLevelOutputIntents covers ISO 19005-4 §6.2.3 on a page-level
// /OutputIntents array. PDF 2.0 admits /OutputIntents in a page dictionary
// (ISO 32000-2:2020, 14.11.5), so PDF/A-4 applies §6.2.3 t1 (ICC header), t2
// (one indirect profile per array), and t3 (no /DestOutputProfileRef) there and
// not only in the catalog. veraPDF 1.30.2 reports all three against
// root/document[0]/pages[0]/outputIntents[0] for flavour 4.
func TestPageLevelOutputIntents(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Two page-level intents: junk ICC headers, distinct profile objects, and a
	// forbidden /DestOutputProfileRef on the second.
	badDoc, badCat := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /OutputIntents [4 0 R 5 0 R] >>",
		"<< /Type /OutputIntent /S /GTS_PDFA1 /OutputConditionIdentifier (CGATS TR 001) /DestOutputProfile 6 0 R >>",
		"<< /Type /OutputIntent /S /GTS_PDFA1 /OutputConditionIdentifier (FOGRA39) /DestOutputProfile 7 0 R /DestOutputProfileRef << /Type /Filespec /F (p.icc) >> >>",
		iccStreamObj(" pla", "ceho", 2),
		iccStreamObj(" pla", "ceho", 2),
	)

	got := checkColorGroup(ctx, &validationCtx{doc: badDoc, catalog: badCat, profile: "PDF/A-4", part: 4})
	for _, want := range []Rule{Rule_19005_4_6_2_3_t01, Rule_19005_4_6_2_3_t02, Rule_19005_4_6_2_3_t03} {
		if !hasRule(got, want) {
			t.Errorf("page-level OutputIntents: missing %v in %v", want, got)
		}
	}

	// ISO 19005-2/3 derive from PDF 1.7, whose page dictionary has no
	// /OutputIntents entry. veraPDF reports §6.2.3 against page output intents
	// for flavour 4 alone, so parts 2 and 3 must stay catalog-only.
	for _, part := range []int{2, 3} {
		res := checkColorGroup(ctx, &validationCtx{doc: badDoc, catalog: badCat, profile: fmt.Sprintf("PDF/A-%db", part), part: part})
		for _, r := range []Rule{outputIntentICCRule(part), outputIntentConsistencyRule(part), destOutputProfileRefRule(part)} {
			if hasRule(res, r) {
				t.Errorf("part %d wrongly reported %v on a page-level OutputIntents array: %v", part, r, res)
			}
		}
	}

	// One valid profile shared by both entries and no /DestOutputProfileRef:
	// nothing to report.
	okDoc, okCat := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /OutputIntents [4 0 R 5 0 R] >>",
		"<< /Type /OutputIntent /S /GTS_PDFA1 /OutputConditionIdentifier (CGATS TR 001) /DestOutputProfile 6 0 R >>",
		"<< /Type /OutputIntent /S /GTS_PDFA1 /OutputConditionIdentifier (FOGRA39) /DestOutputProfile 6 0 R >>",
		iccStreamObj("prtr", "CMYK", 2),
	)

	clean := checkColorGroup(ctx, &validationCtx{doc: okDoc, catalog: okCat, profile: "PDF/A-4", part: 4})
	for _, r := range []Rule{Rule_19005_4_6_2_3_t01, Rule_19005_4_6_2_3_t02, Rule_19005_4_6_2_3_t03} {
		if hasRule(clean, r) {
			t.Errorf("conforming page-level OutputIntents wrongly reported %v: %v", r, clean)
		}
	}
}

// TestOutputIntentICCVersionCeiling pins the ICC version an output intent's
// /DestOutputProfile may carry in each part. PDF/A-1 is based on PDF 1.4,
// which predates ICC v4, so ISO 19005-1, 6.2.2 fails a v4 profile there
// (veraPDF's test reads "version < 3.0"); PDF/A-2 and later admit it.
func TestOutputIntentICCVersionCeiling(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	for _, tc := range []struct {
		part    int
		version byte
		fails   bool
	}{
		{part: 1, version: 2, fails: false},
		{part: 1, version: 4, fails: true},
		{part: 2, version: 4, fails: false},
		{part: 3, version: 4, fails: false},
		{part: 4, version: 4, fails: false},
	} {
		doc, cat := objsDocPDF(t,
			"<< /Type /Catalog /Pages 2 0 R /OutputIntents [4 0 R] >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
			"<< /Type /OutputIntent /S /GTS_PDFA1 /OutputConditionIdentifier (sRGB) /DestOutputProfile 5 0 R >>",
			iccStreamObj("mntr", "RGB ", tc.version),
		)
		var results []Violation
		checkOutputIntentICC(ctx, doc, cat, tc.part, &results)
		if got := hasRule(results, outputIntentICCRule(tc.part)); got != tc.fails {
			t.Errorf("part %d, ICC v%d: reported %v, want %v: %v", tc.part, tc.version, got, tc.fails, results)
		}
	}
}

// TestCatalogOutputIntentICCUnchanged pins the catalog-level §6.2.3 t1/t2
// reporting that [checkICCOutputProfileHeader] serves, so extending the clause
// to pages cannot silently drop it.
func TestCatalogOutputIntentICCUnchanged(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	doc, cat := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R /OutputIntents [4 0 R 5 0 R] >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /OutputIntent /S /GTS_PDFA1 /OutputConditionIdentifier (CGATS TR 001) /DestOutputProfile 6 0 R >>",
		"<< /Type /OutputIntent /S /GTS_PDFA1 /OutputConditionIdentifier (FOGRA39) /DestOutputProfile 7 0 R >>",
		iccStreamObj(" pla", "ceho", 2),
		iccStreamObj(" pla", "ceho", 2),
	)

	var results []Violation
	cs, hasICC := checkOutputIntentICC(ctx, doc, cat, 4, &results)
	if cs != "" || hasICC {
		t.Errorf("invalid ICC colour space must not grant coverage: cs=%q hasICC=%v", cs, hasICC)
	}
	// Two intents, each contributing an invalid device class and an invalid
	// colour space.
	if n := countRule(results, Rule_19005_4_6_2_3_t01); n != 4 {
		t.Errorf("t01 count = %d, want 4: %v", n, results)
	}
	if !hasRule(results, Rule_19005_4_6_2_3_t02) {
		t.Errorf("mismatched catalog DestOutputProfile objects must report t02: %v", results)
	}
}
