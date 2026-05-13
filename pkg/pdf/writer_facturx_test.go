package pdf

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

const facturXSampleCII = `<?xml version="1.0" encoding="UTF-8"?>` +
	`<rsm:CrossIndustryInvoice xmlns:rsm="urn:un:unece:uncefact:data:standard:CrossIndustryInvoice:100">` +
	`<rsm:ExchangedDocumentContext><ram:BusinessProcessSpecifiedDocumentContextParameter>` +
	`<ram:ID>A1</ram:ID></ram:BusinessProcessSpecifiedDocumentContextParameter>` +
	`</rsm:ExchangedDocumentContext></rsm:CrossIndustryInvoice>`

func TestWriter_RemediateFacturX_RoundTrip(t *testing.T) {
	ctx := context.Background()
	data := writerTestPDF()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	w := NewWriter(r)
	report, err := w.RemediateFacturX(ctx, FacturXConfig{
		InvoiceXML: []byte(facturXSampleCII),
		Level:      models.FacturXEN16931,
	})
	if err != nil {
		t.Fatalf("RemediateFacturX: %v", err)
	}
	if len(report.Fixed) == 0 {
		t.Error("expected at least one fix in report")
	}

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer r2.Close()

	open, err := r2.XMPMetadata(ctx)
	if err != nil {
		t.Fatalf("XMPMetadata: %v", err)
	}
	if open == nil {
		t.Fatal("XMP stream missing from remediated output")
	}
	rc, err := open(ctx)
	if err != nil {
		t.Fatalf("opening XMP: %v", err)
	}
	xmpBytes, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		t.Fatalf("reading XMP: %v", err)
	}
	xmp := string(xmpBytes)
	for _, want := range []string{
		`xmlns:fx="urn:factur-x:pdfa:CrossIndustryDocument:invoice:1p0#"`,
		"<fx:DocumentType>INVOICE</fx:DocumentType>",
		"<fx:DocumentFileName>factur-x.xml</fx:DocumentFileName>",
		"<fx:Version>1.0</fx:Version>",
		"<fx:ConformanceLevel>EN 16931</fx:ConformanceLevel>",
		"pdfaExtension:schemas",
	} {
		if !strings.Contains(xmp, want) {
			t.Errorf("XMP missing %q\n--- XMP ---\n%s", want, xmp)
		}
	}

	files, err := r2.EmbeddedFiles(ctx)
	if err != nil {
		t.Fatalf("EmbeddedFiles: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("attachment count: got %d, want 1", len(files))
	}
	if files[0].Name != DefaultFacturXFileName {
		t.Errorf("attachment name: got %q, want %q", files[0].Name, DefaultFacturXFileName)
	}
	if files[0].MIMEType != "application/xml" {
		t.Errorf("attachment MIME: got %q, want %q", files[0].MIMEType, "application/xml")
	}
	rc2, err := files[0].Open(ctx)
	if err != nil {
		t.Fatalf("opening attachment: %v", err)
	}
	got, err := io.ReadAll(rc2)
	rc2.Close()
	if err != nil {
		t.Fatalf("reading attachment: %v", err)
	}
	if string(got) != facturXSampleCII {
		t.Errorf("attachment payload diverged from input invoice XML")
	}

	// The catalog must declare the file as an associated file via /AF
	// with /AFRelationship /Data. The serializer hex-escapes the forward
	// slash in "application/xml", so the raw stream check uses the
	// escaped form (#2F).
	rawPDF := out.String()
	if !strings.Contains(rawPDF, "/AFRelationship /Data") {
		t.Error("output PDF missing /AFRelationship /Data on filespec")
	}
	if !strings.Contains(rawPDF, "/Subtype /application#2Fxml") {
		t.Error("output PDF missing /Subtype /application/xml on embedded stream")
	}
}

func TestWriter_RemediateFacturX_AppliesDefaults(t *testing.T) {
	ctx := context.Background()
	data := writerTestPDF()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	w := NewWriter(r)
	// Profile, FileName, DocumentType, Relationship omitted on purpose.
	if _, err := w.RemediateFacturX(ctx, FacturXConfig{
		InvoiceXML: []byte(facturXSampleCII),
		Level:      models.FacturXBasic,
	}); err != nil {
		t.Fatalf("RemediateFacturX: %v", err)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	rawPDF := out.String()
	if !strings.Contains(rawPDF, DefaultFacturXFileName) {
		t.Errorf("expected default file name %q in output", DefaultFacturXFileName)
	}
	if !strings.Contains(rawPDF, "/AFRelationship /"+DefaultFacturXRelationship) {
		t.Errorf("expected default relationship %q in output", DefaultFacturXRelationship)
	}
}

func TestWriter_RemediateFacturX_RejectsBadConfig(t *testing.T) {
	ctx := context.Background()
	data := writerTestPDF()

	cases := []struct {
		name string
		cfg  FacturXConfig
		want error
	}{
		{
			name: "empty invoice",
			cfg:  FacturXConfig{Level: models.FacturXBasic},
			want: ErrEmptyInvoiceXML,
		},
		{
			name: "invalid level",
			cfg:  FacturXConfig{InvoiceXML: []byte("<x/>"), Level: "TURBO"},
			want: ErrInvalidFacturXLevel,
		},
		{
			name: "non-PDF/A-3 profile",
			cfg: FacturXConfig{
				InvoiceXML: []byte("<x/>"),
				Level:      models.FacturXBasic,
				Profile:    models.PDFA2b,
			},
			want: ErrInvalidFacturXProfile,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			w := NewWriter(r)
			_, err = w.RemediateFacturX(ctx, tc.cfg)
			if !errors.Is(err, tc.want) {
				t.Errorf("err: got %v, want errors.Is(_, %v)", err, tc.want)
			}
		})
	}
}
