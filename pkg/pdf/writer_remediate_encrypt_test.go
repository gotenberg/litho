package pdf

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// aes256 is an encryption a reader opens without a password.
var aes256 = EncryptConfig{OwnerPassword: "owner", Algorithm: EncryptAES256, Permissions: -1}

// TestWriterRefusesToEncryptAPDFARemediation pins that the output of a PDF/A
// remediation is never encrypted: every part of ISO 19005 forbids the Encrypt
// key in the trailer (6.1.3 in each), so an encrypted output would fail the
// profile the remediation was asked to meet while its report said otherwise.
// The refusal names the clause of the part, and nothing is written.
func TestWriterRefusesToEncryptAPDFARemediation(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		data    func() []byte
		encrypt bool
		// atNew are the options given to NewWriter.
		atNew      []WriteOption
		remediate  func(ctx context.Context, w *Writer) (*RemediationReport, error)
		writeOpts  []WriteOption
		wantClause string
	}{
		{
			name:    "Encrypt before a PDF/A-2b remediation",
			data:    signablePDF,
			encrypt: true,
			remediate: func(ctx context.Context, w *Writer) (*RemediationReport, error) {
				return w.RemediatePDFA(ctx, models.PDFA2b)
			},
			wantClause: "ISO 19005-2, 6.1.3",
		},
		{
			name:    "Encrypt before a PDF/A-1b remediation",
			data:    signablePDF,
			encrypt: true,
			remediate: func(ctx context.Context, w *Writer) (*RemediationReport, error) {
				return w.RemediatePDFA(ctx, models.PDFA1b)
			},
			wantClause: "ISO 19005-1, 6.1.3",
		},
		{
			name:    "Encrypt before a Factur-X remediation",
			data:    signablePDF,
			encrypt: true,
			remediate: func(ctx context.Context, w *Writer) (*RemediationReport, error) {
				return w.RemediateFacturX(ctx, FacturXConfig{InvoiceXML: []byte("<x/>"), Level: models.FacturXMinimum})
			},
			wantClause: "ISO 19005-3, 6.1.3",
		},
		{
			name:  "WithEncryption given to NewWriter before a PDF/A-2b remediation",
			data:  signablePDF,
			atNew: []WriteOption{WithEncryption(aes256)},
			remediate: func(ctx context.Context, w *Writer) (*RemediationReport, error) {
				return w.RemediatePDFA(ctx, models.PDFA2b)
			},
			wantClause: "ISO 19005-2, 6.1.3",
		},
		{
			name:  "WithEncryption given to NewWriter before a Factur-X repair",
			data:  defectiveInvoicePDF,
			atNew: []WriteOption{WithEncryption(aes256)},
			remediate: func(ctx context.Context, w *Writer) (*RemediationReport, error) {
				return w.RepairFacturX(ctx)
			},
			wantClause: "ISO 19005-3, 6.1.3",
		},
		{
			name: "WithEncryption after a PDF/A-4 remediation",
			data: signablePDF,
			remediate: func(ctx context.Context, w *Writer) (*RemediationReport, error) {
				return w.RemediatePDFA(ctx, models.PDFA4)
			},
			writeOpts:  []WriteOption{WithEncryption(aes256)},
			wantClause: "ISO 19005-4, 6.1.3",
		},
		{
			name: "WithEncryption after a Factur-X repair",
			data: defectiveInvoicePDF,
			remediate: func(ctx context.Context, w *Writer) (*RemediationReport, error) {
				return w.RepairFacturX(ctx)
			},
			writeOpts:  []WriteOption{WithEncryption(aes256)},
			wantClause: "ISO 19005-3, 6.1.3",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			data := tc.data()
			r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close()
			w := NewWriter(r, tc.atNew...)
			defer w.Close()
			if tc.encrypt {
				w.Encrypt(aes256)
			}
			_, err = tc.remediate(ctx, w)
			if tc.encrypt || len(tc.atNew) > 0 {
				if !errors.Is(err, ErrPDFAForbidsEncryption) {
					t.Fatalf("the remediation returned %v, want ErrPDFAForbidsEncryption", err)
				}
				if !strings.Contains(err.Error(), tc.wantClause) {
					t.Errorf("the refusal %q does not cite %s", err, tc.wantClause)
				}
				return
			}
			if err != nil {
				t.Fatalf("the remediation: %v", err)
			}
			var out bytes.Buffer
			err = w.WriteTo(ctx, &out, tc.writeOpts...)
			if !errors.Is(err, ErrPDFAForbidsEncryption) {
				t.Fatalf("WriteTo returned %v, want ErrPDFAForbidsEncryption", err)
			}
			if !strings.Contains(err.Error(), tc.wantClause) {
				t.Errorf("the refusal %q does not cite %s", err, tc.wantClause)
			}
			if out.Len() != 0 {
				t.Errorf("WriteTo wrote %d bytes of an encrypted PDF/A output", out.Len())
			}
		})
	}
}

// TestWriterEncryptsAPDFUARemediation is the control: ISO 14289-1 does not
// forbid encryption (7.16 only asks that it leaves assistive technology its
// access), so the output of a PDF/UA remediation is encrypted on request.
func TestWriterEncryptsAPDFUARemediation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := signablePDF()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	w := NewWriter(r)
	defer w.Close()
	if _, err := w.RemediatePDFUA(ctx, models.PDFUA1); err != nil {
		t.Fatalf("RemediatePDFUA: %v", err)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out, WithEncryption(aes256)); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	got, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("reopening the output: %v", err)
	}
	defer got.Close()
	if !got.IsEncrypted() {
		t.Error("the output of a PDF/UA remediation written WithEncryption is not encrypted")
	}
}

// TestWriterEncryptBeforeARemediationProtectsTheOutput pins that
// [Writer.Encrypt], which after a remediation is refused, works before one
// whose profile allows encryption, the order the Writer now asks for.
//
// The remediation wrote its probes encrypted and reopened them without a
// password, so under a user password every probe read as broken and the
// remediation failed. Under an empty one, a remediation that kept a later
// round streamed that round's output, which is never encrypted, as the
// encrypted file it was marked as, and the output went out unprotected.
func TestWriterEncryptBeforeARemediationProtectsTheOutput(t *testing.T) {
	t.Parallel()

	uaOne := func(ctx context.Context, _ *testing.T, w *Writer) error {
		_, err := w.RemediatePDFUA(ctx, models.PDFUA1)
		return err
	}
	laterRounds := func(ctx context.Context, t *testing.T, w *Writer) error {
		e := &roundsEngine{steps: []roundsStep{{keys: []string{"PageLayout"}}, {keys: []string{"PageMode"}}}}
		_, err := w.remediate(ctx, engineTarget, DefaultRegressionTolerance, e.revalidate, e.run)
		if err == nil && len(e.disabled) != 2 {
			t.Fatalf("the engine ran %d rounds, want 2", len(e.disabled))
		}
		return err
	}
	for _, tc := range []struct {
		name      string
		password  string
		remediate func(ctx context.Context, t *testing.T, w *Writer) error
	}{
		{"a PDF/UA-1 remediation under a user password", "user", uaOne},
		{"a remediation keeping later rounds under a user password", "user", laterRounds},
		{"a remediation keeping later rounds under an empty user password", "", laterRounds},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			data := signablePDF()
			r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close()
			w := NewWriter(r)
			defer w.Close()
			w.Encrypt(EncryptConfig{UserPassword: tc.password, OwnerPassword: "owner", Algorithm: EncryptAES256, Permissions: -1})
			if err := tc.remediate(ctx, t, w); err != nil {
				t.Fatalf("the remediation: %v", err)
			}
			var out bytes.Buffer
			if err := w.WriteTo(ctx, &out); err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			if tc.password != "" {
				if _, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len())); !errors.Is(err, ErrInvalidPassword) {
					t.Errorf("opening the output without its password returned %v, want ErrInvalidPassword", err)
				}
			}
			got, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()), WithPassword(tc.password))
			if err != nil {
				t.Fatalf("opening the output with its password: %v", err)
			}
			defer got.Close()
			if !got.IsEncrypted() {
				t.Error("the output is not encrypted")
			}
		})
	}
}

// TestPipelineRefusesToEncryptAPDFARemediation pins the refusal through a
// [Pipeline], whose final write takes the encryption.
func TestPipelineRefusesToEncryptAPDFARemediation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := signablePDF()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	var out bytes.Buffer
	_, err = NewPipeline().Open(r).Apply(func(ctx context.Context, w *Writer) error {
		_, err := w.RemediatePDFA(ctx, models.PDFA2b)
		return err
	}).WriteTo(ctx, &out, WithEncryption(aes256))
	if !errors.Is(err, ErrPDFAForbidsEncryption) {
		t.Fatalf("Pipeline.WriteTo returned %v, want ErrPDFAForbidsEncryption", err)
	}
	if out.Len() != 0 {
		t.Errorf("the pipeline wrote %d bytes of an encrypted PDF/A output", out.Len())
	}
}

// TestWriterRefusesAPDFUA1EncryptionBarringAccessibility pins that the output
// of a PDF/UA-1 remediation is never encrypted with permissions that clear bit
// 10 of the P key. ISO 14289-1, 7.16 requires an encrypted conforming file to
// set it, and the remediation judges its output unencrypted, since it writes
// its probes in the clear: its report listed no 7.16 violation while the
// output WriteTo encrypted failed the rule. The refusal names the clause, and
// nothing is written.
func TestWriterRefusesAPDFUA1EncryptionBarringAccessibility(t *testing.T) {
	t.Parallel()

	barred := EncryptConfig{OwnerPassword: "owner", Algorithm: EncryptAES256, Permissions: -1 &^ accessibilityPermission}
	remediate := func(ctx context.Context, w *Writer) error {
		_, err := w.RemediatePDFUA(ctx, models.PDFUA1)
		return err
	}
	const clause = "ISO 14289-1, 7.16"

	for _, tc := range []struct {
		name string
		// write produces the output, returning the refusal of whichever call
		// refuses.
		write func(ctx context.Context, r *Reader, out *bytes.Buffer) error
	}{
		{
			name: "Encrypt before the remediation",
			write: func(ctx context.Context, r *Reader, out *bytes.Buffer) error {
				w := NewWriter(r)
				defer w.Close()
				w.Encrypt(barred)
				if err := remediate(ctx, w); err != nil {
					// A remediation that fails leaves the Writer open to edits.
					if rotErr := w.RotatePages(90); rotErr != nil {
						t.Errorf("RotatePages after the refused remediation: %v", rotErr)
					}
					return err
				}
				return w.WriteTo(ctx, out)
			},
		},
		{
			name: "WithEncryption given to NewWriter before the remediation",
			write: func(ctx context.Context, r *Reader, out *bytes.Buffer) error {
				w := NewWriter(r, WithEncryption(barred))
				defer w.Close()
				if err := remediate(ctx, w); err != nil {
					return err
				}
				return w.WriteTo(ctx, out)
			},
		},
		{
			name: "WithEncryption after the remediation",
			write: func(ctx context.Context, r *Reader, out *bytes.Buffer) error {
				w := NewWriter(r)
				defer w.Close()
				if err := remediate(ctx, w); err != nil {
					t.Fatalf("RemediatePDFUA: %v", err)
				}
				return w.WriteTo(ctx, out, WithEncryption(barred))
			},
		},
		{
			name: "a pipeline encrypting what it remediated",
			write: func(ctx context.Context, r *Reader, out *bytes.Buffer) error {
				_, err := NewPipeline().Open(r).Apply(func(ctx context.Context, w *Writer) error {
					return remediate(ctx, w)
				}).WriteTo(ctx, out, WithEncryption(barred))
				return err
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			data := signablePDF()
			r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close()
			var out bytes.Buffer
			err = tc.write(ctx, r, &out)
			if !errors.Is(err, ErrPDFUAForbidsBarringAccessibility) {
				t.Fatalf("the write returned %v, want ErrPDFUAForbidsBarringAccessibility", err)
			}
			if !strings.Contains(err.Error(), clause) {
				t.Errorf("the refusal %q does not cite %s", err, clause)
			}
			if out.Len() != 0 {
				t.Errorf("%d bytes were written of a PDF/UA-1 output that bars assistive technology", out.Len())
			}
		})
	}
}

// TestWriterEncryptsAPDFUA1RemediationThatPermitsAccessibility is the control:
// permissions that set bit 10 and clear every other permission are what ISO
// 14289-1, 7.16 asks for, so the output is encrypted on request, and it
// validates against 7.16 as the report says.
func TestWriterEncryptsAPDFUA1RemediationThatPermitsAccessibility(t *testing.T) {
	t.Parallel()

	permitted := EncryptConfig{OwnerPassword: "owner", Algorithm: EncryptAES256, Permissions: accessibilityPermission}
	for _, tc := range []struct {
		name    string
		encrypt bool
		atNew   []WriteOption
		opts    []WriteOption
	}{
		{name: "Encrypt before the remediation", encrypt: true},
		{name: "WithEncryption given to NewWriter", atNew: []WriteOption{WithEncryption(permitted)}},
		{name: "WithEncryption after the remediation", opts: []WriteOption{WithEncryption(permitted)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			data := signablePDF()
			r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close()
			w := NewWriter(r, tc.atNew...)
			defer w.Close()
			if tc.encrypt {
				w.Encrypt(permitted)
			}
			report, err := w.RemediatePDFUA(ctx, models.PDFUA1)
			if err != nil {
				t.Fatalf("RemediatePDFUA: %v", err)
			}
			var out bytes.Buffer
			if err := w.WriteTo(ctx, &out, tc.opts...); err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			got, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
			if err != nil {
				t.Fatalf("reopening the output: %v", err)
			}
			defer got.Close()
			if !got.IsEncrypted() {
				t.Fatal("the output is not encrypted, so the test proves nothing")
			}
			violations, err := got.ValidatePDFUAReport(ctx, models.PDFUA1)
			if err != nil {
				t.Fatalf("ValidatePDFUAReport: %v", err)
			}
			for _, v := range violations {
				if v.Clause == "7.16" {
					t.Errorf("the output fails ISO 14289-1, 7.16: %s", v.Context)
				}
			}
			for _, e := range report.Residual {
				if strings.Contains(e.Error(), "7.16") {
					t.Errorf("the report lists %v, which the output does not fail", e)
				}
			}
		})
	}
}
