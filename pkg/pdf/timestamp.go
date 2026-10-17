package pdf

import (
	"bytes"
	"context"
	"crypto/x509"
	"fmt"
	"io"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/mutations"
	"github.com/gotenberg/litho/pkg/internal/signature"
)

// TimestampOption sets the field [Writer.Timestamp] puts its document
// timestamp in.
type TimestampOption func(*timestampOpts)

// timestampOpts holds what [Writer.Timestamp] and its [TimestampOption]
// values set.
type timestampOpts struct {
	url   string
	field string
}

// WithTimestampField names the document timestamp's signature field, as
// [WithSignatureField] names a signature's: an unsigned signature field of
// that name takes it, and a new root field otherwise. Without it the field
// is named Signature1, Signature2 or the first such name no field holds.
func WithTimestampField(name string) TimestampOption {
	return func(o *timestampOpts) { o.field = name }
}

// Timestamp adds an RFC 3161 document timestamp (ISO 32000-2, 12.8.5; ETSI EN
// 319 142-1, 5.4.3) in an incremental update of its own: a signature field
// whose value is a /DocTimeStamp dictionary with the ETSI.RFC3161 subfilter,
// whose /Contents is the token the timestamp authority at tsaURL returns for
// the SHA-256 of the bytes its /ByteRange covers. It vouches that the whole
// document, the signatures and validation data before it included, existed
// at the authority's time: the last step of a PAdES baseline B-LTA
// signature, and a way to renew one.
//
// Timestamp registers the timestamp as [Writer.Sign] registers a signature,
// so it may be called after a remediation, and [Writer.WriteTo] refuses it
// beside an encryption or a decryption with [ErrSignFullRewriteIncompatible].
// With a signature too, the signature comes first, then the validation data
// [WithLTV] adds, then the document timestamp. The authority is reached as
// [WithTimestampAuthority] describes, and one that gives no usable token
// fails the write with [ErrTimestampAuthority]. A later call replaces an
// earlier one.
//
// A signature whose field carried a /Lock does not stop the timestamp: in a
// new field it changes no field the signature's FieldMDP transform locked
// (ISO 32000-1, 12.8.2.4), and the update writes only the catalog, the form
// dictionary, the invisible field and its /DocTimeStamp dictionary. A field
// [WithTimestampField] names that the lock reaches is refused with
// [ErrSignatureFieldLocked], since signing it would change it.
func (w *Writer) Timestamp(tsaURL string, opts ...TimestampOption) {
	w.timestamp = &timestampOpts{url: tsaURL}
	for _, o := range opts {
		o(w.timestamp)
	}
}

// signingStep is one signature an output takes: the signer that computes its
// /Contents and the dictionary it goes under.
type signingStep struct {
	signer mutations.Signer
	info   mutations.SignatureInfo
}

// signingSteps returns the signatures [Writer.WriteTo] appends, in order: the
// one [Writer.Sign] asked for, then the document timestamp
// [Writer.Timestamp] asked for.
func (w *Writer) signingSteps() []signingStep {
	var steps []signingStep
	if w.signer != nil {
		adapter, info := w.signing(w.signer)
		steps = append(steps, signingStep{signer: adapter, info: info})
	}
	if w.timestamp != nil {
		steps = append(steps, signingStep{
			signer: docTimestamper{},
			info:   mutations.SignatureInfo{Field: w.timestamp.field, DocTimeStamp: true, TimestampURL: w.timestamp.url},
		})
	}
	return steps
}

// writeSigned writes the output with write, signed by the first of steps, and
// appends each further step as an incremental update of its own. The output
// is built in memory when more than one update follows the source, since
// each reads what the one before wrote; the signing flow holds its update in
// memory already.
func (w *Writer) writeSigned(ctx context.Context, dst io.Writer, write func(context.Context, io.Writer, *signingStep, ...WriteOption) error, steps []signingStep, opts ...WriteOption) error {
	// The validation data [WithLTV] asks for follows the signature it
	// validates, and a document timestamp covers both.
	ltv := w.signer != nil && w.signOpts.ltv
	if len(steps) <= 1 && !ltv {
		var first *signingStep
		if len(steps) == 1 {
			first = &steps[0]
		}
		return write(ctx, dst, first, opts...)
	}
	var buf bytes.Buffer
	if err := write(ctx, &buf, &steps[0], opts...); err != nil {
		return err
	}
	out := buf.Bytes()
	if ltv {
		next, err := w.appendValidationData(ctx, out, opts...)
		if err != nil {
			return err
		}
		out = next
	}
	for i := range steps[1:] {
		next, err := w.appendSignature(ctx, out, &steps[1+i], opts...)
		if err != nil {
			return err
		}
		out = next
	}
	if _, err := dst.Write(out); err != nil {
		return fmt.Errorf("writing the signed document: %w", err)
	}
	return nil
}

// appendSignature returns data, a document litho signed, with step's
// signature appended in an incremental update.
func (w *Writer) appendSignature(ctx context.Context, data []byte, step *signingStep, opts ...WriteOption) ([]byte, error) {
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
	var buf bytes.Buffer
	if err := mutations.WriteWithSignature(ctx, &buf, base.doc, step.signer, step.info, nil, nil, cfg.xrefStream, w.mediaBoxDefault(cfg)); err != nil {
		return nil, fmt.Errorf("appending the document timestamp: %w", err)
	}
	return buf.Bytes(), nil
}

// reopenBytes opens data, a document written from r's, as [Reader.reopenFile]
// opens a file.
func (r *Reader) reopenBytes(ctx context.Context, data []byte) (*Reader, error) {
	next, err := Open(ctx, bytes.NewReader(data), int64(len(data)), r.reopenOptions()...)
	if err != nil {
		return nil, err
	}
	next.adoptConfiguration(r)
	return next, nil
}

// docTimestamper computes a document timestamp's /Contents: the RFC 3161
// token of the authority params names over the digest of the bytes its
// /ByteRange covers (ISO 32000-2, 12.8.5).
type docTimestamper struct{}

// Sign returns the authority's token over digest.
func (docTimestamper) Sign(ctx context.Context, digest []byte, params mutations.SignParams) ([]byte, error) {
	token, err := signature.RequestTimestamp(ctx, params.TimestampURL, digest)
	if err != nil {
		return nil, fmt.Errorf("timestamping the document: %w", err)
	}
	return token, nil
}

// Certificate returns nil: the authority's certificate is known only from
// its token.
func (docTimestamper) Certificate() *x509.Certificate { return nil }

// Chain returns nil, for the reason [docTimestamper.Certificate] does.
func (docTimestamper) Chain() []*x509.Certificate { return nil }

// EstimatedSize returns the room kept for the token.
func (docTimestamper) EstimatedSize(mutations.SignParams) int { return timestampTokenReserve }
