package compliance

import "testing"

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
