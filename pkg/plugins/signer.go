package plugins

import "crypto/x509"

// Signer produces a PKCS#7 / CMS signature for the given digest.
// Implementations control key material and signing algorithms. litho calls
// [Signer.Sign] during [github.com/gotenberg/litho/pkg/pdf.Writer.WriteTo]
// with the SHA-256 hash of the ByteRange regions.
//
// [github.com/gotenberg/litho/pkg/pdf.NewSigner],
// [github.com/gotenberg/litho/pkg/pdf.ParseSignerPEM] and
// [github.com/gotenberg/litho/pkg/pdf.ParseSignerPKCS12] return one built on
// an X.509 certificate and an RSA or ECDSA key. Implement it for anything
// else, such as a signature with a timestamp or one a remote service computes
// whole.
//
// Implementations must be safe for concurrent use. One signature may call
// Sign twice: again when the RFC 3161 token outgrows the room reserved for it,
// and again under another subfilter when a signature field's seed values
// recommend ETSI.CAdES.detached first and the first SignedData is not a CAdES
// one (see [github.com/gotenberg/litho/pkg/pdf.WithSignatureField]).
type Signer interface {
	// Sign computes a PKCS#7 / CMS signature over the given SHA-256
	// digest bytes.
	Sign(digest []byte) ([]byte, error)
	// Certificate returns the signing certificate for embedding in the
	// signature field's /Cert entry.
	Certificate() *x509.Certificate
	// EstimatedSize returns the maximum expected signature byte count;
	// used to size the /Contents placeholder in the output PDF.
	EstimatedSize() int
}
