package compliance

import (
	"context"
	"strconv"
	"strings"
	"testing"
)

// TestPDFUAIdentificationDeclaredOnProperty pins the PDF/UA identification
// rules (ISO 14289-1 and -2, 5) on a packet declaring each namespace on the
// property element itself, as pikepdf writes it. Every case was measured
// against veraPDF 1.30.2, which reports exactly the listed tests of clause 5.
func TestPDFUAIdentificationDeclaredOnProperty(t *testing.T) {
	t.Parallel()
	el := func(prefix, local, value string) string {
		return `<` + prefix + `:` + local + ` xmlns:` + prefix + `="http://www.aiim.org/pdfua/ns/id/">` + value + `</` + prefix + `:` + local + `>`
	}
	packet := func(props ...string) []byte {
		return []byte(`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
			`<rdf:Description rdf:about="">` + strings.Join(props, "") + `</rdf:Description></rdf:RDF></x:xmpmeta>`)
	}
	for _, tc := range []struct {
		name, profile string
		xmp           []byte
		want          []Rule
	}{
		{"UA-1 part 1", "PDF/UA-1", packet(el("pdfuaid", "part", "1")), nil},
		{"UA-1 part 2", "PDF/UA-1", packet(el("pdfuaid", "part", "2")), []Rule{Rule_14289_1_5_t02}},
		{"UA-1 part under another prefix", "PDF/UA-1", packet(el("pdfua", "part", "1")), []Rule{Rule_14289_1_5_t03}},
		{"UA-1 amd under another prefix", "PDF/UA-1", packet(el("pdfuaid", "part", "1"), el("ua", "amd", "2014")), []Rule{Rule_14289_1_5_t04}},
		{"UA-2 part 2 rev 2024", "PDF/UA-2", packet(el("pdfuaid", "part", "2"), el("pdfuaid", "rev", "2024")), nil},
		{"UA-2 rev 2023", "PDF/UA-2", packet(el("pdfuaid", "part", "2"), el("pdfuaid", "rev", "2023")), []Rule{Rule_14289_2_5_t05}},
		{"UA-2 rev under another prefix", "PDF/UA-2", packet(el("pdfuaid", "part", "2"), el("uar", "rev", "2024")), []Rule{Rule_14289_2_5_t04}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			got := append(checkPDFUAIdentificationIn(ctx, tc.xmp, tc.profile), checkPDFUAIDPrefixesIn(ctx, tc.xmp, tc.profile)...)
			var gotRules, wantRules []string
			for _, v := range got {
				gotRules = append(gotRules, v.Rule.Clause+"/"+strconv.Itoa(v.Rule.TestNumber))
			}
			for _, r := range tc.want {
				wantRules = append(wantRules, r.Clause+"/"+strconv.Itoa(r.TestNumber))
			}
			if strings.Join(gotRules, " ") != strings.Join(wantRules, " ") {
				t.Errorf("clause 5 = %v, want %v", gotRules, wantRules)
			}
		})
	}
}
