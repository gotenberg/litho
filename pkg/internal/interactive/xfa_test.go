package interactive

import (
	"testing"
)

func TestParseXFADatasets_Simple(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<xfa:datasets xmlns:xfa="http://www.xfa.org/schema/xfa-data/1.0/">
  <xfa:data>
    <form1>
      <FirstName>John</FirstName>
      <LastName>Doe</LastName>
      <Email>john@example.com</Email>
    </form1>
  </xfa:data>
</xfa:datasets>`

	result, err := parseXFADatasets([]byte(xml))
	if err != nil {
		t.Fatalf("parseXFADatasets: %v", err)
	}

	expected := map[string]string{
		"form1.FirstName": "John",
		"form1.LastName":  "Doe",
		"form1.Email":     "john@example.com",
	}

	for k, want := range expected {
		got, ok := result[k]
		if !ok {
			t.Errorf("missing key %q", k)
			continue
		}
		if got != want {
			t.Errorf("key %q: got %q, want %q", k, got, want)
		}
	}
}

func TestParseXFADatasets_Nested(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<xfa:datasets xmlns:xfa="http://www.xfa.org/schema/xfa-data/1.0/">
  <xfa:data>
    <form1>
      <address>
        <street>123 Main St</street>
        <city>Springfield</city>
        <state>IL</state>
      </address>
      <phone>555-1234</phone>
    </form1>
  </xfa:data>
</xfa:datasets>`

	result, err := parseXFADatasets([]byte(xml))
	if err != nil {
		t.Fatalf("parseXFADatasets: %v", err)
	}

	expected := map[string]string{
		"form1.address.street": "123 Main St",
		"form1.address.city":   "Springfield",
		"form1.address.state":  "IL",
		"form1.phone":          "555-1234",
	}

	for k, want := range expected {
		got, ok := result[k]
		if !ok {
			t.Errorf("missing key %q", k)
			continue
		}
		if got != want {
			t.Errorf("key %q: got %q, want %q", k, got, want)
		}
	}
}

func TestParseXFADatasets_NoDatasets(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<xfa:template xmlns:xfa="http://www.xfa.org/schema/xfa-template/2.6/">
  <subform name="form1"/>
</xfa:template>`

	result, err := parseXFADatasets([]byte(xml))
	if err != nil {
		t.Fatalf("parseXFADatasets: %v", err)
	}

	if result != nil {
		t.Errorf("expected nil for XML without datasets, got %v", result)
	}
}

func TestParseXFADatasets_EmptyData(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<xfa:datasets xmlns:xfa="http://www.xfa.org/schema/xfa-data/1.0/">
  <xfa:data>
  </xfa:data>
</xfa:datasets>`

	result, err := parseXFADatasets([]byte(xml))
	if err != nil {
		t.Fatalf("parseXFADatasets: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("expected empty map, got %v", result)
	}
}

func TestParseXFADatasets_DeeplyNested(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<xfa:datasets xmlns:xfa="http://www.xfa.org/schema/xfa-data/1.0/">
  <xfa:data>
    <root>
      <level1>
        <level2>
          <level3>deep value</level3>
        </level2>
      </level1>
    </root>
  </xfa:data>
</xfa:datasets>`

	result, err := parseXFADatasets([]byte(xml))
	if err != nil {
		t.Fatalf("parseXFADatasets: %v", err)
	}

	want := "deep value"
	got, ok := result["root.level1.level2.level3"]
	if !ok {
		t.Fatal("missing key root.level1.level2.level3")
	}
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestParseXFADatasets_WhitespaceHandling(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<xfa:datasets xmlns:xfa="http://www.xfa.org/schema/xfa-data/1.0/">
  <xfa:data>
    <form>
      <field>
        trimmed value
      </field>
    </form>
  </xfa:data>
</xfa:datasets>`

	result, err := parseXFADatasets([]byte(xml))
	if err != nil {
		t.Fatalf("parseXFADatasets: %v", err)
	}

	got := result["form.field"]
	if got != "trimmed value" {
		t.Errorf("got %q, want %q", got, "trimmed value")
	}
}

func TestParseXFADatasets_InvalidXML(t *testing.T) {
	xml := `<?xml version="1.0"?><not-closed>`

	result, err := parseXFADatasets([]byte(xml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should return nil since no datasets found.
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

func TestParseXFADatasets_NamespacedElements(t *testing.T) {
	// Real XFA data often uses namespace prefixes on data elements.
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<xfa:datasets xmlns:xfa="http://www.xfa.org/schema/xfa-data/1.0/">
  <xfa:data>
    <ns:form xmlns:ns="http://example.com">
      <ns:name>Alice</ns:name>
      <ns:age>30</ns:age>
    </ns:form>
  </xfa:data>
</xfa:datasets>`

	result, err := parseXFADatasets([]byte(xml))
	if err != nil {
		t.Fatalf("parseXFADatasets: %v", err)
	}

	// Local names should be used (namespace stripped).
	if result["form.name"] != "Alice" {
		t.Errorf("form.name: got %q, want %q", result["form.name"], "Alice")
	}
	if result["form.age"] != "30" {
		t.Errorf("form.age: got %q, want %q", result["form.age"], "30")
	}
}
