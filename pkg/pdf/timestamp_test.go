package pdf

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/gotenberg/litho/internal/pkitest"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// timestampWith writes src through a Writer that setup prepares and returns
// the output, failing the test when WriteTo does.
func timestampWith(t *testing.T, src []byte, setup func(w *Writer), opts ...WriteOption) []byte {
	t.Helper()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	w := NewWriter(r)
	defer w.Close()
	setup(w)
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out, opts...); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	return out.Bytes()
}

// TestWriterTimestamp adds a document timestamp alone, on top of a full
// rewrite and after a signature, and checks the verifier accepts each and
// the dictionary is the one ISO 32000-2, 12.8.5 describes.
func TestWriterTimestamp(t *testing.T) {
	t.Parallel()

	t.Run("alone", func(t *testing.T) {
		t.Parallel()
		pki := pkitest.New(t)
		out := timestampWith(t, writerTestPDF(), func(w *Writer) { w.Timestamp(pki.TSAURL(), WithTimestampField("DTS")) })
		results := verifyWith(t, out, pki)
		if len(results) != 1 {
			t.Fatalf("%d signatures, want 1", len(results))
		}
		v := results[0]
		if !v.HashValid || !v.CertChainValid || v.SubFilter != "ETSI.RFC3161" || v.SignerCN != pki.TSA.Subject.CommonName || v.TimestampError != "" {
			t.Errorf("document timestamp: hash %v, chain %v, subfilter %q, signer %q, timestamp error %q, error %q",
				v.HashValid, v.CertChainValid, v.SubFilter, v.SignerCN, v.TimestampError, v.ErrorMessage)
		}
		dts := signatureDictOf(t, out, "DTS")
		if dts["Type"] != ast.PdfName("DocTimeStamp") {
			t.Errorf("/Type = %v, want DocTimeStamp", dts["Type"])
		}
		if _, ok := dts["M"]; ok {
			t.Error("the document timestamp carries an /M, the signer's claim ISO 32000-2, 12.8.5 leaves to the token")
		}
		if !hasESIC(t, out) {
			t.Error("the catalog of a pre-2.0 document declares no ESIC extension")
		}
	})

	t.Run("on a full rewrite", func(t *testing.T) {
		t.Parallel()
		pki := pkitest.New(t)
		out := timestampWith(t, writerTestPDF(), func(w *Writer) { w.Timestamp(pki.TSAURL()) }, WithFullRewrite())
		if v := verifyWith(t, out, pki); len(v) != 1 || !v[0].HashValid {
			t.Errorf("validations %+v, want one document timestamp that verifies", v)
		}
	})

	t.Run("after a signature", func(t *testing.T) {
		t.Parallel()
		pki := pkitest.New(t)
		signer, err := NewSigner(pki.Leaf, pki.LeafKey, pki.Intermediate)
		if err != nil {
			t.Fatal(err)
		}
		out := timestampWith(t, writerTestPDF(), func(w *Writer) {
			w.Sign(signer, WithPAdES(), WithSignatureField("Approval"))
			w.Timestamp(pki.TSAURL(), WithTimestampField("Archive"))
		})
		results := verifyWith(t, out, pki)
		if len(results) != 2 {
			t.Fatalf("%d signatures, want 2", len(results))
		}
		for _, v := range results {
			if !v.HashValid || !v.CertChainValid {
				t.Errorf("%s: hash %v, chain %v: %s", v.FieldName, v.HashValid, v.CertChainValid, v.ErrorMessage)
			}
		}
		if got := pki.Requests("/tsa"); got != 1 {
			t.Errorf("%d timestamp requests, want 1", got)
		}
	})

	t.Run("after a remediation", func(t *testing.T) {
		t.Parallel()
		pki := pkitest.New(t)
		out := timestampWith(t, writerTestPDF(), func(w *Writer) {
			if _, err := w.RemediatePDFA(context.Background(), models.PDFA2b); err != nil {
				t.Fatalf("RemediatePDFA: %v", err)
			}
			w.Timestamp(pki.TSAURL())
		})
		if v := verifyWith(t, out, pki); len(v) != 1 || !v[0].HashValid {
			t.Errorf("validations %+v, want one document timestamp that verifies", v)
		}
	})

	t.Run("refuses", func(t *testing.T) {
		t.Parallel()
		pki := pkitest.New(t)
		pki.SetTSAMode(pkitest.TSAWrongImprint)
		ctx := context.Background()
		for _, tc := range []struct {
			name  string
			setup func(w *Writer)
			want  error
		}{
			{"a token over another digest", func(w *Writer) { w.Timestamp(pki.TSAURL()) }, ErrTimestampAuthority},
			{"an encryption", func(w *Writer) {
				w.Timestamp(pki.TSAURL())
				w.Encrypt(EncryptConfig{UserPassword: "u", OwnerPassword: "o"})
			}, ErrSignFullRewriteIncompatible},
		} {
			src := writerTestPDF()
			r, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
			if err != nil {
				t.Fatal(err)
			}
			w := NewWriter(r)
			tc.setup(w)
			var out bytes.Buffer
			if err := w.WriteTo(ctx, &out); !errors.Is(err, tc.want) {
				t.Errorf("%s: %v, want %v", tc.name, err, tc.want)
			}
			if out.Len() != 0 {
				t.Errorf("%s: a refused write wrote %d bytes", tc.name, out.Len())
			}
			if err := w.Close(); err != nil {
				t.Error(err)
			}
			if err := r.Close(); err != nil {
				t.Error(err)
			}
		}
	})
}

// hasESIC reports whether the catalog of data declares the ESIC extension.
func hasESIC(t *testing.T, data []byte) bool {
	t.Helper()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	cat, err := document.ResolveCatalog(ctx, r.doc)
	if err != nil {
		t.Fatal(err)
	}
	ext, err := document.ResolveDict(ctx, r.doc, cat.Dict["Extensions"])
	if err != nil || ext == nil {
		return false
	}
	_, ok := ext["ESIC"]
	return ok
}
