package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/pdf"
)

// mutationSpec is one Writer mutation a mutate-* feature runs, with the
// promises its output is checked against.
type mutationSpec struct {
	// name is the feature's suffix: "stamp" is mutate-stamp.
	name string
	// needsDefaultText reads each input page without [pdf.WithHiddenText]
	// too, for a check that tells artifacts apart.
	needsDefaultText bool
	// needsSigner has Prepare build the run's [corpusSigner].
	needsSigner bool
	// needsTagState validates the input at PDF/UA-1 before the write, to
	// learn whether it has a structure tree.
	needsTagState bool
	// extraMetrics are the metrics beyond the six every spec emits.
	extraMetrics []string
	// refusals are the errors the Writer documents as refusing this mutation
	// on some inputs. A refusal skips the PDF rather than failing it.
	refusals []error
	// apply queues the mutation on w and returns what the output promises.
	apply func(ctx context.Context, f *mutationFeature, in *mutationInput, w *pdf.Writer) (mutationPlan, error)
	// verify checks the page text and the mutation's own promises on the
	// reopened output. The page count is checked before it.
	verify func(ctx context.Context, f *mutationFeature, in *mutationInput, out *mutationOutputDoc, plan mutationPlan) []mutationFailure
	// allowance returns how far the rule set may move at one profile.
	allowance func(rc ruleContext) ruleAllowance
}

// ruleContext is what an allowance may depend on: the profile, the plan, and
// the input's and the output's rule sets there.
type ruleContext struct {
	profile string
	plan    mutationPlan
	in, out []string
}

// metrics returns every metric the spec emits.
func (s mutationSpec) metrics() []string {
	return append([]string{
		metricWriteFailed, metricReopenFailed, metricQPDFFailed,
		metricPagesWrong, metricTextWrong, metricRulesetChanged,
	}, s.extraMetrics...)
}

// mutationInput is what the checks need to know about the input.
type mutationInput struct {
	data  []byte
	pages int
	// text holds each page read with [pdf.WithHiddenText], and defaultText
	// each page read with no option, when the spec asks for it.
	text        []pageText
	defaultText []pageText
	// boxes holds each page's crop box.
	boxes      [][4]float64
	signatures int
	// untagged is set, when the spec asks, for an input with neither a
	// structure tree nor /MarkInfo /Marked true, which AutoTag leaves alone.
	untagged bool
}

// mutationOutputDoc is the reopened output.
type mutationOutputDoc struct {
	reader *pdf.Reader
	data   []byte
	pages  int
}

// mutationPlan is what a mutation promises of its output.
type mutationPlan struct {
	// pages is the output's page count.
	pages int
	// pageSource maps each output page to the input page whose text it
	// carries, 0-based.
	pageSource []int
	// deleted is set when the mutation deleted a page, whose structure
	// elements go with it.
	deleted bool
	// password opens an encrypted output.
	password string
	// release frees what the mutation holds until the write, a second Reader.
	release func()
}

// pageText is one page's text, normalized with [normalizeContent]. err is
// set when litho could not read the page, which no check then trusts.
type pageText struct {
	text string
	err  string
}

// hiddenText reads every text category, the ones a mutation must preserve
// whether or not a viewer paints them.
//
//nolint:gochecknoglobals // static option list
var hiddenText = []pdf.ExtractTextOption{pdf.WithHiddenText()}

// readPageTexts reads pages 1 to n. A page litho needs OCR for reads as
// empty, as it does on both sides of every comparison.
func readPageTexts(ctx context.Context, r *pdf.Reader, n int, opts ...pdf.ExtractTextOption) []pageText {
	out := make([]pageText, n)
	for i := range n {
		text, err := r.ExtractText(ctx, i+1, opts...)
		if err != nil && !errors.Is(err, pdf.ErrRequiresOCR) {
			out[i].err = err.Error()
			continue
		}
		out[i].text = normalizeContent(text)
	}
	return out
}

// readMutationInput reads the input's page count, text, boxes and signatures.
func readMutationInput(ctx context.Context, r *pdf.Reader, data []byte, spec mutationSpec) (*mutationInput, error) {
	n, err := r.PageCount(ctx)
	if err != nil {
		return nil, fmt.Errorf("page count: %w", err)
	}
	in := &mutationInput{data: data, pages: n, text: readPageTexts(ctx, r, n, hiddenText...)}
	if spec.needsDefaultText {
		in.defaultText = readPageTexts(ctx, r, n)
	}
	if spec.needsTagState {
		rules, err := uaRules(ctx, r)
		if err != nil {
			return nil, err
		}
		in.untagged = containsAll(rules, structTreeRules[familyUA1]) && containsAll(rules, markInfoRules[familyUA1])
	}
	in.boxes = make([][4]float64, n)
	for i := range n {
		page, err := r.Page(ctx, i+1)
		if err != nil {
			return nil, fmt.Errorf("page %d: %w", i+1, err)
		}
		in.boxes[i] = page.CropBox
		if in.boxes[i] == ([4]float64{}) {
			in.boxes[i] = [4]float64{0, 0, page.Width, page.Height}
		}
	}
	sigs, err := r.ExtractSignatures(ctx)
	if err != nil {
		return nil, fmt.Errorf("signatures: %w", err)
	}
	in.signatures = len(sigs)
	return in, nil
}

// identityPlan promises the input's pages, each carrying its own text.
func identityPlan(in *mutationInput) mutationPlan {
	plan := mutationPlan{pages: in.pages, pageSource: make([]int, in.pages)}
	for i := range plan.pageSource {
		plan.pageSource[i] = i
	}
	return plan
}

// compareText checks output pages from on against the input pages
// pageSource maps them to. With superset, an output page may carry more text
// than its input page, in any order: a flattened field adds its value. It
// returns one failure, for the first page that breaks the promise.
func compareText(in *mutationInput, out []pageText, pageSource []int, from int, superset bool) []mutationFailure {
	for i := from; i < len(pageSource) && i < len(out); i++ {
		src := pageSource[i]
		if src >= len(in.text) || in.text[src].err != "" {
			continue
		}
		want, got := in.text[src], out[i]
		if got.err != "" {
			return []mutationFailure{{Metric: metricTextWrong, Detail: fmt.Sprintf("page %d: %s", i+1, got.err)}}
		}
		if superset && runesContain(got.text, want.text) || !superset && got.text == want.text {
			continue
		}
		return []mutationFailure{{Metric: metricTextWrong, Detail: textDetail(i+1, src+1, want.text, got.text)}}
	}
	return nil
}

// textDetail names the first place two page texts differ.
func textDetail(outPage, inPage int, want, got string) string {
	pos, wantSnippet, gotSnippet := textDiffSummary(want, got, 40)
	return fmt.Sprintf("page %d (input page %d) differs at rune %d: want %q, got %q", outPage, inPage, pos, wantSnippet, gotSnippet)
}

// runesContain reports whether s holds every rune of sub at least as often as
// sub does.
func runesContain(s, sub string) bool {
	counts := make(map[rune]int)
	for _, r := range s {
		counts[r]++
	}
	for _, r := range sub {
		counts[r]--
		if counts[r] < 0 {
			return false
		}
	}
	return true
}

// strictAllowance lets the rule set move only as a rewrite moves it: any
// file-structure rule may pass (see [fileStructureRules]), nothing else may
// change.
func strictAllowance(profileDir string) ruleAllowance {
	return ruleAllowance{mayRemove: fileStructureRules[ruleFamily(profileDir)]}
}

// mutationStampText is the text mutate-stamp draws. Its normalized form is
// what the check looks for on every page.
const mutationStampText = "LITHO MUTATION STAMP"

// mutationStampInset places the stamp this far inside the lower-left corner
// of each page's crop box, in points.
const mutationStampInset = 10

// mutationStampFontSize is the stamp's size in points: small enough to fit a
// narrow page, since text past the crop box is read all the same.
const mutationStampFontSize = 8

// mutationEncryptUser and mutationEncryptOwner are the passwords
// mutate-encrypt encrypts with. A user password makes "opens without it" a
// check rather than the default.
const (
	mutationEncryptUser  = "litho-user"
	mutationEncryptOwner = "litho-owner"
)

// mutationRedactReach is how far past the origin mutate-redact's region
// reaches in every direction, so the region covers everything page 1 draws,
// wherever its boxes sit. It exceeds ISO 32000-1, Annex C's 14,400-unit page
// size limit by far.
const mutationRedactReach = 1e6

// mutationSpecs lists the mutations the gate runs, one mutate-* feature each.
//
// DeletePages refuses to delete the Reader's only page even once an
// InsertPages has queued a second one: mutations.DeletePages compares the
// deletion with the Reader's page count before it reads the staged tree. So
// mutate-pages only appends the copy to a one-page input, and the deletion
// runs on the multi-page ones.
//
// The failures the features count on the code they were written against are
// listed below, so the baseline they set reads as findings rather than noise.
//
// # Corpus evidence
//
//   - litho: 75 of the 92 fixtures have one page, so mutate-pages deletes a
//     page on 17.
//   - litho: 37 fixtures refuse the whole-page redaction by design, 28 with
//     ErrRedactionRefused (content a form XObject draws, an inline image, a
//     stroked path) and 9 with ErrRedactionIncomplete; the 2 encrypted
//     fixtures refuse the signature.
//   - litho imagereach_2b: 6 ruleset_changed cells under mutate-stamp and 6
//     under mutate-pages. veraPDF judges a content stream two pages share once,
//     through the first page's resources, and the stamp's /Contents arrays and
//     the page move change which page that is, so an image the second page
//     draws is judged. veraPDF reports the same rule set on the outputs. Not a
//     mutation defect.
//   - litho a4syntax_4: 1 ruleset_changed cell (PDF/A-4, 6.1.2 test 1) under
//     each of mutate-pages, mutate-flatten and mutate-encrypt: a full rewrite
//     writes a %PDF-1.7 header over a PDF 2.0 source.
//   - litho xrefstm_2b: 1 ruleset_changed cell (PDF/A-1b, 6.7.3 tests 2, 4
//     and 7 pass) under mutate-pages and mutate-encrypt: a full rewrite drops
//     the direct /Info dictionary the source's cross-reference stream holds.
//   - litho complex_2u, forbidden_mix, inlineimage_2b: 3 promise_broken PDFs
//     under mutate-autotag. Their content is already marked, and AutoTag
//     leaves such a document alone by design, which the public API cannot
//     tell the check.
//
//nolint:gochecknoglobals // static feature table
var mutationSpecs = []mutationSpec{
	{
		name:             "stamp",
		needsDefaultText: true,
		apply:            applyStamp,
		verify:           verifyStamp,
		allowance: func(rc ruleContext) ruleAllowance {
			family := ruleFamily(rc.profile)
			a := strictAllowance(rc.profile)
			a.mayAdd = concatRules(fontEmbeddingRules[family], deviceRGBRules[family], textLanguageRules[family])
			a.mayRemove = concatRules(a.mayRemove, explicitResourcesRules[family])
			return a
		},
	},
	{
		name:   "pages",
		apply:  applyPages,
		verify: verifyIdentityText,
		allowance: func(rc ruleContext) ruleAllowance {
			family := ruleFamily(rc.profile)
			a := strictAllowance(rc.profile)
			a.mayRemove = concatRules(a.mayRemove, explicitResourcesRules[family])
			a.mayAdd = signatureDigestRules[family]
			// Deleting a page prunes the structure elements that name it, and
			// a rule only they failed goes with them. The copy that replaces
			// the page carries its content, not its structure.
			a.anyRemove = rc.plan.deleted && tagsContent(rc.profile)
			return a
		},
	},
	{
		name:          "autotag",
		needsTagState: true,
		extraMetrics:  []string{metricPromiseBroken},
		apply: func(_ context.Context, _ *mutationFeature, in *mutationInput, w *pdf.Writer) (mutationPlan, error) {
			w.AutoTag()
			return identityPlan(in), nil
		},
		verify: verifyAutoTag,
		allowance: func(rc ruleContext) ruleAllowance {
			family := ruleFamily(rc.profile)
			// AutoTag leaves a tagged document, an already-marked one and one
			// with nothing to classify alone, and leaving it alone must change
			// nothing. Whether it should have tagged is promise_broken's.
			if !tagsContent(rc.profile) || containsAll(rc.out, structTreeRules[family]) {
				return strictAllowance(rc.profile)
			}
			a := ruleAllowance{anyRemove: true, mayAdd: autoTagReachRules[family]}
			// The tree it builds comes with /MarkInfo, which an input that
			// had a tree may still lack.
			if containsAll(rc.in, structTreeRules[family]) {
				a.mustPass = markInfoRules[family]
			}
			return a
		},
	},
	{
		name:         "flatten",
		extraMetrics: []string{metricPromiseBroken},
		apply: func(_ context.Context, _ *mutationFeature, in *mutationInput, w *pdf.Writer) (mutationPlan, error) {
			w.FlattenForm()
			return identityPlan(in), nil
		},
		verify: verifyFlatten,
		// Flattening removes the form, its widgets and their actions, and
		// every rule only they failed. It adds nothing.
		allowance: func(ruleContext) ruleAllowance {
			return ruleAllowance{anyRemove: true}
		},
	},
	{
		name:         "sign",
		needsSigner:  true,
		extraMetrics: []string{metricSignatureInvalid},
		// ISO 32000-1, 12.8.1 signs the bytes as written, and litho does not
		// sign an encrypted source (see [pdf.Writer.Sign]).
		refusals: []error{pdf.ErrSignFullRewriteIncompatible},
		apply: func(_ context.Context, f *mutationFeature, in *mutationInput, w *pdf.Writer) (mutationPlan, error) {
			w.Sign(f.signer)
			return identityPlan(in), nil
		},
		verify: verifySign,
		allowance: func(rc ruleContext) ruleAllowance {
			return strictAllowance(rc.profile)
		},
	},
	{
		name:         "encrypt",
		extraMetrics: []string{metricPromiseBroken},
		apply: func(_ context.Context, _ *mutationFeature, in *mutationInput, w *pdf.Writer) (mutationPlan, error) {
			w.Encrypt(pdf.EncryptConfig{
				UserPassword:  mutationEncryptUser,
				OwnerPassword: mutationEncryptOwner,
				Algorithm:     pdf.EncryptAES256,
				Permissions:   -1,
			})
			plan := identityPlan(in)
			plan.password = mutationEncryptUser
			return plan, nil
		},
		verify: verifyEncrypt,
		allowance: func(rc ruleContext) ruleAllowance {
			family := ruleFamily(rc.profile)
			a := strictAllowance(rc.profile)
			a.mustFail = encryptionRules[family]
			a.mayAdd = signatureDigestRules[family]
			return a
		},
	},
	{
		name: "redact",
		// Redact refuses content it cannot remove exactly, and WriteTo refuses
		// an output that still holds a removed string (see [pdf.Writer.Redact]).
		refusals: []error{pdf.ErrRedactionRefused, pdf.ErrRedactionIncomplete},
		apply:    applyRedact,
		verify:   verifyRedact,
		// A redaction removes content and clears the metadata by design, so
		// any rule may pass and the metadata rules may fail.
		allowance: func(rc ruleContext) ruleAllowance {
			return ruleAllowance{anyRemove: true, mayAdd: metadataRules[ruleFamily(rc.profile)]}
		},
	},
}

// concatRules joins rule lists into a new slice.
func concatRules(lists ...[]ruleMatch) []ruleMatch {
	var out []ruleMatch
	for _, l := range lists {
		out = append(out, l...)
	}
	return out
}

// containsAll reports whether ids holds every one of want, and false for an
// empty want.
func containsAll(ids, want []string) bool {
	if len(want) == 0 {
		return false
	}
	for _, id := range want {
		if !slices.Contains(ids, id) {
			return false
		}
	}
	return true
}

// uaRules returns the rule ids r fails at PDF/UA-1, the profile whose rules
// tell whether a document has a structure tree and /MarkInfo.
func uaRules(ctx context.Context, r *pdf.Reader) ([]string, error) {
	violations, err := r.ValidatePDFUAReport(ctx, dirToPDFUAProfile["PDF_UA-1"])
	if err != nil {
		return nil, fmt.Errorf("validating at PDF/UA-1: %w", err)
	}
	return distinctRuleIDs(violations), nil
}

// verifyAutoTag checks the text is untouched, and that an input AutoTag has no
// documented reason to leave alone comes out tagged. It leaves alone a tagged
// or already-marked document and one with nothing to classify; of these the
// check sees an existing tree or /MarkInfo, a page litho cannot read, and a
// document with no text. Marked content it cannot see through the public API.
func verifyAutoTag(ctx context.Context, f *mutationFeature, in *mutationInput, out *mutationOutputDoc, plan mutationPlan) []mutationFailure {
	failures := verifyIdentityText(ctx, f, in, out, plan)
	if !in.untagged {
		return failures
	}
	hasText := false
	for _, page := range in.text {
		if page.err != "" {
			return failures
		}
		hasText = hasText || page.text != ""
	}
	if !hasText {
		return failures
	}
	rules, err := uaRules(ctx, out.reader)
	if err != nil {
		return append(failures, mutationFailure{Metric: metricPromiseBroken, Detail: err.Error()})
	}
	if containsAll(rules, structTreeRules[familyUA1]) {
		failures = append(failures, mutationFailure{Metric: metricPromiseBroken, Detail: "an untagged document with text has no structure tree after AutoTag"})
	}
	return failures
}

// applyStamp draws [mutationStampText] on every page, inside its crop box.
// The stamp is an /Artifact (ISO 32000-1, 14.8.2.2), so a plain read of the
// page must not see it and a read with [pdf.WithArtifacts] must.
func applyStamp(_ context.Context, _ *mutationFeature, in *mutationInput, w *pdf.Writer) (mutationPlan, error) {
	for i, box := range in.boxes {
		if err := w.StampText(pdf.TextStamp{
			Text:     mutationStampText,
			X:        box[0] + mutationStampInset,
			Y:        box[1] + mutationStampInset,
			FontSize: mutationStampFontSize,
		}, i+1); err != nil {
			return mutationPlan{}, fmt.Errorf("stamping page %d: %w", i+1, err)
		}
	}
	return identityPlan(in), nil
}

// verifyStamp checks that every page reads as before without artifacts, and
// carries the stamp with them.
func verifyStamp(ctx context.Context, _ *mutationFeature, in *mutationInput, out *mutationOutputDoc, _ mutationPlan) []mutationFailure {
	plain := readPageTexts(ctx, out.reader, out.pages)
	for i := range min(len(plain), len(in.defaultText)) {
		want, got := in.defaultText[i], plain[i]
		if want.err != "" {
			continue
		}
		if got.err != "" || got.text != want.text {
			detail := got.err
			if detail == "" {
				detail = textDetail(i+1, i+1, want.text, got.text)
			}
			return []mutationFailure{{Metric: metricTextWrong, Detail: "without artifacts, " + detail}}
		}
	}
	stamp := normalizeContent(mutationStampText)
	for i, got := range readPageTexts(ctx, out.reader, out.pages, hiddenText...) {
		if got.err == "" && !strings.Contains(got.text, stamp) {
			return []mutationFailure{{Metric: metricTextWrong, Detail: fmt.Sprintf("page %d does not carry the stamp", i+1)}}
		}
	}
	return nil
}

// applyPages moves page 1 to the end of a multi-page input: a second Reader
// over the same bytes supplies a copy of page 1, InsertPages appends it, and
// DeletePages removes the original. A one-page input only gets the copy
// appended, because DeletePages refuses to delete the Reader's only page
// even once the insertion is queued.
func applyPages(ctx context.Context, _ *mutationFeature, in *mutationInput, w *pdf.Writer) (mutationPlan, error) {
	copySrc, err := pdf.Open(ctx, bytes.NewReader(in.data), int64(len(in.data)))
	if err != nil {
		return mutationPlan{}, fmt.Errorf("opening the copy source: %w", err)
	}
	plan := mutationPlan{release: func() { copySrc.Close() }}
	if err := w.InsertPages(in.pages, copySrc, 1); err != nil {
		return plan, fmt.Errorf("inserting page 1: %w", err)
	}
	if in.pages == 1 {
		plan.pages = 2
		plan.pageSource = []int{0, 0}
		return plan, nil
	}
	if err := w.DeletePages(1); err != nil {
		return plan, fmt.Errorf("deleting page 1: %w", err)
	}
	plan.pages = in.pages
	plan.deleted = true
	for i := 1; i < in.pages; i++ {
		plan.pageSource = append(plan.pageSource, i)
	}
	plan.pageSource = append(plan.pageSource, 0)
	return plan, nil
}

// verifyIdentityText checks each output page carries exactly the text of the
// input page the plan maps it to.
func verifyIdentityText(ctx context.Context, _ *mutationFeature, in *mutationInput, out *mutationOutputDoc, plan mutationPlan) []mutationFailure {
	return compareText(in, readPageTexts(ctx, out.reader, out.pages, hiddenText...), plan.pageSource, 0, false)
}

// verifyFlatten checks each page keeps its text, now with the field values
// drawn into it, and that no form is left: no /AcroForm and no widget litho
// reads as a field.
func verifyFlatten(ctx context.Context, _ *mutationFeature, in *mutationInput, out *mutationOutputDoc, plan mutationPlan) []mutationFailure {
	failures := compareText(in, readPageTexts(ctx, out.reader, out.pages, hiddenText...), plan.pageSource, 0, true)
	fields, err := out.reader.HasFormFields(ctx)
	switch {
	case err != nil:
		failures = append(failures, mutationFailure{Metric: metricPromiseBroken, Detail: fmt.Sprintf("reading the form fields: %v", err)})
	case fields:
		failures = append(failures, mutationFailure{Metric: metricPromiseBroken, Detail: "a form field survives the flattening"})
	case out.reader.HasAcroForm(ctx):
		failures = append(failures, mutationFailure{Metric: metricPromiseBroken, Detail: "the /AcroForm survives the flattening"})
	}
	return failures
}

// verifySign checks the text is untouched and the output holds one more
// signature than the input, one made by the corpus signer that litho's
// verifier accepts: the digest matches the bytes its /ByteRange names and the
// certificate chains to the signer's root.
func verifySign(ctx context.Context, f *mutationFeature, in *mutationInput, out *mutationOutputDoc, plan mutationPlan) []mutationFailure {
	failures := verifyIdentityText(ctx, f, in, out, plan)
	results, err := out.reader.VerifySignatures(ctx, f.signer.roots())
	if err != nil {
		return append(failures, mutationFailure{Metric: metricSignatureInvalid, Detail: fmt.Sprintf("verifying: %v", err)})
	}
	if len(results) != in.signatures+1 {
		return append(failures, mutationFailure{
			Metric: metricSignatureInvalid,
			Detail: fmt.Sprintf("%d signatures, want %d", len(results), in.signatures+1),
		})
	}
	for _, v := range results {
		if v.SignerCN == corpusSignerCN && v.HashValid && v.CertChainValid {
			return failures
		}
	}
	var seen []string
	for _, v := range results {
		seen = append(seen, fmt.Sprintf("%s: hash %t chain %t %s", v.FieldName, v.HashValid, v.CertChainValid, v.ErrorMessage))
	}
	return append(failures, mutationFailure{Metric: metricSignatureInvalid, Detail: "no valid corpus signature: " + strings.Join(seen, "; ")})
}

// verifyEncrypt checks the text reads as before with the user password, and
// that the output is encrypted: litho reports it so, and opening it without
// the password fails with [pdf.ErrInvalidPassword].
func verifyEncrypt(ctx context.Context, f *mutationFeature, in *mutationInput, out *mutationOutputDoc, plan mutationPlan) []mutationFailure {
	failures := verifyIdentityText(ctx, f, in, out, plan)
	if !out.reader.IsEncrypted() {
		return append(failures, mutationFailure{Metric: metricPromiseBroken, Detail: "the output is not encrypted"})
	}
	r, err := pdf.Open(ctx, bytes.NewReader(out.data), int64(len(out.data)))
	if err == nil {
		r.Close()
		return append(failures, mutationFailure{Metric: metricPromiseBroken, Detail: "the output opens without its user password"})
	}
	if !errors.Is(err, pdf.ErrInvalidPassword) {
		return append(failures, mutationFailure{Metric: metricPromiseBroken, Detail: fmt.Sprintf("opening without the password: %v, want ErrInvalidPassword", err)})
	}
	return failures
}

// applyRedact redacts everything page 1 draws, with a region far larger than
// any page box, so nothing straddles it: every refusal is then about how the
// content is drawn, never about where the region ends. The geometry of a
// partial region stays with the unit tests.
func applyRedact(ctx context.Context, _ *mutationFeature, in *mutationInput, w *pdf.Writer) (mutationPlan, error) {
	region := pdf.RedactRegion{
		Page: 1,
		Rect: [4]float64{-mutationRedactReach, -mutationRedactReach, mutationRedactReach, mutationRedactReach},
	}
	if _, err := w.Redact(ctx, []pdf.RedactRegion{region}); err != nil {
		return mutationPlan{}, fmt.Errorf("redacting page 1: %w", err)
	}
	return identityPlan(in), nil
}

// verifyRedact checks page 1 reads empty with every hidden category included,
// and every other page reads as before.
func verifyRedact(ctx context.Context, _ *mutationFeature, in *mutationInput, out *mutationOutputDoc, plan mutationPlan) []mutationFailure {
	texts := readPageTexts(ctx, out.reader, out.pages, hiddenText...)
	if len(texts) > 0 && (texts[0].err != "" || texts[0].text != "") {
		detail := texts[0].err
		if detail == "" {
			detail = fmt.Sprintf("%q survives", truncate(texts[0].text, 40))
		}
		return []mutationFailure{{Metric: metricTextWrong, Detail: "page 1 after redaction: " + detail}}
	}
	failures := compareText(in, texts, plan.pageSource, 1, false)
	for i := range failures {
		failures[i].Detail = "after redacting page 1, " + failures[i].Detail
	}
	return failures
}
