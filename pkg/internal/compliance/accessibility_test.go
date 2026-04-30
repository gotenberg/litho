package compliance

import (
	"context"
	"testing"
)

func TestExtractXMPPropertyValue(t *testing.T) {
	t.Parallel()

	t.Run("attribute form", func(t *testing.T) {
		t.Parallel()
		raw := `<rdf:Description pdfuaid:part="2" pdfuaid:rev="2024">`
		got := extractXMPPropertyValue(raw, "pdfuaid", "part")
		if got != "2" {
			t.Errorf("extractXMPPropertyValue() = %q, want %q", got, "2")
		}
	})

	t.Run("element form", func(t *testing.T) {
		t.Parallel()
		raw := `<pdfuaid:part>2</pdfuaid:part>`
		got := extractXMPPropertyValue(raw, "pdfuaid", "part")
		if got != "2" {
			t.Errorf("extractXMPPropertyValue() = %q, want %q", got, "2")
		}
	})

	t.Run("element form with whitespace", func(t *testing.T) {
		t.Parallel()
		raw := `<pdfuaid:rev>  2024  </pdfuaid:rev>`
		got := extractXMPPropertyValue(raw, "pdfuaid", "rev")
		if got != "2024" {
			t.Errorf("extractXMPPropertyValue() = %q, want %q", got, "2024")
		}
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()
		raw := `<rdf:Description>`
		got := extractXMPPropertyValue(raw, "pdfuaid", "part")
		if got != "" {
			t.Errorf("extractXMPPropertyValue() = %q, want empty", got)
		}
	})

	t.Run("empty input", func(t *testing.T) {
		t.Parallel()
		got := extractXMPPropertyValue("", "pdfuaid", "part")
		if got != "" {
			t.Errorf("extractXMPPropertyValue() = %q, want empty", got)
		}
	})
}

func TestIsFourDigitYear(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  bool
	}{
		{"2024", true},
		{"1999", true},
		{"0001", true},
		{"9999", true},
		{"", false},
		{"123", false},
		{"12345", false},
		{"abcd", false},
		{"20a4", false},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			if got := isFourDigitYear(tc.input); got != tc.want {
				t.Errorf("isFourDigitYear(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

// TestCheckPDFUAIDPrefixesIn verifies that the PDF/UA prefix checks key
// off the PDF/UA Identification namespace URI, not just the local name.
// Properties from the PDF/A Identification namespace (pdfaid:part,
// pdfaid:rev, etc.) share local names with PDF/UA properties but live
// under a different URI and must never trigger these rules.
func TestCheckPDFUAIDPrefixesIn(t *testing.T) {
	t.Parallel()

	const combinedA4UA2 = `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
<rdf:Description rdf:about=""
  xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/"
  xmlns:pdfuaid="http://www.aiim.org/pdfua/ns/id/">
  <pdfaid:part>4</pdfaid:part>
  <pdfaid:rev>2020</pdfaid:rev>
  <pdfuaid:part>2</pdfuaid:part>
</rdf:Description>
</rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`

	const uaNsUnderWrongPrefix = `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
<rdf:Description rdf:about=""
  xmlns:foo="http://www.aiim.org/pdfua/ns/id/">
  <foo:part>2</foo:part>
  <foo:rev>2024</foo:rev>
</rdf:Description>
</rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`

	const revNotAYear = `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
<rdf:Description rdf:about=""
  xmlns:pdfuaid="http://www.aiim.org/pdfua/ns/id/">
  <pdfuaid:part>2</pdfuaid:part>
  <pdfuaid:rev>abcd</pdfuaid:rev>
</rdf:Description>
</rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`

	cases := []struct {
		name      string
		xmp       string
		profile   string
		wantRules []Rule
	}{
		{
			name:      "combined PDF/A-4 and PDF/UA-2: no violations",
			xmp:       combinedA4UA2,
			profile:   "PDF/UA-2",
			wantRules: nil,
		},
		{
			name:      "combined PDF/A-4 and PDF/UA-2 checked as PDF/UA-1: no violations",
			xmp:       combinedA4UA2,
			profile:   "PDF/UA-1",
			wantRules: nil,
		},
		{
			// The fixture has both foo:part and foo:rev under the PDF/UA
			// URI. PDF/UA-2 checks both part (t03) and rev (t04); both
			// should fire since neither appears under the canonical
			// "pdfuaid" prefix.
			name:      "PDF/UA namespace under wrong prefix trips PDF/UA-2 part and rev rules",
			xmp:       uaNsUnderWrongPrefix,
			profile:   "PDF/UA-2",
			wantRules: []Rule{Rule_14289_2_5_t03, Rule_14289_2_5_t04},
		},
		{
			name:      "PDF/UA namespace under wrong prefix trips PDF/UA-1 part rule",
			xmp:       uaNsUnderWrongPrefix,
			profile:   "PDF/UA-1",
			wantRules: []Rule{Rule_14289_1_5_t03},
		},
		{
			name:      "pdfuaid:rev that is not a 4-digit year fires t5",
			xmp:       revNotAYear,
			profile:   "PDF/UA-2",
			wantRules: []Rule{Rule_14289_2_5_t05},
		},
		{
			// VeraPDF fixture PDF_UA-1/5-t04-fail-a.pdf: both pdfuaid
			// and the non-canonical pdfuaia bind the same PDF/UA URI.
			// 'amd' appears under pdfuaia; 'part' uses the canonical
			// prefix to satisfy t02. t04 must still fire.
			name: "dual binding with non-canonical prefix on amd",
			xmp: `<rdf:Description
  xmlns:pdfuaid="http://www.aiim.org/pdfua/ns/id/"
  xmlns:pdfuaia="http://www.aiim.org/pdfua/ns/id/">
  <pdfuaid:part>1</pdfuaid:part>
  <pdfuaia:amd>A</pdfuaia:amd>
</rdf:Description>`,
			profile:   "PDF/UA-1",
			wantRules: []Rule{Rule_14289_1_5_t04},
		},
		{
			// VeraPDF fixture PDF_UA-1/5-t05-fail-a.pdf, same pattern
			// as t04 but for 'corr'.
			name: "dual binding with non-canonical prefix on corr",
			xmp: `<rdf:Description
  xmlns:pdfuaid="http://www.aiim.org/pdfua/ns/id/"
  xmlns:pdfuaia="http://www.aiim.org/pdfua/ns/id/">
  <pdfuaid:part>1</pdfuaid:part>
  <pdfuaia:corr>B</pdfuaia:corr>
</rdf:Description>`,
			profile:   "PDF/UA-1",
			wantRules: []Rule{Rule_14289_1_5_t05},
		},
		{
			// VeraPDF fixture PDF_UA-2/5-t03-fail-a.pdf: pdfuadd +
			// pdfuaid both bind the UA URI; 'part' uses pdfuadd and
			// 'rev' uses the canonical prefix. t03 must fire.
			name: "dual binding with non-canonical prefix on part (UA-2)",
			xmp: `<rdf:Description
  xmlns:pdfuaid="http://www.aiim.org/pdfua/ns/id/"
  xmlns:pdfuadd="http://www.aiim.org/pdfua/ns/id/"
  pdfuadd:part="2"
  pdfuaid:rev="2024"/>`,
			profile:   "PDF/UA-2",
			wantRules: []Rule{Rule_14289_2_5_t03},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := checkPDFUAIDPrefixesIn(context.Background(), []byte(tc.xmp), tc.profile)
			gotRules := make([]Rule, 0, len(got))
			for _, v := range got {
				gotRules = append(gotRules, v.Rule)
			}
			if len(gotRules) != len(tc.wantRules) {
				t.Fatalf("got %d violations %v, want %d %v", len(gotRules), gotRules, len(tc.wantRules), tc.wantRules)
			}
			for i, want := range tc.wantRules {
				if gotRules[i].TestNumber != want.TestNumber || gotRules[i].Clause != want.Clause {
					t.Errorf("violation %d: got %+v, want %+v", i, gotRules[i], want)
				}
			}
		})
	}
}

// TestContainsDCTitle_NamespaceAware verifies containsDCTitle only
// returns true when the "dc" prefix is bound to the Dublin Core URI. A
// rogue XMP that binds "dc" to a different namespace and defines
// <dc:title> must not count as a compliant title.
func TestContainsDCTitle_NamespaceAware(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		xmp  string
		want bool
	}{
		{
			name: "canonical element form",
			xmp: `<rdf:Description xmlns:dc="http://purl.org/dc/elements/1.1/">
  <dc:title><rdf:Alt><rdf:li xml:lang="x-default">Doc</rdf:li></rdf:Alt></dc:title>
</rdf:Description>`,
			want: true,
		},
		{
			name: "canonical attribute form",
			xmp:  `<rdf:Description xmlns:dc="http://purl.org/dc/elements/1.1/" dc:title="Doc"/>`,
			want: true,
		},
		{
			name: "rogue namespace bound to dc prefix",
			xmp: `<rdf:Description xmlns:dc="http://example.org/imposter/">
  <dc:title>Impostor</dc:title>
</rdf:Description>`,
			want: false,
		},
		{
			name: "no dc binding",
			xmp:  `<rdf:Description><other:title>Doc</other:title></rdf:Description>`,
			want: false,
		},
		{
			name: "DC namespace bound to non-canonical prefix",
			xmp: `<rdf:Description xmlns:dublin="http://purl.org/dc/elements/1.1/">
  <dublin:title>Doc</dublin:title>
</rdf:Description>`,
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := containsDCTitle(context.Background(), []byte(tc.xmp)); got != tc.want {
				t.Errorf("containsDCTitle = %v, want %v\n%s", got, tc.want, tc.xmp)
			}
		})
	}
}

// TestContainsPDFUAIDPart_NamespaceAware verifies containsPDFUAIDPart
// keys off the PDF/UA Identification namespace URI rather than a
// hardcoded "pdfuaid" prefix.
func TestContainsPDFUAIDPart_NamespaceAware(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		xmp  string
		part string
		want bool
	}{
		{
			name: "canonical pdfuaid prefix, element form",
			xmp: `<rdf:Description xmlns:pdfuaid="http://www.aiim.org/pdfua/ns/id/">
  <pdfuaid:part>2</pdfuaid:part>
</rdf:Description>`,
			part: "2",
			want: true,
		},
		{
			name: "canonical pdfuaid prefix, attribute form",
			xmp:  `<rdf:Description xmlns:pdfuaid="http://www.aiim.org/pdfua/ns/id/" pdfuaid:part="2"/>`,
			part: "2",
			want: true,
		},
		{
			name: "PDF/UA namespace under non-canonical prefix",
			xmp: `<rdf:Description xmlns:ua="http://www.aiim.org/pdfua/ns/id/">
  <ua:part>1</ua:part>
</rdf:Description>`,
			part: "1",
			want: true,
		},
		{
			name: "pdfuaid prefix bound to a different URI",
			xmp: `<rdf:Description xmlns:pdfuaid="http://example.org/imposter/">
  <pdfuaid:part>2</pdfuaid:part>
</rdf:Description>`,
			part: "2",
			want: false,
		},
		{
			name: "wrong part value",
			xmp: `<rdf:Description xmlns:pdfuaid="http://www.aiim.org/pdfua/ns/id/">
  <pdfuaid:part>1</pdfuaid:part>
</rdf:Description>`,
			part: "2",
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := containsPDFUAIDPart(context.Background(), []byte(tc.xmp), tc.part); got != tc.want {
				t.Errorf("containsPDFUAIDPart(_, %q) = %v, want %v\n%s", tc.part, got, tc.want, tc.xmp)
			}
		})
	}
}
