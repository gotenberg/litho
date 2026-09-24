package pdf

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"testing/fstest"
)

// TestRepairFacturX_JudgesWithTheReadersSchematron pins that a repair judges
// its output with the Schematron the reader was opened with. The gate reopens
// the repaired file to revalidate it, and a bare reopen fell back to the
// bundled rules: the residuals then missed every finding of the caller's own.
func TestRepairFacturX_JudgesWithTheReadersSchematron(t *testing.T) {
	t.Parallel()

	const rules = `<?xml version="1.0" encoding="UTF-8"?>
<sch:schema xmlns:sch="http://purl.oclc.org/dsdl/schematron">
  <sch:ns prefix="rsm" uri="urn:un:unece:uncefact:data:standard:CrossIndustryInvoice:100"/>
  <sch:pattern id="caller">
    <sch:rule context="//rsm:CrossIndustryInvoice">
      <sch:assert id="BR-CALLER" flag="fatal" test="@Foo">[BR-CALLER] Foo attribute is mandatory</sch:assert>
    </sch:rule>
  </sch:pattern>
</sch:schema>`
	ctx := context.Background()
	data := defectiveInvoicePDFWith(facturXSampleCII)
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)),
		WithSchematron(fstest.MapFS{"rules.sch": &fstest.MapFile{Data: []byte(rules)}}, "rules.sch", ""))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	report, err := NewWriter(r).RepairFacturX(ctx)
	if err != nil {
		t.Fatalf("RepairFacturX: %v", err)
	}
	if len(report.Fixed) == 0 {
		t.Fatal("RepairFacturX fixed nothing: the gate that reopens the output never ran")
	}
	for _, e := range report.Residual {
		if strings.Contains(e.Error(), "BR-CALLER") {
			return
		}
	}
	t.Errorf("residuals lack the reader's own Schematron finding BR-CALLER: %v", report.Residual)
}
