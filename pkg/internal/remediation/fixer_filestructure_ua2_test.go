package remediation

import (
	"context"
	"errors"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/models"
)

// TestMatchFileStructure_UA2LaterHeader pins which ISO 14289-2, 6.1 t1
// findings the full rewrite answers: a header later than 2.0, which no catalog
// /Version lowers, and not an earlier one, which the catalog /Version
// declareOutputVersion writes raises without rewriting the file. Measured on
// veraPDF 1.30.3, the outputs of both pass the test.
func TestMatchFileStructure_UA2LaterHeader(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		header, catalog string
		want            bool
	}{
		{"2.9", "", true},
		{"2.9", "/Version /2.0", true},
		{"1.7", "", false},
		{"2.0", "/Version /2.1", false},
	} {
		doc, _ := newTestDoc(t, catalogVersionPDF(tc.header, tc.catalog))
		errs, err := compliance.ValidatePDFUA(context.Background(), doc, models.PDFUA2)
		if err != nil {
			t.Fatalf("%s %s: ValidatePDFUA: %v", tc.header, tc.catalog, err)
		}
		found := false
		for _, e := range errs {
			var v compliance.Violation
			if !errors.As(e, &v) || v.Rule != compliance.Rule_14289_2_6_1_t01 {
				continue
			}
			found = true
			if got := matchFileStructure(v); got != tc.want {
				t.Errorf("header %s, catalog %q: claimed for a rewrite = %v, want %v (%s)", tc.header, tc.catalog, got, tc.want, v.Context)
			}
		}
		if !found {
			t.Errorf("header %s, catalog %q: no 6.1 t1 finding, so this case asserts nothing", tc.header, tc.catalog)
		}
	}
}
