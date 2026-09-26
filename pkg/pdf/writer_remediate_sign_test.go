package pdf

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"fmt"
	"log/slog"
	"math/big"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/models"
)

// OIDs of the CMS SignedData [cmsTestSigner] builds (RFC 5652, RFC 5754, RFC
// 5758).
var (
	oidCMSSignedData      = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 2}
	oidCMSData            = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 1}
	oidCMSSHA256          = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 1}
	oidCMSECDSAWithSHA256 = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 2}
)

// cmsAlgorithm is an AlgorithmIdentifier with no parameters.
type cmsAlgorithm struct {
	Algorithm asn1.ObjectIdentifier
}

// cmsIssuerAndSerial identifies the signing certificate (RFC 5652, 10.2.4).
type cmsIssuerAndSerial struct {
	Issuer       asn1.RawValue
	SerialNumber *big.Int
}

// cmsSignerInfo is a SignerInfo with no signed attributes (RFC 5652, 5.3).
type cmsSignerInfo struct {
	Version            int
	SID                cmsIssuerAndSerial
	DigestAlgorithm    cmsAlgorithm
	SignatureAlgorithm cmsAlgorithm
	Signature          []byte
}

// cmsEncapContent is a detached EncapsulatedContentInfo: its type and no
// content.
type cmsEncapContent struct {
	ContentType asn1.ObjectIdentifier
}

// cmsSignedDataOut is the SignedData [cmsTestSigner] writes (RFC 5652, 5.1).
// Certificates is written as given, so it carries its own [0] tag.
type cmsSignedDataOut struct {
	Version          int
	DigestAlgorithms []cmsAlgorithm `asn1:"set"`
	EncapContentInfo cmsEncapContent
	Certificates     asn1.RawValue
	SignerInfos      []cmsSignerInfo `asn1:"set"`
}

// cmsContentInfoOut wraps the SignedData (RFC 5652, 3). Content is written as
// given, so it carries its own [0] EXPLICIT tag.
type cmsContentInfoOut struct {
	ContentType asn1.ObjectIdentifier
	Content     asn1.RawValue
}

// cmsTestSigner signs with a throwaway ECDSA key and wraps the signature in a
// CMS SignedData holding one signer and its certificate, which is what ISO
// 19005-2, 6.4.3 tests 2 and 3 ask of a signature, so a signed PDF/A-2b output
// validates like the unsigned one. It counts the digests it signs.
type cmsTestSigner struct {
	cert  *x509.Certificate
	key   *ecdsa.PrivateKey
	calls atomic.Int32
}

func newCMSTestSigner(t *testing.T) *cmsTestSigner {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating the signing key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "litho test signer"},
		NotBefore:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:     time.Date(2036, 1, 1, 0, 0, 0, 0, time.UTC),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("creating the signing certificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parsing the signing certificate: %v", err)
	}
	return &cmsTestSigner{cert: cert, key: key}
}

func (s *cmsTestSigner) Sign(digest []byte) ([]byte, error) {
	s.calls.Add(1)
	sig, err := ecdsa.SignASN1(rand.Reader, s.key, digest)
	if err != nil {
		return nil, fmt.Errorf("signing the digest: %w", err)
	}
	signed, err := asn1.Marshal(cmsSignedDataOut{
		Version:          1,
		DigestAlgorithms: []cmsAlgorithm{{Algorithm: oidCMSSHA256}},
		EncapContentInfo: cmsEncapContent{ContentType: oidCMSData},
		Certificates: asn1.RawValue{
			Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: s.cert.Raw,
		},
		SignerInfos: []cmsSignerInfo{{
			Version:            1,
			SID:                cmsIssuerAndSerial{Issuer: asn1.RawValue{FullBytes: s.cert.RawIssuer}, SerialNumber: s.cert.SerialNumber},
			DigestAlgorithm:    cmsAlgorithm{Algorithm: oidCMSSHA256},
			SignatureAlgorithm: cmsAlgorithm{Algorithm: oidCMSECDSAWithSHA256},
			Signature:          sig,
		}},
	})
	if err != nil {
		return nil, fmt.Errorf("encoding the SignedData: %w", err)
	}
	return asn1.Marshal(cmsContentInfoOut{
		ContentType: oidCMSSignedData,
		Content:     asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: signed},
	})
}

func (s *cmsTestSigner) Certificate() *x509.Certificate { return s.cert }

func (s *cmsTestSigner) EstimatedSize() int { return 4096 }

// signatureCount returns how many signatures the document data holds.
func signatureCount(t *testing.T, data []byte) int {
	t.Helper()

	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("reopening the output: %v", err)
	}
	defer r.Close()
	sigs, err := r.ExtractSignatures(ctx)
	if err != nil {
		t.Fatalf("ExtractSignatures: %v", err)
	}
	return len(sigs)
}

// TestWriterSignKeepsTheRepairsOfEveryRemediationRound pins that a signed output
// carries every repair a remediation kept, however many rounds it kept and
// whether [Writer.Sign] came before or after it.
//
// The engine of [roundsEngine] makes each round's need independent of what the
// real fixers leave for a later one: one step is one round.
//
// A remediation that kept one round leaves its repairs in the Writer's
// overrides, and the signing path started from an empty allocator, so the
// signed file was the source plus a signature. One that kept more rounds hands
// the Writer its output to edit, so signing kept them. Signing before the
// remediation also sent each probe write through the signing path, which ran
// the caller's signer once per probe.
func TestWriterSignKeepsTheRepairsOfEveryRemediationRound(t *testing.T) {
	t.Parallel()

	oneRound := []roundsStep{{keys: []string{"PageLayout"}}}
	threeRounds := []roundsStep{
		{keys: []string{"PageLayout"}}, {keys: []string{"PageMode"}}, {keys: []string{"Lang"}},
	}
	for _, tc := range []struct {
		name      string
		steps     []roundsStep
		signFirst bool
	}{
		{name: "one round, signed after", steps: oneRound},
		{name: "one round, signed before", steps: oneRound, signFirst: true},
		{name: "three rounds, signed after", steps: threeRounds},
		{name: "three rounds, signed before", steps: threeRounds, signFirst: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			data := validXrefPDF(t)
			r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close()

			signer := newCMSTestSigner(t)
			e := &roundsEngine{steps: tc.steps}
			w := NewWriter(r)
			defer w.Close()
			if tc.signFirst {
				w.Sign(signer)
			}
			if _, err := w.remediate(ctx, engineTarget, DefaultRegressionTolerance, e.revalidate, e.run); err != nil {
				t.Fatalf("remediate: %v", err)
			}
			if len(e.disabled) != len(tc.steps) {
				t.Fatalf("the engine ran %d rounds, want %d", len(e.disabled), len(tc.steps))
			}
			if !tc.signFirst {
				w.Sign(signer)
			}

			var out bytes.Buffer
			if err := w.WriteTo(ctx, &out); err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			got, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
			if err != nil {
				t.Fatalf("reopening the output: %v", err)
			}
			defer got.Close()
			if res, err := e.revalidate(ctx, got); err != nil || len(res) != 0 {
				t.Errorf("the signed output misses a repair: %v %v", res, err)
			}
			if n := signatureCount(t, out.Bytes()); n != 1 {
				t.Errorf("the output holds %d signatures, want 1", n)
			}
			if n := signer.calls.Load(); n != 1 {
				t.Errorf("the signer ran %d times, want once", n)
			}
		})
	}
}

// signablePDF builds a one-page document a PDF/A-2b remediation repairs in an
// incremental update, since its header carries the binary comment and its
// cross-reference table is exact. What the remediation adds is what every
// PDF/A remediation adds to a document with no metadata: an XMP packet naming
// the profile, an output intent and a trailer /ID. Its Helvetica stays
// unembedded, so the remediation leaves a residual.
func signablePDF() []byte {
	const content = "BT /F1 12 Tf 20 700 Td (page) Tj ET\n"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R" +
			" /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n%\xE2\xE3\xCF\xD3\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

// validatePDFA2b validates data against PDF/A-2b.
func validatePDFA2b(t *testing.T, data []byte) []error {
	t.Helper()

	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("reopening the output: %v", err)
	}
	defer r.Close()
	res, err := r.ValidatePDFA(ctx, models.PDFA2b)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	return res
}

// TestWriterSignedRemediationValidatesLikeTheUnsigned pins, through the public
// API, that signing a remediated document keeps the remediation: the signed
// output validates to the remediation's residual, as the unsigned output does.
//
// It holds whatever number of rounds the remediation keeps, so it asserts the
// outcome rather than the round count. When it was written the remediation of
// [signablePDF] kept one round, and the signed output failed ISO 19005-2,
// 6.6.2.1 and 6.2.4.3 again: the signature was appended to the unrepaired
// source.
func TestWriterSignedRemediationValidatesLikeTheUnsigned(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := signablePDF()
	before := validatePDFA2b(t, data)

	unsigned := func() ([]byte, *RemediationReport) {
		r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		defer r.Close()
		w := NewWriter(r)
		defer w.Close()
		report, err := w.RemediatePDFA(ctx, models.PDFA2b)
		if err != nil {
			t.Fatalf("RemediatePDFA: %v", err)
		}
		var out bytes.Buffer
		if err := w.WriteTo(ctx, &out); err != nil {
			t.Fatalf("WriteTo: %v", err)
		}
		return out.Bytes(), report
	}
	plain, plainReport := unsigned()
	if len(plainReport.Fixed) == 0 || len(plainReport.Residual) >= len(before) {
		t.Fatalf("the remediation fixed %q and left %d of %d violations, so the test proves nothing",
			plainReport.Fixed, len(plainReport.Residual), len(before))
	}
	want := violationTexts(validatePDFA2b(t, plain))
	if got := violationTexts(plainReport.Residual); !slices.Equal(got, want) {
		t.Fatalf("the unsigned output validates to %q, want the remediation's residual %q", want, got)
	}

	for _, tc := range []struct {
		name string
		sign func(ctx context.Context, r *Reader, signer Signer) ([]byte, error)
	}{
		{
			name: "signed after the remediation",
			sign: func(ctx context.Context, r *Reader, signer Signer) ([]byte, error) {
				w := NewWriter(r)
				defer w.Close()
				if _, err := w.RemediatePDFA(ctx, models.PDFA2b); err != nil {
					return nil, err
				}
				w.Sign(signer)
				var out bytes.Buffer
				err := w.WriteTo(ctx, &out)
				return out.Bytes(), err
			},
		},
		{
			name: "signed before the remediation",
			sign: func(ctx context.Context, r *Reader, signer Signer) ([]byte, error) {
				w := NewWriter(r)
				defer w.Close()
				w.Sign(signer)
				if _, err := w.RemediatePDFA(ctx, models.PDFA2b); err != nil {
					return nil, err
				}
				var out bytes.Buffer
				err := w.WriteTo(ctx, &out)
				return out.Bytes(), err
			},
		},
		{
			name: "a pipeline signing in a later stage",
			sign: func(ctx context.Context, r *Reader, signer Signer) ([]byte, error) {
				var out bytes.Buffer
				_, err := NewPipeline().Open(r).
					Apply(func(ctx context.Context, w *Writer) error {
						_, err := w.RemediatePDFA(ctx, models.PDFA2b)
						return err
					}).
					Apply(func(_ context.Context, w *Writer) error {
						w.Sign(signer)
						return nil
					}).
					WriteTo(ctx, &out)
				return out.Bytes(), err
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rounds := &keptRounds{}
			r, err := Open(ctx, bytes.NewReader(data), int64(len(data)), WithLogger(slog.New(rounds)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close()
			signer := newCMSTestSigner(t)
			signed, err := tc.sign(ctx, r, signer)
			if err != nil {
				t.Fatalf("remediating and signing: %v", err)
			}
			t.Logf("the remediation kept %d later rounds", rounds.n.Load())

			if got := violationTexts(validatePDFA2b(t, signed)); !slices.Equal(got, want) {
				t.Errorf("the signed output validates to %q, want the unsigned output's %q", got, want)
			}
			if n := signatureCount(t, signed); n != 1 {
				t.Errorf("the output holds %d signatures, want 1", n)
			}
			if n := signer.calls.Load(); n != 1 {
				t.Errorf("the signer ran %d times, want once", n)
			}
		})
	}
}

// TestWriterSignsARemediationThatRewrites pins that a remediation whose repairs
// need a full rewrite is signed, in an incremental update on top of the
// remediation's finished output, as a caller would by writing, reopening and
// signing, and that the signed output validates like the unsigned one. With the
// /ID pinned, the signed output starts with the unsigned output's bytes.
//
// The header of [validXrefPDF] carries no binary comment, which ISO 19005-2,
// 6.1.2 requires and only a rewrite can add. Signing refused such a
// remediation with [ErrSignFullRewriteIncompatible], since the signature must
// be the last thing written and the rewrite renumbers every object.
func TestWriterSignsARemediationThatRewrites(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := validXrefPDF(t)
	// The /ID a remediation mints is random unless pinned, and the test
	// compares the signed output with the unsigned one byte for byte.
	pinnedID := WithDocumentID([]byte("0123456789abcdef"))

	unsigned := func() ([]byte, *RemediationReport) {
		r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		defer r.Close()
		w := NewWriter(r)
		defer w.Close()
		report, err := w.RemediatePDFA(ctx, models.PDFA2b, pinnedID)
		if err != nil {
			t.Fatalf("RemediatePDFA: %v", err)
		}
		if !w.forceRewrite {
			t.Fatal("the remediation writes an incremental update, so the test proves nothing")
		}
		var out bytes.Buffer
		if err := w.WriteTo(ctx, &out); err != nil {
			t.Fatalf("WriteTo: %v", err)
		}
		return out.Bytes(), report
	}
	plain, plainReport := unsigned()
	want := violationTexts(validatePDFA2b(t, plain))
	if got := violationTexts(plainReport.Residual); !slices.Equal(got, want) {
		t.Fatalf("the unsigned output validates to %q, want the remediation's residual %q", want, got)
	}

	for _, tc := range []struct {
		name string
		sign func(ctx context.Context, r *Reader, signer Signer) ([]byte, error)
	}{
		{
			name: "signed after the remediation",
			sign: func(ctx context.Context, r *Reader, signer Signer) ([]byte, error) {
				w := NewWriter(r)
				defer w.Close()
				if _, err := w.RemediatePDFA(ctx, models.PDFA2b, pinnedID); err != nil {
					return nil, err
				}
				w.Sign(signer)
				var out bytes.Buffer
				err := w.WriteTo(ctx, &out)
				return out.Bytes(), err
			},
		},
		{
			name: "signed before the remediation",
			sign: func(ctx context.Context, r *Reader, signer Signer) ([]byte, error) {
				w := NewWriter(r)
				defer w.Close()
				w.Sign(signer)
				if _, err := w.RemediatePDFA(ctx, models.PDFA2b, pinnedID); err != nil {
					return nil, err
				}
				var out bytes.Buffer
				err := w.WriteTo(ctx, &out)
				return out.Bytes(), err
			},
		},
		{
			name: "a pipeline signing in a later stage",
			sign: func(ctx context.Context, r *Reader, signer Signer) ([]byte, error) {
				var out bytes.Buffer
				_, err := NewPipeline().Open(r).
					Apply(func(ctx context.Context, w *Writer) error {
						_, err := w.RemediatePDFA(ctx, models.PDFA2b, pinnedID)
						return err
					}).
					Apply(func(_ context.Context, w *Writer) error {
						w.Sign(signer)
						return nil
					}).
					WriteTo(ctx, &out)
				return out.Bytes(), err
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close()
			signer := newCMSTestSigner(t)
			signed, err := tc.sign(ctx, r, signer)
			if err != nil {
				t.Fatalf("remediating and signing: %v", err)
			}

			if got := violationTexts(validatePDFA2b(t, signed)); !slices.Equal(got, want) {
				t.Errorf("the signed output validates to %q, want the unsigned output's %q", got, want)
			}
			checkSignedOutput(t, signed, signer)
			if n := signer.calls.Load(); n != 1 {
				t.Errorf("the signer ran %d times, want once", n)
			}
			if !bytes.HasPrefix(signed, plain) {
				t.Error("the signature is not an incremental update on top of the unsigned remediated output")
			}
		})
	}
}
