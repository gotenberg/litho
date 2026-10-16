package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/gotenberg/litho/pkg/pdf"
)

// The inventory counts mutate-sign emits beside its failure counts. They
// count what the run reached, not what failed, so they are not cost metrics:
// see uncomparedMetrics in baseline.go.
const (
	// metricFieldSigned counts the unsigned signature fields of the input
	// signed in place, each in a write of its own.
	metricFieldSigned = "field_signed"
	// metricFieldRefused counts the unsigned signature fields the Writer
	// refused to sign for a reason the field states: a /Lock action ISO
	// 32000-1, Table 233 does not define, or a seed value requirement the
	// signature cannot meet (Table 234).
	metricFieldRefused = "field_refused"
)

// maxSignFieldTrials caps the unsigned signature fields mutate-sign signs in
// writes of their own, one PDF. Anti-cost guard, not spec-mandated: every
// trial is a full incremental write and a verification, and a real form holds
// a handful of signature fields.
//
// # Corpus evidence
//
//   - litho: siglock_2b holds the most unsigned signature fields, five;
//     sigcms_2b and sigtagged_ua1 hold one each, and no other fixture holds
//     one.
const maxSignFieldTrials = 8

// maxQPDFRefChain caps the references [qpdfForm.resolve] follows from one
// value. Anti-DoS guard, not spec-mandated: an object that only names another
// object is rare, and a crafted chain could otherwise loop.
const maxQPDFRefChain = 32

// signFieldTrial is one unsigned signature field of the input, signed in a
// write of its own: output holds the signed bytes, or err the Writer's error.
// dropped are the input's signatures the write left out (see
// [pdf.Writer.DroppedSignatures]).
type signFieldTrial struct {
	name    string
	output  []byte
	err     error
	dropped []pdf.DroppedSignature
}

// qpdfFormField is one field of qpdf --json's "acroform" key: qpdf lists the
// terminal fields, with the field flags they inherit (ISO 32000-1, 12.7.3.1)
// and their value as qpdf's JSON writes it, null when the field has none.
type qpdfFormField struct {
	Object     string          `json:"object"`
	FullName   string          `json:"fullname"`
	FieldType  string          `json:"fieldtype"`
	FieldFlags int             `json:"fieldflags"`
	Value      json.RawMessage `json:"value"`
}

// unset reports whether the field has no value.
func (f qpdfFormField) unset() bool {
	v := bytes.TrimSpace(f.Value)
	return len(v) == 0 || bytes.Equal(v, []byte("null"))
}

// qpdfForm is a document's interactive form and objects as qpdf reads them,
// so mutate-sign does not grade litho's writer with litho's reader.
type qpdfForm struct {
	// fields are the terminal fields in the order of the field tree, one per
	// fully qualified name.
	fields []qpdfFormField
	// objects maps "N G R" to the object's value, the dictionary of a
	// stream.
	objects map[string]any
}

// readQPDFForm runs qpdf --json over data and reads its "acroform" and
// "qpdf" keys. qpdf exits 3 when it read the file with warnings, and still
// writes the JSON.
func readQPDFForm(ctx context.Context, qpdf string, data []byte) (*qpdfForm, error) {
	tmp, err := os.CreateTemp("", "litho-mutate-*.pdf")
	if err != nil {
		return nil, fmt.Errorf("qpdf staging file: %w", err)
	}
	path := tmp.Name()
	defer os.Remove(path)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return nil, fmt.Errorf("writing the qpdf staging file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("closing the qpdf staging file: %w", err)
	}

	cmd := exec.CommandContext(ctx, qpdf, "--json=2", "--json-key=acroform", "--json-key=qpdf", "--json-stream-data=none", path) //nolint:gosec // qpdf from LookPath, path is our staging file
	stdout, runErr := cmd.Output()
	var exitErr *exec.ExitError
	if runErr != nil && (!errors.As(runErr, &exitErr) || exitErr.ExitCode() != 3) {
		return nil, fmt.Errorf("running qpdf --json: %w", runErr)
	}
	var doc struct {
		AcroForm struct {
			Fields []qpdfFormField `json:"fields"`
		} `json:"acroform"`
		QPDF []json.RawMessage `json:"qpdf"`
	}
	if err := json.Unmarshal(stdout, &doc); err != nil {
		return nil, fmt.Errorf("reading qpdf --json: %w", err)
	}
	if len(doc.QPDF) < 2 {
		return nil, errors.New("qpdf --json lists no objects")
	}
	var raw map[string]struct {
		Value  any `json:"value"`
		Stream *struct {
			Dict any `json:"dict"`
		} `json:"stream"`
	}
	if err := json.Unmarshal(doc.QPDF[1], &raw); err != nil {
		return nil, fmt.Errorf("reading qpdf --json objects: %w", err)
	}

	form := &qpdfForm{objects: make(map[string]any, len(raw))}
	for key, obj := range raw {
		ref, ok := strings.CutPrefix(key, "obj:")
		if !ok {
			continue
		}
		if obj.Stream != nil {
			form.objects[ref] = obj.Stream.Dict
		} else {
			form.objects[ref] = obj.Value
		}
	}
	seen := make(map[string]bool)
	for _, f := range doc.AcroForm.Fields {
		// qpdf lists a field once per widget.
		if seen[f.FullName] {
			continue
		}
		seen[f.FullName] = true
		form.fields = append(form.fields, f)
	}
	return form, nil
}

// qpdfRef matches a reference as qpdf's JSON writes it.
var qpdfRef = regexp.MustCompile(`^\d+ \d+ R$`)

// resolve returns v with the references it names followed.
func (q *qpdfForm) resolve(v any) any {
	for range maxQPDFRefChain {
		s, ok := v.(string)
		if !ok || !qpdfRef.MatchString(s) {
			return v
		}
		v = q.objects[s]
	}
	return nil
}

// dict returns v resolved as a dictionary, nil when it is none.
func (q *qpdfForm) dict(v any) map[string]any {
	d, _ := q.resolve(v).(map[string]any)
	return d
}

// name returns v resolved as a name, without its slash, "" when it is none.
func (q *qpdfForm) name(v any) string {
	s, _ := q.resolve(v).(string)
	n, ok := strings.CutPrefix(s, "/")
	if !ok {
		return ""
	}
	return n
}

// text returns v resolved as a string, ok false when it is none. qpdf writes
// a string it can read as text "u:" and the text, any other "b:" and its
// bytes in hexadecimal.
func (q *qpdfForm) text(v any) (string, bool) {
	s, _ := q.resolve(v).(string)
	if t, ok := strings.CutPrefix(s, "u:"); ok {
		return t, true
	}
	if h, ok := strings.CutPrefix(s, "b:"); ok {
		b, err := hex.DecodeString(h)
		if err != nil {
			return "", false
		}
		return string(b), true
	}
	return "", false
}

// integer returns v resolved as a number, 0 when it is none.
func (q *qpdfForm) integer(v any) int {
	n, _ := q.resolve(v).(float64)
	return int(n)
}

// names returns the names of the array v.
func (q *qpdfForm) names(v any) []string {
	arr, _ := q.resolve(v).([]any)
	var out []string
	for _, e := range arr {
		if n := q.name(e); n != "" {
			out = append(out, n)
		}
	}
	return out
}

// texts returns the strings of the array v.
func (q *qpdfForm) texts(v any) []string {
	arr, _ := q.resolve(v).([]any)
	var out []string
	for _, e := range arr {
		if t, ok := q.text(e); ok {
			out = append(out, t)
		}
	}
	return out
}

// field returns the field named name, ok false when the form has none.
func (q *qpdfForm) field(name string) (qpdfFormField, bool) {
	for _, f := range q.fields {
		if f.FullName == name {
			return f, true
		}
	}
	return qpdfFormField{}, false
}

// unsignedSignatureFields returns the names of the form's unsigned signature
// fields, in the order of the field tree. A field whose seed values name a
// timestamp authority is left out: litho would ask that authority for a
// token (ISO 32000-1, Table 234, TimeStamp), and the gate reaches no network.
func (q *qpdfForm) unsignedSignatureFields() []string {
	var out []string
	for _, f := range q.fields {
		if f.FieldType != "/Sig" || !f.unset() {
			continue
		}
		if sv := q.dict(q.dict(f.Object)["/SV"]); sv != nil && sv["/TimeStamp"] != nil {
			continue
		}
		out = append(out, f.FullName)
	}
	return out
}

// signFields reads the input's form with qpdf and signs each of its unsigned
// signature fields, up to [maxSignFieldTrials], in a write of its own. It
// returns the form, nil when qpdf is absent or cannot read the input, and the
// trials. A source litho refuses to sign at all ends the trials: the write
// into a new field refuses too, and skips the PDF.
func (f *mutationFeature) signFields(ctx context.Context, in *mutationInput) (*qpdfForm, []signFieldTrial) {
	if f.qpdf == "" {
		return nil, nil
	}
	form, err := readQPDFForm(ctx, f.qpdf, in.data)
	if err != nil {
		return nil, nil
	}
	var trials []signFieldTrial
	for _, name := range form.unsignedSignatureFields() {
		if len(trials) == maxSignFieldTrials || ctx.Err() != nil {
			break
		}
		t := f.signInto(ctx, in.data, name)
		if errors.Is(t.err, pdf.ErrSignFullRewriteIncompatible) {
			break
		}
		trials = append(trials, t)
	}
	return form, trials
}

// signInto signs data in the field named name.
func (f *mutationFeature) signInto(ctx context.Context, data []byte, name string) signFieldTrial {
	t := signFieldTrial{name: name}
	src, err := pdf.Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.err = fmt.Errorf("opening the input: %w", err)
		return t
	}
	defer src.Close()
	w := pdf.NewWriter(src)
	defer w.Close()
	w.Sign(f.signer.Signer, pdf.WithSignatureField(name))
	var buf bytes.Buffer
	if err := w.WriteTo(ctx, &buf); err != nil {
		t.err = fmt.Errorf("signing in the field %q: %w", name, err)
		return t
	}
	t.output, t.dropped = buf.Bytes(), w.DroppedSignatures()
	return t
}

// lockActions are the actions ISO 32000-1, Table 233 defines for a signature
// field lock dictionary.
//
//nolint:gochecknoglobals // static spec table
var lockActions = []string{"All", "Include", "Exclude"}

// fieldLock is a signature field's /Lock as qpdf reads it.
type fieldLock struct {
	present bool
	action  string
	fields  []string
}

// readLock reads the /Lock of the field named name.
func (q *qpdfForm) readLock(name string) fieldLock {
	f, _ := q.field(name)
	lock := q.dict(q.dict(f.Object)["/Lock"])
	if lock == nil {
		return fieldLock{}
	}
	return fieldLock{present: true, action: q.name(lock["/Action"]), fields: q.texts(lock["/Fields"])}
}

// locks reports whether the lock makes the field named name read-only: ISO
// 32000-1, Table 233 locks every field for All, the fields named and their
// descendants for Include, and every other field for Exclude.
func (l fieldLock) locks(name string) bool {
	named := slices.ContainsFunc(l.fields, func(n string) bool {
		return name == n || strings.HasPrefix(name, n+".")
	})
	switch l.action {
	case "All":
		return true
	case "Include":
		return named
	case "Exclude":
		return !named
	}
	return false
}

// The /Ff bits of a seed value dictionary (ISO 32000-1, 12.7.4.5, Table
// 234; ISO 32000-2 adds bits 8 and 9) the gate judges, and the nine the two
// parts define.
const (
	seedFfFilter       = 1 << 0
	seedFfSubFilter    = 1 << 1
	seedFfV            = 1 << 2
	seedFfReasons      = 1 << 3
	seedFfDigestMethod = 1 << 6
	seedFfDefined      = 1<<9 - 1
)

// seedEntries are the seed value dictionary entries [judgeSeed] judges. A
// dictionary with another entry, a certificate or MDP requirement among them,
// is one the gate does not judge a refusal of.
//
//nolint:gochecknoglobals // static spec table
var seedEntries = []string{"/Type", "/Ff", "/Filter", "/SubFilter", "/V", "/Reasons", "/DigestMethod"}

// signerSubFilters are the subfilters litho's signer writes: ISO 32000-1,
// 12.8.3.3 and ISO 32000-2, 12.8.3.4.
//
//nolint:gochecknoglobals // static spec table
var signerSubFilters = []string{"adbe.pkcs7.detached", "ETSI.CAdES.detached"}

// maxSeedParserVersion is the seed value dictionary version whose entries
// litho reads every one of: ISO 32000-1, Table 234 defines 1 and 2.
const maxSeedParserVersion = 2

// seedJudgement is what the gate makes of a signature field's seed values
// (ISO 32000-1, 12.7.4.5, Table 234).
type seedJudgement struct {
	// judged is false when the dictionary holds an entry or a requirement
	// bit the gate does not judge, so a refusal of it is taken as stated.
	judged bool
	// unmet names a requirement litho's signature cannot meet, "" when it
	// meets them all.
	unmet string
	// subFilter is the subfilter the signature must carry, "" when the seed
	// values leave it to the signer.
	subFilter string
	// reasons are the reasons the signature's /Reason must be one of, nil
	// when they ask none.
	reasons []string
}

// judgeSeed judges the seed values of the field named name. The signature the
// gate asks for names no subfilter, reason or timestamp authority, and litho
// signs over SHA-256 with the Adobe.PPKLite handler.
func (q *qpdfForm) judgeSeed(name string) (seedJudgement, bool) {
	f, _ := q.field(name)
	sv := q.dict(q.dict(f.Object)["/SV"])
	if sv == nil {
		return seedJudgement{}, false
	}
	ff := q.integer(sv["/Ff"])
	j := seedJudgement{judged: ff&^seedFfDefined == 0}
	for key := range sv {
		if !slices.Contains(seedEntries, key) {
			j.judged = false
		}
	}
	unmet := func(reason string) {
		if j.unmet == "" {
			j.unmet = reason
		}
	}

	if filter := q.name(sv["/Filter"]); ff&seedFfFilter != 0 && filter != "" && filter != "Adobe.PPKLite" {
		unmet("it requires the " + filter + " handler")
	}
	if v := q.integer(sv["/V"]); ff&seedFfV != 0 && v > maxSeedParserVersion {
		unmet(fmt.Sprintf("it requires a seed value parser of version %d", v))
	}
	// Table 234, SubFilter: the first name the signer supports is the one to
	// use, and a required list the signer supports none of cannot be met.
	if subFilters := q.names(sv["/SubFilter"]); len(subFilters) > 0 {
		if i := slices.IndexFunc(subFilters, func(n string) bool { return slices.Contains(signerSubFilters, n) }); i >= 0 {
			j.subFilter = subFilters[i]
		} else if ff&seedFfSubFilter != 0 {
			unmet(fmt.Sprintf("it requires one of the subfilters %v", subFilters))
		}
	}
	if methods := q.names(sv["/DigestMethod"]); ff&seedFfDigestMethod != 0 && len(methods) > 0 && !slices.Contains(methods, "SHA256") {
		unmet(fmt.Sprintf("it requires one of the digests %v", methods))
	}
	if reasons := q.texts(sv["/Reasons"]); ff&seedFfReasons != 0 && len(reasons) > 0 {
		j.reasons = reasons
	}
	return j, true
}

// verifySignFields checks every trial of [mutationFeature.signFields]: a
// signed field against what signing it promises (see
// [mutationFeature.checkSignedField]), a refusal against the field's own
// /Lock and seed values. It returns the failures and the inventory counts.
func (f *mutationFeature) verifySignFields(ctx context.Context, in *mutationInput, form *qpdfForm, trials []signFieldTrial) ([]mutationFailure, map[string]int) {
	counts := map[string]int{metricFieldSigned: 0, metricFieldRefused: 0}
	var failures []mutationFailure
	for _, t := range trials {
		if t.err == nil {
			counts[metricFieldSigned]++
			failures = append(failures, f.checkSignedField(ctx, in, form, t)...)
			continue
		}
		if refusalExplained(form, t) {
			counts[metricFieldRefused]++
			continue
		}
		metric := metricWriteFailed
		if errors.Is(t.err, pdf.ErrSignatureFieldUnavailable) || errors.Is(t.err, pdf.ErrSeedValueUnmet) {
			metric = metricPromiseBroken
		}
		failures = append(failures, mutationFailure{Metric: metric, Detail: t.err.Error()})
	}
	return failures, counts
}

// refusalExplained reports whether the field the trial names states a reason
// for the Writer's refusal. ISO 32000-1, 12.7.4.5 lets a
// signature field refuse a signature through an undefined /Lock action
// (Table 233) or a seed value requirement (Table 234). The field is unsigned
// and a signature field, so neither error has another cause.
func refusalExplained(form *qpdfForm, t signFieldTrial) bool {
	switch {
	case errors.Is(t.err, pdf.ErrSignatureFieldUnavailable):
		lock := form.readLock(t.name)
		return lock.present && !slices.Contains(lockActions, lock.action)
	case errors.Is(t.err, pdf.ErrSeedValueUnmet):
		j, ok := form.judgeSeed(t.name)
		return ok && (!j.judged || j.unmet != "")
	}
	return false
}

// checkSignedField checks what signing an unsigned signature field in place
// promises (ISO 32000-1, 12.7.4.5, and [pdf.WithSignatureField]):
//
//   - litho's verifier accepts the new signature, made by the run's key, in
//     that field, beside every signature the input held (signature_invalid);
//   - the form keeps its fields, no new one taking the signature, and the
//     field holds a signature dictionary (promise_broken);
//   - every field the field's /Lock names turns read-only, and no other
//     field's flags change (Table 233, Table 221), and the signature records
//     the lock in a FieldMDP transform (12.8.2.4, Table 256), or none without
//     a lock (promise_broken);
//   - the signature carries the subfilter and the reason the seed values
//     choose, and the field's seed values hold no requirement it cannot meet
//     (Table 234, promise_broken).
func (f *mutationFeature) checkSignedField(ctx context.Context, in *mutationInput, form *qpdfForm, t signFieldTrial) []mutationFailure {
	broken := func(format string, args ...any) mutationFailure {
		return mutationFailure{Metric: metricPromiseBroken, Detail: fmt.Sprintf("field %q: ", t.name) + fmt.Sprintf(format, args...)}
	}
	var failures []mutationFailure
	if failure, ok := f.checkFieldSignature(ctx, in, t); ok {
		failures = append(failures, failure)
	}

	out, err := readQPDFForm(ctx, f.qpdf, t.output)
	if err != nil {
		return append(failures, broken("qpdf cannot read the signed output: %v", err))
	}
	if added, lost := fieldNameDiff(form, out, t.dropped); len(added)+len(lost) > 0 {
		failures = append(failures, broken("signing in place added the fields %q and lost %q", added, lost))
	}
	signed, ok := out.field(t.name)
	var value any
	if err := json.Unmarshal(signed.Value, &value); err != nil {
		value = nil
	}
	sig := out.dict(value)
	if !ok || sig == nil {
		return append(failures, broken("the field holds no signature dictionary"))
	}

	lock := form.readLock(t.name)
	for _, inField := range form.fields {
		outField, ok := out.field(inField.FullName)
		if !ok {
			continue
		}
		want := inField.FieldFlags
		if lock.locks(inField.FullName) {
			// Table 221: bit 1 of /Ff is ReadOnly.
			want |= 1
		}
		if outField.FieldFlags != want {
			failures = append(failures, broken("field %q has the flags %d, want %d under the /Lock %s %q",
				inField.FullName, outField.FieldFlags, want, lock.action, lock.fields))
			break
		}
	}
	if msg := checkFieldMDP(out, sig, lock); msg != "" {
		failures = append(failures, broken("%s", msg))
	}

	if j, ok := form.judgeSeed(t.name); ok {
		if j.unmet != "" {
			failures = append(failures, broken("signed although %s", j.unmet))
		}
		if got := out.name(sig["/SubFilter"]); j.subFilter != "" && got != j.subFilter {
			failures = append(failures, broken("the signature's subfilter is %s, the seed values choose %s", got, j.subFilter))
		}
		reason, _ := out.text(sig["/Reason"])
		if j.reasons != nil && !slices.Contains(j.reasons, reason) {
			failures = append(failures, broken("the signature's reason is %q, the seed values require one of %q", reason, j.reasons))
		}
	}
	return failures
}

// checkFieldSignature checks litho's verifier accepts a signature made by the
// run's key in the field the trial signed, beside every signature the input
// held, or alone when the write dropped them (see [verifySign]). ok is false
// when it does.
func (f *mutationFeature) checkFieldSignature(ctx context.Context, in *mutationInput, t signFieldTrial) (mutationFailure, bool) {
	name := t.name
	invalid := func(format string, args ...any) (mutationFailure, bool) {
		return mutationFailure{Metric: metricSignatureInvalid, Detail: fmt.Sprintf("field %q: ", name) + fmt.Sprintf(format, args...)}, true
	}
	r, err := pdf.Open(ctx, bytes.NewReader(t.output), int64(len(t.output)))
	if err != nil {
		return invalid("reopening: %v", err)
	}
	defer r.Close()
	results, err := r.VerifySignatures(ctx, f.signer.roots())
	if err != nil {
		return invalid("verifying: %v", err)
	}
	want := in.signatures + 1
	if len(t.dropped) > 0 {
		want = 1
	}
	if len(results) != want {
		return invalid("%d signatures, want %d", len(results), want)
	}
	for _, v := range results {
		if v.FieldName != name {
			continue
		}
		if v.SignerCN == corpusSignerCN && v.HashValid && v.CertChainValid {
			return mutationFailure{}, false
		}
		return invalid("signer %q, hash %t, chain %t: %s", v.SignerCN, v.HashValid, v.CertChainValid, v.ErrorMessage)
	}
	return invalid("no signature in the field")
}

// fieldNameDiff returns the field names out has and in has not, and the ones
// in has and out has not, each sorted. The field of a signature the write
// dropped goes with it (see [pdf.DroppedSignature]), so it is not lost.
func fieldNameDiff(in, out *qpdfForm, dropped []pdf.DroppedSignature) (added, lost []string) {
	names := func(q *qpdfForm) map[string]bool {
		m := make(map[string]bool, len(q.fields))
		for _, f := range q.fields {
			m[f.FullName] = true
		}
		return m
	}
	inNames, outNames := names(in), names(out)
	for n := range outNames {
		if !inNames[n] {
			added = append(added, n)
		}
	}
	for n := range inNames {
		if !outNames[n] && !slices.ContainsFunc(dropped, func(d pdf.DroppedSignature) bool { return d.Field == n }) {
			lost = append(lost, n)
		}
	}
	sort.Strings(added)
	sort.Strings(lost)
	return added, lost
}

// checkFieldMDP checks the signature dictionary sig records lock as ISO
// 32000-1, 12.7.4.5 asks: a field lock is recorded in a FieldMDP signature
// reference (12.8.2.4, Tables 253 and 256) of the lock's action and fields.
// Without a lock, the signature carries no FieldMDP reference. It returns
// what fails, "" when nothing does.
func checkFieldMDP(out *qpdfForm, sig map[string]any, lock fieldLock) string {
	refs, _ := out.resolve(sig["/Reference"]).([]any)
	var params map[string]any
	for _, ref := range refs {
		d := out.dict(ref)
		if out.name(d["/TransformMethod"]) == "FieldMDP" {
			params = out.dict(d["/TransformParams"])
			break
		}
	}
	switch {
	case !lock.present && params != nil:
		return "the signature records a FieldMDP transform and the field has no /Lock"
	case !lock.present:
		return ""
	case params == nil:
		return "the signature records no FieldMDP transform of the field's /Lock"
	}
	if got := out.name(params["/Action"]); got != lock.action {
		return fmt.Sprintf("the FieldMDP transform's action is %s, the /Lock's %s", got, lock.action)
	}
	if lock.action != "All" && !slices.Equal(out.texts(params["/Fields"]), lock.fields) {
		return fmt.Sprintf("the FieldMDP transform names the fields %q, the /Lock %q", out.texts(params["/Fields"]), lock.fields)
	}
	return ""
}

// applySign signs the input. Each unsigned signature field of the input is
// first signed in a write of its own (see [mutationFeature.signFields]), so
// every /Lock and seed value path a fixture holds runs; the mutation then
// signs in the first field that took a signature, as [pdf.WithSignatureField]
// signs an existing field, or in a new field when none did.
func applySign(ctx context.Context, f *mutationFeature, in *mutationInput, w *pdf.Writer) (mutationPlan, error) {
	plan := identityPlan(in)
	plan.counts = make(map[string]int)
	plan.signForm, plan.signTrials = f.signFields(ctx, in)
	for _, t := range plan.signTrials {
		if t.err == nil {
			plan.signField = t.name
			break
		}
	}
	if plan.signField != "" {
		w.Sign(f.signer.Signer, pdf.WithSignatureField(plan.signField))
	} else {
		w.Sign(f.signer.Signer)
	}
	return plan, nil
}
