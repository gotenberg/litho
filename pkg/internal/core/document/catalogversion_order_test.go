package document

import (
	"bytes"
	"context"
	"fmt"
	"testing"
)

// versionedPDF assembles a one-page document with the given header version and
// catalog /Version entry. An empty catalogVersion omits the entry.
func versionedPDF(headerVersion, catalogVersion string) []byte {
	catalog := "<< /Type /Catalog /Pages 2 0 R >>"
	if catalogVersion != "" {
		catalog = fmt.Sprintf("<< /Type /Catalog /Pages 2 0 R /Version /%s >>", catalogVersion)
	}
	objs := []string{
		catalog,
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	}

	var buf bytes.Buffer
	fmt.Fprintf(&buf, "%%PDF-%s\n", headerVersion)
	offsets := make([]int, len(objs)+1)
	for i, body := range objs {
		offsets[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	start := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offsets[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		len(objs)+1, start)
	return buf.Bytes()
}

// TestCatalogVersionReplacesTheHeaderVersion pins the precedence rule of
// ISO 32000-1, 7.5.2: the catalog /Version, if present, is used instead of the
// header version, later or not. Table 28 describes only when a producer writes
// the entry, so an earlier catalog version still wins on read. pdf.js resolves
// it the same way ("return this.catalog.version || this.#version").
//
// A /Version that is not a version number is ignored: pdf.js validates the
// same shape and warns, and a caller matching the version against a profile
// requirement can do nothing with a name like /Nonsense.
//
// # Corpus evidence
//
//   - pdfjs PDFJS-9279-reduced.pdf: header 1.7 and catalog /1.4, and its
//     metadata golden expects 1.4.
func TestCatalogVersionReplacesTheHeaderVersion(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		header  string
		catalog string
		want    string
	}{
		{"catalog is later", "1.4", "2.0", "2.0"},
		{"catalog is later within the same major", "1.4", "1.7", "1.7"},
		{"catalog is earlier", "1.7", "1.4", "1.4"},
		{"catalog is earlier across majors", "2.0", "1.7", "1.7"},
		{"catalog equals the header", "1.6", "1.6", "1.6"},
		{"no catalog entry", "1.5", "", "1.5"},
		{"catalog entry is not a version", "1.5", "Nonsense", "1.5"},
		{"catalog entry has a trailing character", "1.5", "1.4x", "1.5"},
		{"two-digit minor", "1.4", "1.10", "1.10"},
	} {
		data := versionedPDF(tc.header, tc.catalog)
		doc, err := NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatalf("%s: NewDocument: %v", tc.name, err)
		}
		if doc.Version != tc.want {
			t.Errorf("%s: header %s, catalog %q: Version = %q, want %q",
				tc.name, tc.header, tc.catalog, doc.Version, tc.want)
		}
	}
}
