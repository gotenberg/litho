package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"math"
	"regexp"
	"slices"
	"strconv"
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
	// learn whether it has a structure tree, and scans it for marked content.
	needsTagState bool
	// extraMetrics are the metrics beyond the six every spec emits.
	extraMetrics []string
	// counters are the inventory counts the spec emits beside its metrics,
	// which count what the run reached rather than what failed (see
	// [mutationPlan.counts]).
	counters []string
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
	// structure tree nor /MarkInfo /Marked true, the inputs AutoTag does not
	// leave alone as tagged.
	untagged bool
	// markedContent is set, when the spec asks, for an input a stream of
	// which holds a marked-content operator (see [scanMarkedContent]).
	markedContent bool
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
	// autoTag is the report [pdf.Writer.AutoTag] returned, which the write
	// fills.
	autoTag *pdf.AutoTagReport
	// droppedSignatures are the input's signatures the write left out (see
	// [pdf.Writer.DroppedSignatures]), which the write fills.
	droppedSignatures []pdf.DroppedSignature
	// release frees what the mutation holds until the write, a second Reader.
	release func()
	// signField names the unsigned signature field mutate-sign signs in, ""
	// for a new field.
	signField string
	// signForm is the input's form as qpdf reads it, and signTrials each of
	// its unsigned signature fields signed in a write of its own (see
	// [mutationFeature.signFields]).
	signForm   *qpdfForm
	signTrials []signFieldTrial
	// counts are the spec's inventory counts, which verify fills.
	counts map[string]int
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
		in.markedContent = scanMarkedContent(data)
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
// file-structure rule may pass (see [fileStructureRules]), and so may the rule
// a page with no resource dictionary fails, since the Writer writes one on
// every page it writes (see [pageResourcesRules]). Nothing else may change.
func strictAllowance(profileDir string) ruleAllowance {
	family := ruleFamily(profileDir)
	return ruleAllowance{mayRemove: concatRules(fileStructureRules[family], pageResourcesRules[family])}
}

// mutationStampText is the text mutate-stamp draws. Its normalized form is
// what the check looks for on every page.
const mutationStampText = "LITHO MUTATION STAMP"

// mutationStampInset places the stamp this far inside the lower-left corner
// of each page's crop box, in points.
const mutationStampInset = 10

// mutationStampTolerance is how far, in points, the stamp's first glyph may
// read from where the stamp was placed. Text extraction reports the origin of
// the glyph the stamp's Td places, so a correct stamp reads at the exact
// point, and the tolerance absorbs only the rounding of written reals. A box
// origin smaller than it moves the stamp by less than the check can see.
const mutationStampTolerance = 0.5

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

// mutationEncryptPermissions is the mask mutate-encrypt encrypts with: every
// permission bit of ISO 32000-2, Table 22 set (3-6 and 9-12), and every
// reserved bit at the value the table forbids, bits 1-2 set and bits 7-8 and
// 13-32 clear. The writer corrects the reserved bits whatever the mask holds
// (see [pdf.EncryptConfig]), so the output's P is -4, as for -1, and
// [verifyEncrypt] sees a regression in either correction, where -1 hid one
// that stopped setting bits 7-8 and 13-32.
const mutationEncryptPermissions int32 = 0b1111_0011_1111

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
// mutate-pages only appends the copies to a one-page input, and the deletion
// runs on the multi-page ones.
//
// The failures the features count on the code they were written against are
// listed below, so the baseline they set reads as findings rather than noise.
//
// # Corpus evidence
//
//   - litho: 151 of the 215 fixtures have one page, so mutate-pages deletes a
//     page on 64.
//   - litho: 50 fixtures refuse the whole-page redaction by design, 49 with
//     ErrRedactionRefused (36 content a form XObject draws, 4 image samples
//     it cannot decode, 3 a shading paint the region reaches, 2 text whose
//     glyphs cannot be placed, 1 text whose position viewers read two ways,
//     2 an image carrying /Decode or /Mask entries, recolorimg_2b's colour-key
//     ImKey among them, 1 an annotation another page still shows) and 1,
//     redactcopies_2b, with ErrRedactionIncomplete.
//   - litho encperm_ua1, trailer_2b: the 2 encrypted fixtures refuse the
//     signature with ErrSignFullRewriteIncompatible.
//   - litho formshare_ua1, sharedtag_ua1, sharedclose_ua1: no ruleset_changed
//     cell under mutate-stamp. Their pages share one /Contents stream drawing
//     a marked form, which veraPDF parses once per /Contents object. While
//     each StampText call of applyStamp gave the page it stamped a /Contents
//     array of its own, veraPDF 1.30.2 read the shared stream once per page
//     and counted the form again, so the stamp added 7.20 test 2 at PDF/UA-1
//     on both tools. The pages now keep naming one array, which draws each
//     page's stamps through a form its own resources name.
//   - litho sharedtag_ua1: 1 ruleset_changed cell under mutate-autotag, 7.1
//     test 2 added at PDF/UA-1, and veraPDF 1.30.2 adds it too: the marked
//     form is drawn inside the artifact the auto-tagger makes of the graphics
//     around it, as remediation at PDF/UA-1 leaves it (see the fixture's
//     README section).
//   - litho imagereach_2b: no ruleset_changed cell. Its two pages share one
//     content stream, and litho judges it under each page's resources (ISO
//     32000-1, 7.8.3), so the stamp's /Contents arrays and the page move no
//     longer change which page's image is judged. Before that, both features
//     counted 11 cells, one per PDF/A profile, as veraPDF still would under
//     mutate-pages: it judges a shared stream once, through the first page's
//     resources. Under mutate-stamp the pages keep naming one /Contents
//     array, so veraPDF 1.30.2 judges the image as it does on the source and
//     adds at PDF/A-2b only the stamp's own 6.2.11.4.1 test 1 and 6.2.4.3
//     test 2, where a /Contents array for each page added 6.2.6 test 1 and
//     6.2.8 test 3 as well.
//   - litho namedmcid_ua1, structcopy_ua2: while the copy of a tagged first
//     page carried its content and not its structure, its MCIDs mapped to no
//     element, and mutate-pages added 7.1 test 3 at PDF/UA-1 and 8.2.2 test 1
//     at PDF/UA-2 on both tools, and 7.18.5 test 1 at PDF/UA-1 on
//     structcopy_ua2, whose Link lost its element. The copy carries its
//     structure elements now, and the cells pass.
//   - litho: with the deletion no longer allowed to pass any rule at the
//     tagged profiles, the 33 fixtures whose first page moves to the end
//     count no ruleset_changed cell.
//   - litho: naming page 1 twice moves no mutate-pages cell: the 124 fixtures
//     count the 986 matched cells that the single copy counted, measured
//     before PDF/A-4e, 4f, 1a, 3a and 3u joined the manifest.
//   - litho structcopy_ua2, pagelabels_2b, annotshare_ua1, sigtagged_ua1,
//     complex_ua1, formstruct_ua2: veraPDF reports the input's rule set on the
//     output of the repeat at each home profile, at PDF/UA-1 for the two
//     PDF/UA-2 fixtures, and in all thirteen flavours for pagelabels_2b.
//   - litho: planted in InsertPages, a repeat written as its first copy's page
//     object again counts 124 pages_wrong PDFs, litho reading one page fewer
//     than the plan promises, and a repeat copied without its structure counts
//     57 ruleset_changed cells on 30 tagged fixtures: 7.1 test 3, or on 3 an
//     annotation rule of 7.18, at PDF/UA-1, and 8.2.2 test 1 at PDF/UA-2.
//   - litho a4syntax_4: 1 ruleset_changed cell (PDF/A-4, 6.1.2 test 1) under
//     each of mutate-pages, mutate-flatten and mutate-encrypt: a full rewrite
//     writes a %PDF-1.7 header over a PDF 2.0 source.
//   - litho xrefstm_2b: 1 ruleset_changed cell (PDF/A-1b, 6.7.3 tests 2, 4
//     and 7 pass) under mutate-pages and mutate-encrypt: a full rewrite drops
//     the direct /Info dictionary the source's cross-reference stream holds.
//   - litho: mutate-autotag tags 59 PDFs and leaves 43 alone, each for the
//     reason [pdf.AutoTagReport] names and [verifyAutoTag] finds on the input:
//     26 already tagged, 13 with nothing to tag, 3 with marked content and 1
//     with a page it cannot read.
//   - litho complex_2u, forbidden_mix, inlineimage_2b: the 3 with marked
//     content counted promise_broken until AutoTag reported its skips.
//   - litho undecodable_2b: the page it cannot read is page 7, and litho
//     reads no text from it either.
//   - litho patternreach_2b: 1 promise_broken PDF. Its only text is one Type
//     3 A at (10, 10) on pages 4 and 11, which AutoTag reads as a running
//     footer, and a document whose text is all furniture has nothing to tag.
//     AutoTag writes no tree, so nothing is hidden; the check reads the
//     input's text and cannot tell furniture from content.
//   - litho refreach_ua1: 1 promise_broken PDF, the same shape: its only
//     text is one Type 3 A at (1, 1), shown on two pages, which AutoTag
//     reads as a running footer.
//   - litho formreuse_ua1: 1 promise_broken PDF, the same shape: its only
//     text is Type 3 A glyphs drawn at one spot on each page.
//   - litho: [scanMarkedContent] finds marked content in 26 inputs, the 23
//     already tagged that carry it and the 3 above, and in none of the 59
//     AutoTag tags.
//   - litho: planted in AutoTag, a defect that reports every untagged page
//     as marked counts 73 promise_broken PDFs, and one that builds the tree
//     without wiring it into the catalog counts 59.
//
//nolint:gochecknoglobals // static feature table
var mutationSpecs = []mutationSpec{
	{
		name:             "stamp",
		needsDefaultText: true,
		extraMetrics:     []string{metricPromiseBroken},
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
		name:         "pages",
		extraMetrics: []string{metricPromiseBroken},
		apply:        applyPages,
		verify:       verifyPagesMove,
		allowance: func(rc ruleContext) ruleAllowance {
			family := ruleFamily(rc.profile)
			a := strictAllowance(rc.profile)
			a.mayRemove = concatRules(a.mayRemove, explicitResourcesRules[family])
			// Deleting a page prunes the structure elements that name it, and
			// each copy that replaces the page carries them again, so a rule
			// only they failed stays failed and no rule may go on their
			// account. The repeat adds none: its copy carries elements of its
			// own. A rewrite drops the input's signatures (see
			// [droppedSignaturesRemove]).
			a.anyRemove = droppedSignaturesRemove(rc)
			return a
		},
	},
	{
		name:          "autotag",
		needsTagState: true,
		extraMetrics:  []string{metricPromiseBroken},
		apply: func(_ context.Context, _ *mutationFeature, in *mutationInput, w *pdf.Writer) (mutationPlan, error) {
			plan := identityPlan(in)
			plan.autoTag = w.AutoTag()
			return plan, nil
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
		extraMetrics: []string{metricSignatureInvalid, metricPromiseBroken},
		counters:     []string{metricFieldSigned, metricFieldRefused},
		// ISO 32000-1, 12.8.1 signs the bytes as written, and litho does not
		// sign an encrypted source (see [pdf.Writer.Sign]).
		refusals: []error{pdf.ErrSignFullRewriteIncompatible},
		apply:    applySign,
		verify:   verifySign,
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
				Permissions:   mutationEncryptPermissions,
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
			a.anyRemove = droppedSignaturesRemove(rc)
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

// droppedSignaturesRemove reports whether the write left the input's
// signatures out, which lets any rule the input fails pass on the output.
//
// A full rewrite moves every byte a signature covers (ISO 32000-1, 12.8.1), so
// the Writer draws each signed widget into its page and drops the signature,
// its field and the /Perms entries naming it. A rule only they failed goes with
// them: the signature rules of ISO 19005-2, 6.4.3, and the annotation rules of
// ISO 14289 the widgets failed. A rewrite that copied a signature again would
// fail ISO 19005-2, 6.4.3 test 1 on the output, which no allowance admits.
//
// # Corpus evidence
//
//   - litho sigcms_2b: mutate-pages and mutate-encrypt drop its four signed
//     fields, and 6.4.3 tests 1 to 3 pass at PDF/A-2a, 2b, 2u and 3b.
//   - litho docmdp_2b: they drop its certification signature, and at PDF/UA-1
//     7.18.1 test 3, 7.18.3 test 1 and 7.18.4 test 1 pass, at PDF/UA-2 8.9.3.3
//     test 1, all of which its widget failed.
func droppedSignaturesRemove(rc ruleContext) bool {
	return len(rc.plan.droppedSignatures) > 0
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

// applyStamp draws [mutationStampText] on every page, [mutationStampInset]
// points inside the lower-left corner of its crop box. [pdf.TextStamp] takes
// X and Y from the page's bottom-left corner, not from the user-space origin,
// so the stamp names the inset alone: adding the box origin moved it twice on
// a page whose box does not start at (0, 0). The stamp is an /Artifact (ISO
// 32000-1, 14.8.2.2), so a plain read of the page must not see it and a read
// with [pdf.WithArtifacts] must.
func applyStamp(_ context.Context, _ *mutationFeature, in *mutationInput, w *pdf.Writer) (mutationPlan, error) {
	for i := range in.pages {
		if err := w.StampText(pdf.TextStamp{
			Text:     mutationStampText,
			X:        mutationStampInset,
			Y:        mutationStampInset,
			FontSize: mutationStampFontSize,
		}, i+1); err != nil {
			return mutationPlan{}, fmt.Errorf("stamping page %d: %w", i+1, err)
		}
	}
	return identityPlan(in), nil
}

// verifyStamp checks that every page reads as before without artifacts,
// carries the stamp with them, and carries it where [pdf.TextStamp] promises
// (see [stampPlacement]).
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
	return stampPlacement(ctx, in, out)
}

// stampPlacement checks that the stamp's text starts [mutationStampInset]
// points right of and above each page's visible lower-left corner, within
// [mutationStampTolerance]. The corner is the lesser of each pair of the crop
// box's coordinates, since a rectangle may name either pair of opposite
// corners (ISO 32000-1, 7.9.5). A stamp drawn from the user-space origin, or
// moved by the box origin twice, still reads as the stamp's text: only its
// position tells it apart. A page litho reads no positions from is left to
// the text check.
//
// A page narrower than the stamp clips its tail: a glyph whose origin lies
// past the right edge of the crop box paints outside the visible region (ISO
// 32000-1, 14.11.2), and litho reads it apart from the visible run, as PDF.js
// drops it. The stamp then reads as a prefix of its text, which still says
// where it starts.
//
// # Corpus evidence
//
//   - litho sharedarray_2b: its pages are 100 points wide, so the stamp's
//     last letter starts at x = 102 and the visible run reads "LITHO
//     MUTATION STAM" at (10, 10).
//   - litho: every stamp on the 310 pages of the other 133 fixtures reads at
//     exactly (10, 10), since none has a crop box off the origin.
//   - litho boxorigin_2b: planted in turn, the cell adding the box origin to X
//     and Y, stampOriginShift reading the crop box's first two entries, and
//     no shift at all each count promise_broken. The text check passes all
//     three.
//   - litho viewerboxes_2b: stampOriginShift reading the first two entries of
//     page 1's /MediaBox [10 10 300] counts promise_broken, the stamp reading
//     at (20, 20).
func stampPlacement(ctx context.Context, in *mutationInput, out *mutationOutputDoc) []mutationFailure {
	stamp := normalizeContent(mutationStampText)
	for i := range min(out.pages, len(in.boxes)) {
		elements, err := out.reader.ExtractTextElements(ctx, i+1)
		if err != nil {
			continue
		}
		box := in.boxes[i]
		wantX := min(box[0], box[2]) + mutationStampInset
		wantY := min(box[1], box[3]) + mutationStampInset
		var seen []string
		placed := false
		for _, e := range elements {
			text := normalizeContent(e.Text)
			if !strings.HasPrefix(text, stamp) && (text == "" || !strings.HasPrefix(stamp, text)) {
				continue
			}
			if math.Abs(e.X-wantX) <= mutationStampTolerance && math.Abs(e.Y-wantY) <= mutationStampTolerance {
				placed = true
				break
			}
			seen = append(seen, fmt.Sprintf("(%g, %g)", e.X, e.Y))
		}
		if placed {
			continue
		}
		where := "nowhere"
		if len(seen) > 0 {
			where = "at " + strings.Join(seen, ", ")
		}
		return []mutationFailure{{
			Metric: metricPromiseBroken,
			Detail: fmt.Sprintf("page %d: the stamp reads %s, want (%g, %g)", i+1, where, wantX, wantY),
		}}
	}
	return nil
}

// applyPages moves page 1 to the end of a multi-page input and repeats it
// there: a second Reader over the same bytes supplies page 1, InsertPages
// appends it twice, and DeletePages removes the original. A one-page input
// only gets the two copies appended, because DeletePages refuses to delete the
// Reader's only page even once the insertion is queued.
//
// InsertPages names page 1 twice so that every input exercises a repeat, which
// the Writer writes as a page of its own: a page object has one /Parent and
// the page tree lists it once (ISO 32000-1, 7.7.3.2 and Table 30). Each copy
// promises the text and the label of page 1, and a tagged copy carries
// structure elements of its own, so the repeat moves the rule set no further
// than a single copy does.
func applyPages(ctx context.Context, _ *mutationFeature, in *mutationInput, w *pdf.Writer) (mutationPlan, error) {
	copySrc, err := pdf.Open(ctx, bytes.NewReader(in.data), int64(len(in.data)))
	if err != nil {
		return mutationPlan{}, fmt.Errorf("opening the copy source: %w", err)
	}
	plan := mutationPlan{release: func() { copySrc.Close() }}
	if err := w.InsertPages(in.pages, copySrc, 1, 1); err != nil {
		return plan, fmt.Errorf("inserting page 1 twice: %w", err)
	}
	if in.pages == 1 {
		plan.pages = 3
		plan.pageSource = []int{0, 0, 0}
		return plan, nil
	}
	if err := w.DeletePages(1); err != nil {
		return plan, fmt.Errorf("deleting page 1: %w", err)
	}
	plan.pages = in.pages + 1
	plan.deleted = true
	for i := 1; i < in.pages; i++ {
		plan.pageSource = append(plan.pageSource, i)
	}
	plan.pageSource = append(plan.pageSource, 0, 0)
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
// certificate chains to the signer's root. A signature on a full rewrite, which
// the Writer writes for a source it repaired on reading, is the output's only
// one: the rewrite drops the input's (see [pdf.Writer.DroppedSignatures]).
// Signed in an existing field, the accepted signature must sit in that field.
// Each unsigned signature field signed in a write of its own is checked too
// (see [mutationFeature.verifySignFields]).
func verifySign(ctx context.Context, f *mutationFeature, in *mutationInput, out *mutationOutputDoc, plan mutationPlan) []mutationFailure {
	failures := verifyIdentityText(ctx, f, in, out, plan)
	fieldFailures, counts := f.verifySignFields(ctx, in, plan.signForm, plan.signTrials)
	failures = append(failures, fieldFailures...)
	if plan.counts != nil {
		maps.Copy(plan.counts, counts)
	}
	results, err := out.reader.VerifySignatures(ctx, f.signer.roots())
	if err != nil {
		return append(failures, mutationFailure{Metric: metricSignatureInvalid, Detail: fmt.Sprintf("verifying: %v", err)})
	}
	want := in.signatures + 1
	if len(plan.droppedSignatures) > 0 {
		want = 1
	}
	if len(results) != want {
		return append(failures, mutationFailure{
			Metric: metricSignatureInvalid,
			Detail: fmt.Sprintf("%d signatures, want %d", len(results), want),
		})
	}
	for _, v := range results {
		if v.SignerCN == corpusSignerCN && v.HashValid && v.CertChainValid && (plan.signField == "" || v.FieldName == plan.signField) {
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
// that the output is encrypted: litho reports it so, its P entry holds the
// reserved bits ISO 32000-2, Table 22 requires (see [reservedPermissionBits]),
// and opening it without the password fails with [pdf.ErrInvalidPassword].
func verifyEncrypt(ctx context.Context, f *mutationFeature, in *mutationInput, out *mutationOutputDoc, plan mutationPlan) []mutationFailure {
	failures := verifyIdentityText(ctx, f, in, out, plan)
	if !out.reader.IsEncrypted() {
		return append(failures, mutationFailure{Metric: metricPromiseBroken, Detail: "the output is not encrypted"})
	}
	p, err := encryptionPermissions(out.data)
	if err != nil {
		failures = append(failures, mutationFailure{Metric: metricPromiseBroken, Detail: "reading the P entry: " + err.Error()})
	} else if broken := reservedPermissionBits(p); broken != "" {
		failures = append(failures, mutationFailure{Metric: metricPromiseBroken, Detail: fmt.Sprintf("the P entry %d %s", p, broken)})
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

// encryptEntry matches the /Encrypt entry of a trailer or cross-reference
// stream dictionary that names an indirect encryption dictionary. Encryption
// applies to strings and streams alone (ISO 32000-1, 7.6.1), so the entry
// reads in the clear.
var encryptEntry = regexp.MustCompile(`/Encrypt\s+(\d+)\s+(\d+)\s+R`)

// permissionsEntry matches the P entry of an encryption dictionary. /Perms
// does not match, since a digit or a sign must follow the name.
var permissionsEntry = regexp.MustCompile(`/P\s*([-+]?\d+)`)

// encryptionPermissions returns the P entry of the encryption dictionary data
// names, read from the bytes. P is an integer, which encryption leaves alone
// (ISO 32000-1, 7.6.1), and the encryption dictionary is never in an object
// stream (ISO 32000-1, 7.5.7). The Reader exposes the permissions P grants,
// not the reserved bits it holds. The last /Encrypt entry and the last
// definition of its object win, as they do for a reader of an updated file.
func encryptionPermissions(data []byte) (int32, error) {
	refs := encryptEntry.FindAllSubmatch(data, -1)
	if len(refs) == 0 {
		return 0, errors.New("no /Encrypt entry names an indirect dictionary")
	}
	ref := refs[len(refs)-1]
	header := regexp.MustCompile(`(?:^|[^0-9])` + string(ref[1]) + `\s+` + string(ref[2]) + `\s+obj\b`)
	defs := header.FindAllIndex(data, -1)
	if len(defs) == 0 {
		return 0, fmt.Errorf("the encryption dictionary %s %s R is not defined", ref[1], ref[2])
	}
	body := data[defs[len(defs)-1][1]:]
	if end := bytes.Index(body, []byte("endobj")); end >= 0 {
		body = body[:end]
	}
	m := permissionsEntry.FindSubmatch(body)
	if m == nil {
		return 0, fmt.Errorf("the encryption dictionary %s %s R has no P entry", ref[1], ref[2])
	}
	v, err := strconv.ParseInt(string(m[1]), 10, 64)
	if err != nil || v < math.MinInt32 || v > math.MaxUint32 {
		return 0, fmt.Errorf("the P entry %s is not a 32-bit integer", m[1])
	}
	// A producer may write P unsigned; the 32 bits are the same.
	return int32(uint32(v)), nil //nolint:gosec // v lies in [MinInt32, MaxUint32], so its low 32 bits are P.
}

// reservedPermissionBits reports how p breaks the reserved bits of the P
// entry of a standard security handler of revision 3 or greater, "" when it
// keeps them. ISO 32000-2, 7.6.4.2, Table 22 reads "Reserved. Must be zero
// (0)." for bits 1-2, "Reserved. Must be 1." for bits 7-8 and "(Security
// handlers of revision 3 or greater) Reserved. Must be 1." for bits 13-32.
// The encrypt mutation writes AES-256, a revision 6 handler.
func reservedPermissionBits(p int32) string {
	const (
		zeroBits int32 = 0b11
		oneBits  int32 = 0b1100_0000 | ^int32(1<<12-1)
	)
	var broken []string
	if p&zeroBits != 0 {
		broken = append(broken, "sets reserved bits 1-2")
	}
	if p&oneBits != oneBits {
		broken = append(broken, "clears a reserved bit of 7-8 or 13-32")
	}
	if len(broken) == 0 {
		return ""
	}
	return strings.Join(broken, " and ") + " (ISO 32000-2, Table 22)"
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
