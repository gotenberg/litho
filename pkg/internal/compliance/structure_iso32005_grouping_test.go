package compliance

import "testing"

// clausesOf lists the Table 5 clause/test pairs a fixture drew, so a test can
// assert on the exact row AND test number: the two halves of a Table 5 row
// share a clause and differ only by test number, and picking the wrong half is
// the defect these tests pin.
func clausesOf(vs []Violation) map[string]bool {
	out := make(map[string]bool, len(vs))
	for _, v := range vs {
		out[v.Rule.Clause] = true
		if v.Rule.TestNumber != 0 {
			out[v.Rule.Clause+"/"+itoaTest(v.Rule.TestNumber)] = true
		}
	}
	return out
}

func itoaTest(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}

// linkWithKids lays out a Link structure element holding n empty children of
// the given type, carrying no direct content of its own.
func linkWithKids(t *testing.T, childType string, n int) []Violation {
	t.Helper()

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /Document /P 4 0 R /K [6 0 R] >>",
	}
	kids := ""
	for i := range n {
		kids += " " + itoaTest(7+i) + " 0 R"
	}
	objs = append(objs, "<< /Type /StructElem /S /Link /P 5 0 R /K ["+kids+" ] >>")
	for range n {
		objs = append(objs, "<< /Type /StructElem /S /"+childType+" /P 6 0 R >>")
	}
	return iso32005Violations(t, objs...)
}

// TestISO32005_GroupingByUse pins that the Table 5 isGrouping tests judge an
// element by what it holds, not by its type name alone.
//
// ISO 32000-2:2020, 14.8.4.2 lets Link sit at block or inline level, so a Link
// holding only empty block children is being used as a container and IS
// grouping. Keying on the type name alone answered both halves of the
// Link-Caption row wrongly: litho reported test 2 ("shall not contain", the
// non-grouping half) for a Link holding a single Caption, where veraPDF reports
// nothing, and missed test 1 ("at most one", the grouping half) for a Link
// holding two, which veraPDF does report.
func TestISO32005_GroupingByUse(t *testing.T) {
	t.Parallel()

	t.Run("a grouping Link with one Caption is silent", func(t *testing.T) {
		t.Parallel()
		got := clausesOf(linkWithKids(t, "Caption", 1))
		if got["Table 5. Link-Caption"] {
			t.Errorf("a grouping Link may hold one Caption; got %v", got)
		}
	})

	t.Run("a grouping Link with two Captions fails the at-most-one half", func(t *testing.T) {
		t.Parallel()
		got := clausesOf(linkWithKids(t, "Caption", 2))
		if !got["Table 5. Link-Caption/1"] {
			t.Errorf("two Captions must fail Link-Caption test 1; got %v", got)
		}
		if got["Table 5. Link-Caption/2"] {
			t.Errorf("the non-grouping half must not fire for a grouping Link; got %v", got)
		}
	})

	t.Run("a grouping Link may hold a heading", func(t *testing.T) {
		t.Parallel()
		// Table 5. Link-Hn is "when used as a NON-grouping element", so a Link
		// used as a container is outside its scope.
		got := clausesOf(linkWithKids(t, "H1", 1))
		if got["Table 5. Link-Hn"] {
			t.Errorf("a grouping Link may hold a heading; got %v", got)
		}
	})

	t.Run("an inline child keeps the Link out of the grouping role", func(t *testing.T) {
		t.Parallel()
		// A Span kid makes the Link inline-level content, so the non-grouping
		// half applies again and a Caption beside it is forbidden.
		vs := iso32005Violations(t,
			"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
			"<< /Type /StructTreeRoot /K 5 0 R >>",
			"<< /Type /StructElem /S /Document /P 4 0 R /K [6 0 R] >>",
			"<< /Type /StructElem /S /Link /P 5 0 R /K [7 0 R 8 0 R] >>",
			"<< /Type /StructElem /S /Span /P 6 0 R >>",
			"<< /Type /StructElem /S /Caption /P 6 0 R >>",
		)
		if got := clausesOf(vs); !got["Table 5. Link-Caption/2"] {
			t.Errorf("an inline kid makes the Link non-grouping; got %v", got)
		}
	})
}

// TestISO32005IsGroupingUnit pins the classification itself, including the
// types whose grouping role never depends on content.
func TestISO32005IsGroupingUnit(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		key        string
		hasContent bool
		kids       []string
		want       bool
	}{
		{"a grouping type groups even with content", "Div", true, nil, true},
		{"TOC groups by name", "TOC", true, nil, true},
		{"a block-only type never groups", "P", false, nil, false},
		{"a table row never groups", "TR", false, nil, false},
		{"an empty Link groups", "Link", false, nil, true},
		{"a Link with content does not", "Link", true, nil, false},
		{"a Link with an inline kid does not", "Link", false, []string{"Span"}, false},
		{"a Link with a block kid groups", "Link", false, []string{"Caption"}, true},
		{"Caption is content-dependent too", "Caption", false, nil, true},
		{"an unresolved type never groups", "", false, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := iso32005IsGrouping(tc.key, tc.hasContent, tc.kids); got != tc.want {
				t.Errorf("iso32005IsGrouping(%q, %v, %v) = %v, want %v",
					tc.key, tc.hasContent, tc.kids, got, tc.want)
			}
		})
	}
}
