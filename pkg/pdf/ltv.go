package pdf

import (
	"bytes"
	"context"
	"crypto/x509"
	"fmt"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/mutations"
	"github.com/gotenberg/litho/pkg/internal/signature"
)

// ValidationData is the validation material [WithValidationData] hands a
// signature's document security store instead of fetching it.
//
// A ValidationData is read, never modified, by [Writer.WriteTo]; do not
// modify it while a write that was given it runs.
type ValidationData struct {
	// Certificates are certificates the signer's and the timestamp
	// authority's chains may need beyond those their SignedData carry.
	Certificates []*x509.Certificate
	// OCSPResponses are DER OCSPResponse messages (RFC 6960, 4.2.1).
	OCSPResponses [][]byte
	// CRLs are DER certificate revocation lists (RFC 5280, 5.1).
	CRLs [][]byte
}

// WithLTV adds the signature's long-term validation data to the document
// security store (ISO 32000-2, 12.8.4.3).
//
// The data goes in an incremental update after the signature. It holds:
//
//   - the certificates of the signer's chain
//   - the certificates of the chain of the timestamp authority that
//     timestamped the signature
//   - for each certificate below a root, an OCSP response or a CRL saying it
//     is not revoked, also listed in the signature's VRI dictionary
//     (12.8.4.4)
//
// With [WithPAdES] and [WithTimestampAuthority] the signature is then a PAdES
// baseline B-LT one (ETSI EN 319 142-1, 6.3). A [Writer.Timestamp] on top of
// it makes it B-LTA.
//
// Without [WithValidationData], [Writer.WriteTo] fetches the data. It takes
// an issuer the SignedData lacks from the certificate's authority information
// access. It takes an OCSP response from the certificate's OCSP responders,
// and failing that its CRL from its CRL distribution points. It uses http or
// https, and bounds each exchange by ctx, a one-minute timeout and a size
// cap. An OCSP response counts only when the certificate's issuer, or a
// responder it delegated to, signed it. A CRL counts only when the issuer
// signed it.
//
// A certificate nothing vouches for, or whose issuer cannot be found, fails
// the write with [ErrValidationData]. A certificate reported revoked fails it
// with [ErrCertificateRevoked]. Nothing is written then. A store the document
// has already keeps what it holds and gains only what it lacks. The responses
// carry their responders' times, so the output differs from one write to the
// next.
func WithLTV() SignOption {
	return func(o *signOpts) { o.ltv = true }
}

// WithValidationData supplies the validation data [WithLTV] embeds, so
// signing reaches no network: the store gets the chains the signature's
// SignedData and the given certificates form, and for each certificate below
// a root the first given OCSP response, or failing that CRL, that vouches for
// it. A certificate none vouches for fails the write with
// [ErrValidationData]. It implies [WithLTV].
func WithValidationData(data ValidationData) SignOption {
	return func(o *signOpts) {
		o.ltv = true
		o.validation = &data
	}
}

// toInternal returns the validation data as the signature package reads it.
func (v *ValidationData) toInternal() *signature.ValidationData {
	if v == nil {
		return nil
	}
	out := &signature.ValidationData{OCSPs: v.OCSPResponses, CRLs: v.CRLs}
	for _, c := range v.Certificates {
		if c != nil {
			out.Certs = append(out.Certs, c.Raw)
		}
	}
	return out
}

// appendValidationData returns data, a document the Writer just signed, with
// the new signature's validation data added to its document security store
// in an incremental update (see [WithLTV]). The new signature is the one
// whose /ByteRange reaches the end of data.
func (w *Writer) appendValidationData(ctx context.Context, data []byte, opts ...WriteOption) ([]byte, error) {
	cfg := writeOpts{}
	for _, o := range opts {
		o(&cfg)
	}
	base, err := w.reader.reopenBytes(ctx, data)
	if err != nil {
		return nil, fmt.Errorf("reopening the signed document: %w", err)
	}
	defer func() {
		if closeErr := base.Close(); closeErr != nil {
			w.reader.doc.Logger.Debug("writer: closing the signed document failed", slog.Any("err", closeErr))
		}
	}()
	if base.doc.Rebuilt || base.doc.HeaderOffset > 0 {
		return nil, fmt.Errorf("the signed document does not read back as written")
	}
	if cfg.logger != nil {
		base.doc.Logger = cfg.logger
	}
	sigs, err := signature.ExtractSignatures(ctx, base.doc)
	if err != nil {
		return nil, fmt.Errorf("reading the new signature: %w", err)
	}
	var contents []byte
	for _, s := range sigs {
		if s.ByteRange[2]+s.ByteRange[3] == int64(len(data)) {
			contents = []byte(s.Contents)
		}
	}
	if contents == nil {
		return nil, fmt.Errorf("the signed document holds no signature reaching its end")
	}
	collected, err := signature.CollectValidationData(ctx, contents, w.signOpts.validation.toInternal())
	if err != nil {
		return nil, fmt.Errorf("gathering the signature's validation data: %w", err)
	}
	var buf bytes.Buffer
	if err := mutations.WriteDSS(ctx, &buf, base.doc, mutations.DSSData{
		Certs: collected.Certs, OCSPs: collected.OCSPs, CRLs: collected.CRLs, Signature: contents,
	}, cfg.xrefStream); err != nil {
		return nil, fmt.Errorf("adding the validation data: %w", err)
	}
	return buf.Bytes(), nil
}
