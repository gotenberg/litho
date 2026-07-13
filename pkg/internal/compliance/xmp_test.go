package compliance

import (
	"bytes"
	"context"
	"testing"
)

// TestXMPCheckPDFAIdentification_AmdCorrPrefix covers the amd/corr wrong-prefix
// detection. The XML parser resolves namespaces by URI, so pdfa:amd (bound to
// the PDF/A Identification URI) looks correct; the byte scan must catch the
// non-canonical literal prefix.
func TestXMPCheckPDFAIdentification_AmdCorrPrefix(t *testing.T) {
	t.Parallel()
	wrap := func(decls, body string) []byte {
		return []byte(`<?xpacket begin=""?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
<rdf:Description rdf:about="" ` + decls + `>
` + body + `
</rdf:Description>
</rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`)
	}
	const idURI = `"http://www.aiim.org/pdfa/ns/id/"`

	hasRule := func(results []Violation, clause string, test int) bool {
		for _, v := range results {
			if v.Rule.Clause == clause && v.Rule.TestNumber == test {
				return true
			}
		}
		return false
	}

	t.Run("amd wrong prefix fires t06", func(t *testing.T) {
		t.Parallel()
		var results []Violation
		xmpCheckPDFAIdentification(context.Background(),
			wrap(`xmlns:pdfa=`+idURI, `<pdfa:amd>2016</pdfa:amd>`), "PDF/A-2b", 2, &results)
		if !hasRule(results, "6.6.4", 6) {
			t.Errorf("expected §6.6.4 t06 (amd wrong prefix), got %v", results)
		}
	})

	t.Run("corr wrong prefix fires t07", func(t *testing.T) {
		t.Parallel()
		var results []Violation
		xmpCheckPDFAIdentification(context.Background(),
			wrap(`xmlns:pdfa=`+idURI, `<pdfa:corr>A</pdfa:corr>`), "PDF/A-2b", 2, &results)
		if !hasRule(results, "6.6.4", 7) {
			t.Errorf("expected §6.6.4 t07 (corr wrong prefix), got %v", results)
		}
	})

	t.Run("canonical pdfaid:amd does not fire", func(t *testing.T) {
		t.Parallel()
		var results []Violation
		xmpCheckPDFAIdentification(context.Background(),
			wrap(`xmlns:pdfaid=`+idURI, `<pdfaid:amd>2016</pdfaid:amd>`), "PDF/A-2b", 2, &results)
		if hasRule(results, "6.6.4", 6) {
			t.Errorf("canonical pdfaid:amd must not flag t06, got %v", results)
		}
	})

	t.Run("amd wrong prefix part 1 fires 6.7.11 t06", func(t *testing.T) {
		t.Parallel()
		var results []Violation
		xmpCheckPDFAIdentification(context.Background(),
			wrap(`xmlns:pdfa=`+idURI, `<pdfa:amd>2016</pdfa:amd>`), "PDF/A-1b", 1, &results)
		if !hasRule(results, "6.7.11", 6) {
			t.Errorf("expected §6.7.11 t06 (PDF/A-1 amd wrong prefix), got %v", results)
		}
	})
}

func TestXMPDetectEncoding(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{
			name: "UTF-8 ASCII XML declaration",
			data: []byte(`<?xml version="1.0" encoding="UTF-8"?><x/>`),
			want: "UTF-8",
		},
		{
			name: "UTF-16 BE BOM",
			data: []byte{0xFE, 0xFF, 0x00, '<', 0x00, 'x', 0x00, '/', 0x00, '>'},
			want: "UTF-16BE",
		},
		{
			name: "UTF-16 LE BOM",
			data: []byte{0xFF, 0xFE, '<', 0x00, 'x', 0x00, '/', 0x00, '>', 0x00},
			want: "UTF-16LE",
		},
		{
			name: "UTF-16 LE no BOM",
			data: []byte{'<', 0x00, '?', 0x00, 'x', 0x00, 'm', 0x00, 'l', 0x00},
			want: "UTF-16LE",
		},
		{
			name: "UTF-16 BE no BOM",
			data: []byte{0x00, '<', 0x00, '?', 0x00, 'x', 0x00, 'm', 0x00, 'l'},
			want: "UTF-16BE",
		},
		{
			name: "UTF-32 LE BOM",
			data: []byte{0xFF, 0xFE, 0x00, 0x00, '<', 0, 0, 0, 'x', 0, 0, 0},
			want: "UTF-32LE",
		},
		{
			name: "UTF-32 BE BOM",
			data: []byte{0x00, 0x00, 0xFE, 0xFF, 0, 0, 0, '<', 0, 0, 0, 'x'},
			want: "UTF-32BE",
		},
		{
			name: "UTF-32 LE no BOM",
			data: []byte{'<', 0, 0, 0, '?', 0, 0, 0},
			want: "UTF-32LE",
		},
		{
			name: "UTF-32 BE no BOM",
			data: []byte{0, 0, 0, '<', 0, 0, 0, '?'},
			want: "UTF-32BE",
		},
		{
			name: "UTF-8 with explicit UTF-16 declaration in header",
			data: []byte(`<?xml version="1.0" encoding="UTF-16LE"?>`),
			want: "UTF-16LE",
		},
		{
			name: "empty input is UTF-8",
			data: []byte{},
			want: "UTF-8",
		},
		{
			name: "single byte ASCII",
			data: []byte{'a'},
			want: "UTF-8",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := xmpDetectEncoding(tc.data); got != tc.want {
				t.Errorf("xmpDetectEncoding = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestXMPDecodeToUTF8(t *testing.T) {
	t.Run("UTF-8 passthrough", func(t *testing.T) {
		t.Parallel()
		in := []byte("hello")
		out, err := xmpDecodeToUTF8(in, "UTF-8")
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !bytes.Equal(out, in) {
			t.Errorf("UTF-8 passthrough mutated: %q vs %q", out, in)
		}
	})

	t.Run("UTF-16 BE round trip", func(t *testing.T) {
		t.Parallel()
		// "Hi" in UTF-16 BE with BOM.
		in := []byte{0xFE, 0xFF, 0x00, 'H', 0x00, 'i'}
		out, err := xmpDecodeToUTF8(in, "UTF-16BE")
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if string(out) != "Hi" {
			t.Errorf("UTF-16 BE decode = %q, want %q", out, "Hi")
		}
	})

	t.Run("UTF-16 LE round trip strips BOM", func(t *testing.T) {
		t.Parallel()
		in := []byte{0xFF, 0xFE, 'H', 0x00, 'i', 0x00}
		out, err := xmpDecodeToUTF8(in, "UTF-16LE")
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if string(out) != "Hi" {
			t.Errorf("UTF-16 LE decode = %q, want %q", out, "Hi")
		}
	})

	t.Run("UTF-32 LE round trip", func(t *testing.T) {
		t.Parallel()
		in := []byte{0xFF, 0xFE, 0x00, 0x00, 'H', 0, 0, 0, 'i', 0, 0, 0}
		out, err := xmpDecodeToUTF8(in, "UTF-32LE")
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if string(out) != "Hi" {
			t.Errorf("UTF-32 LE decode = %q, want %q", out, "Hi")
		}
	})

	t.Run("UTF-32 BE round trip", func(t *testing.T) {
		t.Parallel()
		in := []byte{0x00, 0x00, 0xFE, 0xFF, 0, 0, 0, 'H', 0, 0, 0, 'i'}
		out, err := xmpDecodeToUTF8(in, "UTF-32BE")
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if string(out) != "Hi" {
			t.Errorf("UTF-32 BE decode = %q, want %q", out, "Hi")
		}
	})
}

func TestXMPEncodingRule(t *testing.T) {
	tests := []struct {
		part    int
		wantHit bool
	}{
		{1, false}, // PDF/A-1 has no UTF-8 mandate
		{2, true},
		{3, true},
		{4, true},
		{5, false},
	}
	for _, tc := range tests {
		got := xmpEncodingRule(tc.part, "UTF-16LE")
		if (len(got) > 0) != tc.wantHit {
			t.Errorf("xmpEncodingRule(%d) hit=%v, want %v", tc.part, len(got) > 0, tc.wantHit)
		}
	}
}

func TestXMPNormalizeForChecks(t *testing.T) {
	t.Run("UTF-8 passes through silently", func(t *testing.T) {
		t.Parallel()
		var results []Violation
		in := []byte(`<?xml version="1.0" encoding="UTF-8"?>`)
		out := xmpNormalizeForChecks(in, 2, &results)
		if !bytes.Equal(out, in) {
			t.Errorf("UTF-8 mutated by normaliser")
		}
		if len(results) != 0 {
			t.Errorf("UTF-8 emitted %d violations, want 0", len(results))
		}
	})

	t.Run("UTF-16 LE emits violation and decodes", func(t *testing.T) {
		t.Parallel()
		var results []Violation
		// "<x/>" in UTF-16 LE with BOM.
		in := []byte{0xFF, 0xFE, '<', 0, 'x', 0, '/', 0, '>', 0}
		out := xmpNormalizeForChecks(in, 2, &results)
		if string(out) != "<x/>" {
			t.Errorf("decoded = %q, want %q", out, "<x/>")
		}
		if len(results) != 1 {
			t.Fatalf("violation count = %d, want 1", len(results))
		}
	})

	t.Run("UTF-16 in PDF/A-1 still decodes but emits no rule", func(t *testing.T) {
		t.Parallel()
		var results []Violation
		in := []byte{0xFF, 0xFE, '<', 0, 'x', 0, '/', 0, '>', 0}
		out := xmpNormalizeForChecks(in, 1, &results)
		if string(out) != "<x/>" {
			t.Errorf("PDF/A-1 decode = %q, want %q", out, "<x/>")
		}
		if len(results) != 0 {
			t.Errorf("PDF/A-1 emitted %d violations, want 0", len(results))
		}
	})
}

func TestDCTitleLangAltMalformed(t *testing.T) {
	t.Parallel()
	wrap := func(title string) []byte {
		return []byte(`<?xpacket?><x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description xmlns:dc="http://purl.org/dc/elements/1.1/">` + title + `</rdf:Description></rdf:RDF></x:xmpmeta>`)
	}
	tests := []struct {
		name  string
		title string
		want  bool
	}{
		{"single li no lang", `<dc:title><rdf:Alt><rdf:li>T</rdf:li></rdf:Alt></dc:title>`, false},
		{"single li specific lang", `<dc:title><rdf:Alt><rdf:li xml:lang="en-US">T</rdf:li></rdf:Alt></dc:title>`, false},
		{"explicit x-default", `<dc:title><rdf:Alt><rdf:li xml:lang="x-default">T</rdf:li><rdf:li xml:lang="fr">Le</rdf:li></rdf:Alt></dc:title>`, false},
		{"multi li no x-default", `<dc:title><rdf:Alt><rdf:li>T</rdf:li><rdf:li>T</rdf:li></rdf:Alt></dc:title>`, true},
		{"no dc:title", ``, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := dcTitleLangAltMalformed(wrap(tc.title)); got != tc.want {
				t.Errorf("dcTitleLangAltMalformed(%q) = %v, want %v", tc.title, got, tc.want)
			}
		})
	}
}

// TestXMPCheckPDFAIdentification_PresentSchemaMissingField covers the
// schema-present/value-missing split: veraPDF reports the missing-identification
// rule (t1) only when the pdfaid schema is entirely absent. A present schema
// (any pdfaid field) with a missing part or conformance is a value-level
// violation (t2/t3), not t1.
func TestXMPCheckPDFAIdentification_PresentSchemaMissingField(t *testing.T) {
	t.Parallel()
	wrap := func(body string) []byte {
		return []byte(`<?xpacket begin=""?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
<rdf:Description rdf:about="" xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/">
` + body + `
</rdf:Description>
</rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`)
	}
	hasRule := func(results []Violation, clause string, test int) bool {
		for _, v := range results {
			if v.Rule.Clause == clause && v.Rule.TestNumber == test {
				return true
			}
		}
		return false
	}

	// PDF/A-4: rev present, part absent -> t2 (part), not t1.
	t.Run("part missing with schema present fires t2 not t1", func(t *testing.T) {
		t.Parallel()
		var results []Violation
		xmpCheckPDFAIdentification(context.Background(),
			wrap(`<pdfaid:rev>2020</pdfaid:rev>`), "PDF/A-4", 4, &results)
		if hasRule(results, "6.7.3", 1) {
			t.Errorf("schema present (rev), part absent: must not fire §6.7.3 t1, got %v", results)
		}
		if !hasRule(results, "6.7.3", 2) {
			t.Errorf("part absent: want §6.7.3 t2, got %v", results)
		}
	})

	// PDF/A-2b: part present, conformance absent -> t3, not t1.
	t.Run("conformance missing with schema present fires t3 not t1", func(t *testing.T) {
		t.Parallel()
		var results []Violation
		xmpCheckPDFAIdentification(context.Background(),
			wrap(`<pdfaid:part>2</pdfaid:part>`), "PDF/A-2b", 2, &results)
		if hasRule(results, "6.6.4", 1) {
			t.Errorf("schema present (part), conformance absent: must not fire §6.6.4 t1, got %v", results)
		}
		if !hasRule(results, "6.6.4", 3) {
			t.Errorf("conformance absent: want §6.6.4 t3, got %v", results)
		}
	})

	// No pdfaid field at all -> t1 (schema missing).
	t.Run("no pdfaid field fires t1", func(t *testing.T) {
		t.Parallel()
		var results []Violation
		xmpCheckPDFAIdentification(context.Background(),
			wrap(``), "PDF/A-2b", 2, &results)
		if !hasRule(results, "6.6.4", 1) {
			t.Errorf("no pdfaid field: want §6.6.4 t1 (schema missing), got %v", results)
		}
	})
}
