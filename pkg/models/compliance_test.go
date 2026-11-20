package models

import (
	"encoding/json"
	"testing"
)

func TestPDFAProfile_JSONValue(t *testing.T) {
	tests := []struct {
		profile PDFAProfile
		want    string
	}{
		{PDFA1b, `"PDF/A-1b"`},
		{PDFA1a, `"PDF/A-1a"`},
		{PDFA2b, `"PDF/A-2b"`},
		{PDFA2u, `"PDF/A-2u"`},
		{PDFA2a, `"PDF/A-2a"`},
		{PDFA3b, `"PDF/A-3b"`},
		{PDFA3u, `"PDF/A-3u"`},
		{PDFA3a, `"PDF/A-3a"`},
		{PDFA4, `"PDF/A-4"`},
		{PDFA4e, `"PDF/A-4e"`},
		{PDFA4f, `"PDF/A-4f"`},
	}
	for _, tc := range tests {
		t.Run(string(tc.profile), func(t *testing.T) {
			data, err := json.Marshal(tc.profile)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(data) != tc.want {
				t.Errorf("got %s, want %s", data, tc.want)
			}
		})
	}
}

func TestPDFUAProfile_JSONValue(t *testing.T) {
	tests := []struct {
		profile PDFUAProfile
		want    string
	}{
		{PDFUA1, `"PDF/UA-1"`},
		{PDFUA2, `"PDF/UA-2"`},
	}
	for _, tc := range tests {
		t.Run(string(tc.profile), func(t *testing.T) {
			data, err := json.Marshal(tc.profile)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(data) != tc.want {
				t.Errorf("got %s, want %s", data, tc.want)
			}
		})
	}
}

func TestPDFAProfile_IsValid(t *testing.T) {
	valid := []PDFAProfile{
		PDFA1a, PDFA1b,
		PDFA2a, PDFA2b, PDFA2u,
		PDFA3a, PDFA3b, PDFA3u,
		PDFA4, PDFA4e, PDFA4f,
	}
	for _, p := range valid {
		if !p.IsValid() {
			t.Errorf("PDFAProfile(%q).IsValid() = false, want true", p)
		}
		if p.String() != string(p) {
			t.Errorf("String() drift on %q", p)
		}
	}

	invalid := []PDFAProfile{"", "PDF/A", "PDF/A-1", "PDF/A-1c", "pdf/a-2b"}
	for _, p := range invalid {
		if p.IsValid() {
			t.Errorf("PDFAProfile(%q).IsValid() = true, want false", p)
		}
	}
}

func TestPDFUAProfile_IsValid(t *testing.T) {
	for _, p := range []PDFUAProfile{PDFUA1, PDFUA2} {
		if !p.IsValid() {
			t.Errorf("PDFUAProfile(%q).IsValid() = false, want true", p)
		}
		if p.String() != string(p) {
			t.Errorf("String() drift on %q", p)
		}
	}
	for _, p := range []PDFUAProfile{"", "PDF/UA", "PDF/UA-3", "pdf/ua-1"} {
		if p.IsValid() {
			t.Errorf("PDFUAProfile(%q).IsValid() = true, want false", p)
		}
	}
}

func TestWTPDFProfile_IsValid(t *testing.T) {
	for _, p := range []WTPDFProfile{WTPDF1Accessibility, WTPDF1Reuse} {
		if !p.IsValid() {
			t.Errorf("WTPDFProfile(%q).IsValid() = false, want true", p)
		}
		if p.String() != string(p) {
			t.Errorf("String() drift on %q", p)
		}
	}
	for _, p := range []WTPDFProfile{"", "WTPDF", "WTPDF-1.0", "WTPDF-2.0-Reuse", "wtpdf-1.0-reuse", "PDF/UA-2"} {
		if p.IsValid() {
			t.Errorf("WTPDFProfile(%q).IsValid() = true, want false", p)
		}
	}
}

func TestWTPDFProfile_JSONValue(t *testing.T) {
	tests := []struct {
		profile WTPDFProfile
		want    string
	}{
		{WTPDF1Accessibility, `"WTPDF-1.0-Accessibility"`},
		{WTPDF1Reuse, `"WTPDF-1.0-Reuse"`},
	}
	for _, tc := range tests {
		t.Run(string(tc.profile), func(t *testing.T) {
			data, err := json.Marshal(tc.profile)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(data) != tc.want {
				t.Errorf("got %s, want %s", data, tc.want)
			}
		})
	}
}

func TestFacturXLevel_JSONValue(t *testing.T) {
	tests := []struct {
		level FacturXLevel
		want  string
	}{
		{FacturXMinimum, `"MINIMUM"`},
		{FacturXBasicWL, `"BASIC WL"`},
		{FacturXBasic, `"BASIC"`},
		{FacturXEN16931, `"EN 16931"`},
		{FacturXExtended, `"EXTENDED"`},
		{FacturXXRechnung, `"XRECHNUNG"`},
	}
	for _, tc := range tests {
		t.Run(string(tc.level), func(t *testing.T) {
			data, err := json.Marshal(tc.level)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(data) != tc.want {
				t.Errorf("got %s, want %s", data, tc.want)
			}
		})
	}
}

func TestZUGFeRDVersion_JSONValue(t *testing.T) {
	tests := []struct {
		version ZUGFeRDVersion
		want    string
	}{
		{ZUGFeRD1, `"1.0"`},
		{ZUGFeRD2, `"2.0"`},
		{ZUGFeRD21, `"2.1"`},
	}
	for _, tc := range tests {
		t.Run(string(tc.version), func(t *testing.T) {
			data, err := json.Marshal(tc.version)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(data) != tc.want {
				t.Errorf("got %s, want %s", data, tc.want)
			}
		})
	}
}

func TestZUGFeRDVersion_IsValid(t *testing.T) {
	for _, v := range []ZUGFeRDVersion{ZUGFeRD1, ZUGFeRD2, ZUGFeRD21} {
		if !v.IsValid() {
			t.Errorf("ZUGFeRDVersion(%q).IsValid() = false, want true", v)
		}
		if v.String() != string(v) {
			t.Errorf("String() drift on %q", v)
		}
	}
	for _, v := range []ZUGFeRDVersion{"", "3.0", "2", "2.2"} {
		if v.IsValid() {
			t.Errorf("ZUGFeRDVersion(%q).IsValid() = true, want false", v)
		}
	}
}

func TestFacturXLevel_IsValid(t *testing.T) {
	valid := []FacturXLevel{
		FacturXMinimum, FacturXBasicWL, FacturXBasic, FacturXEN16931, FacturXExtended, FacturXXRechnung,
	}
	for _, l := range valid {
		if !l.IsValid() {
			t.Errorf("FacturXLevel(%q).IsValid() = false, want true", l)
		}
		if l.String() != string(l) {
			t.Errorf("String() drift on %q", l)
		}
	}
	invalid := []FacturXLevel{"", "minimum", "basic", "EN16931", "Extended"}
	for _, l := range invalid {
		if l.IsValid() {
			t.Errorf("FacturXLevel(%q).IsValid() = true, want false", l)
		}
	}
}
