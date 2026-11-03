package compliance

import (
	"errors"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestInvoiceLevel pins the level each Factur-X 1.0.07, 4.3 specification
// identifier declares, and the refusal of a payload that is no
// Cross-Industry Invoice.
func TestInvoiceLevel(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		guideline string
		want      models.FacturXLevel
	}{
		{"urn:factur-x.eu:1p0:minimum", models.FacturXMinimum},
		{"urn:factur-x.eu:1p0:basicwl", models.FacturXBasicWL},
		{"urn:cen.eu:en16931:2017#compliant#urn:factur-x.eu:1p0:basic", models.FacturXBasic},
		{"urn:cen.eu:en16931:2017", models.FacturXEN16931},
		{" urn:cen.eu:en16931:2017 ", models.FacturXEN16931},
		{"urn:cen.eu:en16931:2017#conformant#urn:factur-x.eu:1p0:extended", models.FacturXExtended},
		{"urn:cen.eu:en16931:2017#compliant#urn:xeinkauf.de:kosit:xrechnung_3.0", models.FacturXXRechnung},
		{"urn:cen.eu:en16931:2017#compliant#urn:xoev-de:kosit:standard:xrechnung_2.3", models.FacturXXRechnung},
		{"urn:example:other", ""},
		{"", ""},
	} {
		payload := strings.Replace(ciiInvoiceXML, "urn:cen.eu:en16931:2017", tc.guideline, 1)
		got, guideline, err := InvoiceLevel([]byte(payload))
		if err != nil {
			t.Errorf("%q: %v", tc.guideline, err)
			continue
		}
		if got != tc.want || guideline != strings.TrimSpace(tc.guideline) {
			t.Errorf("%q: level %q, guideline %q, want %q", tc.guideline, got, guideline, tc.want)
		}
	}

	for _, payload := range []string{"", "id,amount\n", "<invoice/>", ciiInvoiceXML[:len(ciiInvoiceXML)/2], `<rsm:CrossIndustryInvoice xmlns:rsm="urn:test"/>`} {
		if _, _, err := InvoiceLevel([]byte(payload)); !errors.Is(err, ErrInvoiceNotCII) {
			t.Errorf("%q: err %v, want ErrInvoiceNotCII", payload, err)
		}
	}
}
