package compliance

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestUA2Version pins ISO 14289-2, 6.1 t1 on the header and catalog /Version
// pairs measured on veraPDF 1.30.3: the catalog's version replaces the
// header's unless it is smaller, both read as Java floats, and a header with
// no readable number reads 1.4.
func TestUA2Version(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		header, version string
		want            bool
	}{
		{"%PDF-2.0", "", false},
		{"%PDF-1.7", "", true},
		{"%PDF-2.9", "", true},
		{"%PDF-1.7", "/2.0", false},
		{"%PDF-2.0", "/1.7", false},
		{"%PDF-2.0", "/2.1", true},
		{"%PDF-2.0", "/2.9", true},
		{"%PDF-2.9", "/2.0", true},
		{"%PDF-1.7", "/2", false},
		{"%PDF-1.7", "/2.00", false},
		{"%PDF-1.7", "/2.0f", false},
		{"%PDF-1.7", "/Nonsense", true},
		{"%PDF-1.7", "(2.0)", true},
		{"%PDF-1.7", "4 0 R", false},
		{"%PDF-2.00", "", false},
		{"%PDF-2.0junk", "", false},
		{"%PDF-2.f", "", false},
		{"%PDF-2.x", "", true},
		{"%PDF-2", "", true},
	} {
		catalog := "<< /Type /Catalog /Pages 2 0 R >>"
		if tc.version != "" {
			catalog = "<< /Type /Catalog /Pages 2 0 R /Version " + tc.version + " >>"
		}
		data := cyclePDF(catalog,
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
			"/2.0")
		data = bytes.Replace(data, []byte("%PDF-1.7"), []byte(tc.header), 1)
		ctx := context.Background()
		doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatalf("%s %s: NewDocument: %v", tc.header, tc.version, err)
		}
		cat, err := resolveCatalog(ctx, doc)
		if err != nil {
			t.Fatalf("%s %s: catalog: %v", tc.header, tc.version, err)
		}
		got := hasRule(checkUA2VersionGroup(ctx, &validationCtx{doc: doc, catalog: cat}), Rule_14289_2_6_1_t01)
		if got != tc.want {
			t.Errorf("header %s, /Version %q: 6.1 t1 reported = %v, want %v", tc.header, tc.version, got, tc.want)
		}
	}
}

// TestJavaParseFloat pins the float syntax the version test reads, Java's.
func TestJavaParseFloat(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		in   string
		want float32
		ok   bool
	}{
		{"2.0", 2, true},
		{"2", 2, true},
		{"2.", 2, true},
		{"2.f", 2, true},
		{" 2.0d", 2, true},
		{"2.x", 0, false},
		{"inf", 0, false},
		{"nan", 0, false},
		{"1_0", 0, false},
		{"f", 0, false},
		{"", 0, false},
	} {
		got, ok := javaParseFloat(tc.in)
		if ok != tc.ok || ok && got != tc.want {
			t.Errorf("javaParseFloat(%q) = %v, %v; want %v, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}
