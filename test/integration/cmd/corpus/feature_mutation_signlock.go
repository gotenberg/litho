package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"sync"

	"github.com/gotenberg/litho/internal/pkitest"
	"github.com/gotenberg/litho/pkg/pdf"
)

// lockProbeField names the new field the follow-ups of a locked signature
// sign in. No fixture holds a field of that name, so a signature in it makes
// a new root field.
const lockProbeField = "LithoLockProbe"

// objectHeader matches the header of an indirect object an incremental update
// defines (ISO 32000-1, 7.3.10).
var objectHeader = regexp.MustCompile(`(?m)(?:^|[\r\n])\s*(\d+) (\d+) obj\b`)

// checkLockFollowUps checks what may follow the signature of a field whose
// /Lock the trial honoured (ISO 32000-1, 12.7.4.5, Table 233): signed holds
// the trial's output as qpdf reads it.
//
//   - Another signature, in each other unsigned signature field of the input
//     and in a new field, is refused with [pdf.ErrSignatureFieldLocked] when
//     the lock reaches that field, since signing it would change a field the
//     FieldMDP transform locks (12.8.2.4, Table 256), and signs otherwise.
//   - A document timestamp in a new field (ISO 32000-2, 12.8.5; ETSI EN 319
//     142-1, 5.4) is written, and changes no field of the signed revision (see
//     [judgeTimestampUpdate]).
//
// Each failure counts promise_broken.
func (f *mutationFeature) checkLockFollowUps(ctx context.Context, in *mutationInput, form, signed *qpdfForm, t signFieldTrial, lock fieldLock) []mutationFailure {
	broken := func(format string, args ...any) mutationFailure {
		return mutationFailure{Metric: metricPromiseBroken, Detail: fmt.Sprintf("after the /Lock %s of field %q: ", lock.action, t.name) + fmt.Sprintf(format, args...)}
	}
	var failures []mutationFailure
	targets := slices.DeleteFunc(form.unsignedSignatureFields(), func(n string) bool { return n == t.name })
	for _, name := range append(targets, lockProbeField) {
		next := f.signInto(ctx, t.output, name)
		if msg := judgeLockedSign(form, lock, name, next.err); msg != "" {
			failures = append(failures, broken("%s", msg))
		}
	}

	stamped, err := timestampOnce(ctx, t.output)
	if err != nil {
		return append(failures, broken("the document timestamp was refused: %v", err))
	}
	after, err := readQPDFForm(ctx, f.qpdf, stamped)
	if err != nil {
		return append(failures, broken("qpdf cannot read the timestamped output: %v", err))
	}
	if msg := judgeTimestampUpdate(t.output, stamped, signed, after); msg != "" {
		failures = append(failures, broken("%s", msg))
	}
	if msg := f.judgeTimestampSignatures(ctx, in, t, stamped); msg != "" {
		failures = append(failures, broken("%s", msg))
	}
	return failures
}

// judgeLockedSign judges err, what signing the field named name returned
// after a signature under lock: the lock reaching the field asks for
// [pdf.ErrSignatureFieldLocked], and a field it does not reach signs, unless
// the field's own seed values or /Lock explain the refusal
// ([refusalExplained]). It returns what fails, "" when nothing does.
func judgeLockedSign(form *qpdfForm, lock fieldLock, name string, err error) string {
	if lock.locks(name) {
		if !errors.Is(err, pdf.ErrSignatureFieldLocked) {
			return fmt.Sprintf("signing field %q, which the lock reaches, returned %v, want ErrSignatureFieldLocked", name, err)
		}
		return ""
	}
	if err != nil && !refusalExplained(form, signFieldTrial{name: name, err: err}) {
		return fmt.Sprintf("signing field %q, which the lock does not reach, was refused: %v", name, err)
	}
	return ""
}

// timestampOnce adds a document timestamp to data in a new field, from a
// timestamp authority of its own on the loopback interface that stops when it
// returns, so the gate reaches no network.
func timestampOnce(ctx context.Context, data []byte) ([]byte, error) {
	var mu sync.Mutex
	var reported []string
	tsa, err := pkitest.Start(func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		reported = append(reported, fmt.Sprintf(format, args...))
	})
	if err != nil {
		return nil, fmt.Errorf("starting the timestamp authority: %w", err)
	}
	defer tsa.Close()
	src, err := pdf.Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("opening the signed output: %w", err)
	}
	defer src.Close()
	w := pdf.NewWriter(src)
	defer w.Close()
	w.Timestamp(tsa.TSAURL())
	var buf bytes.Buffer
	if err := w.WriteTo(ctx, &buf); err != nil {
		mu.Lock()
		defer mu.Unlock()
		return nil, fmt.Errorf("timestamping (authority reports %q): %w", reported, err)
	}
	return buf.Bytes(), nil
}

// judgeTimestampUpdate judges the document timestamp stamped appends to
// signed, read by qpdf as before and after. The update is incremental (ISO
// 32000-1, 7.5.6), so signed's bytes and its signature stand. It redefines
// none of signed's objects but the catalog and the form dictionary, which a
// new field is listed in (12.7.2), so no field the lock covers, no widget and
// no appearance changes (12.8.2.4, Table 256). Every field keeps its flags
// and value, and the one field it adds holds a document timestamp. It
// returns what fails, "" when nothing does.
func judgeTimestampUpdate(signed, stamped []byte, before, after *qpdfForm) string {
	if !bytes.HasPrefix(stamped, signed) {
		return "the document timestamp is not an incremental update of the signed output"
	}
	update := stamped[len(signed):]
	if bytes.Contains(update, []byte("/ObjStm")) {
		return "the document timestamp's update packs objects in an object stream, which the gate does not read"
	}
	allowed := before.catalogRefs()
	var rewritten []string
	for _, m := range objectHeader.FindAllSubmatch(update, -1) {
		ref := string(m[1]) + " " + string(m[2]) + " R"
		if _, existed := before.objects[ref]; existed && !slices.Contains(allowed, ref) {
			rewritten = append(rewritten, ref)
		}
	}
	if len(rewritten) > 0 {
		sort.Strings(rewritten)
		return fmt.Sprintf("the document timestamp's update rewrites %q, objects of the signed revision other than the catalog and the form %q", rewritten, allowed)
	}
	for _, in := range before.fields {
		out, ok := after.field(in.FullName)
		if !ok {
			return fmt.Sprintf("the document timestamp lost field %q", in.FullName)
		}
		if out.FieldFlags != in.FieldFlags || !bytes.Equal(bytes.TrimSpace(out.Value), bytes.TrimSpace(in.Value)) {
			return fmt.Sprintf("the document timestamp changed field %q: flags %d to %d, value %s to %s", in.FullName, in.FieldFlags, out.FieldFlags, in.Value, out.Value)
		}
	}
	// The form lists the new field (ISO 32000-1, 12.7.2, Table 218), where
	// qpdf's field list follows the widgets on the pages, and the invisible
	// field is on none.
	var added []any
	listed := before.rootFields()
	for _, ref := range after.rootFields() {
		if !slices.Contains(listed, ref) {
			added = append(added, ref)
		}
	}
	if len(added) != 1 {
		return fmt.Sprintf("the document timestamp added %d root fields to the form, want one", len(added))
	}
	if after.name(after.dict(after.dict(added[0])["/V"])["/Type"]) != "DocTimeStamp" {
		return fmt.Sprintf("the field %v the document timestamp added holds no /DocTimeStamp dictionary", added[0])
	}
	return ""
}

// rootFields returns the entries of the form's /Fields, references as qpdf
// writes them.
func (q *qpdfForm) rootFields() []any {
	root, _ := q.dict(q.objects[qpdfTrailer])["/Root"].(string)
	fields, _ := q.resolve(q.dict(q.dict(root)["/AcroForm"])["/Fields"]).([]any)
	return fields
}

// judgeTimestampSignatures checks litho's verifier reads one more signature
// in stamped than the trial's output holds, and still accepts the trial's
// signature in its field (see [mutationFeature.checkFieldSignature]). It
// returns what fails, "" when nothing does.
func (f *mutationFeature) judgeTimestampSignatures(ctx context.Context, in *mutationInput, t signFieldTrial, stamped []byte) string {
	r, err := pdf.Open(ctx, bytes.NewReader(stamped), int64(len(stamped)))
	if err != nil {
		return fmt.Sprintf("reopening the timestamped output: %v", err)
	}
	defer r.Close()
	results, err := r.VerifySignatures(ctx, f.signer.roots())
	if err != nil {
		return fmt.Sprintf("verifying the timestamped output: %v", err)
	}
	want := in.signatures + 2
	if len(t.dropped) > 0 {
		want = 2
	}
	if len(results) != want {
		return fmt.Sprintf("the timestamped output holds %d signatures, want %d", len(results), want)
	}
	for _, v := range results {
		if v.FieldName == t.name && (v.SignerCN != corpusSignerCN || !v.HashValid || !v.CertChainValid) {
			return fmt.Sprintf("the timestamp left the signature in field %q invalid: hash %t, chain %t: %s", t.name, v.HashValid, v.CertChainValid, v.ErrorMessage)
		}
	}
	return ""
}
