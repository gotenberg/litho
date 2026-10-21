package remediation

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/icc"
)

// sRGBICCProfile is litho's built-in sRGB profile, shared with the writers
// that state samples they produced in sRGB.
var sRGBICCProfile = icc.SRGBProfile()

// sGrayICCProfile is litho's generated version 2 grey profile, the stock
// replacement for a grey destination profile (see [replaceICCProfile]).
var sGrayICCProfile = icc.SGrayProfile()

// cmykICCProfile is litho's generated version 2 CMYK printer profile, the
// stock profile of the CMYK output intents remediation adds and of the CMYK
// destination profiles it replaces (see [replaceICCProfile]). A CMYK colour
// reads through it as DeviceCMYK reads by default.
var cmykICCProfile = icc.CMYKPrinterProfile()

// cmykCondition and cmykRegistry name the printing condition the stock CMYK
// profile characterises: the CGATS TR 001 entry of the ICC characterization
// data registry. ISO 32000-1, 14.11.5, Table 365 reads the
// /OutputConditionIdentifier as a name from the registry /RegistryName names.
const (
	cmykCondition = "CGATS TR 001"
	cmykRegistry  = "http://www.color.org"
)

// cmykICCStream returns the stock CMYK profile as an ICC profile stream of
// four components, FlateDecode-compressed: its BToA table makes it 58 KB,
// which deflates to 27 KB, and most PDF/A outputs carry a copy, the
// DefaultCMYK [fixDeviceColorIntent] stages beside an RGB intent.
func cmykICCStream() ast.PdfStream {
	return flateStream(ast.PdfDict{"N": ast.PdfInt(4)}, cmykICCProfile)
}

// sRGBICCStream returns the stock sRGB profile as an ICC profile stream of
// three components.
func sRGBICCStream() ast.PdfStream {
	return ast.NewStream(ast.PdfDict{"N": ast.PdfInt(3)}, sRGBICCProfile)
}

// hasMatchingOutputIntent reports whether an existing PDF/A output intent
// already covers the needed colour space (CMYK if needsCMYK, RGB otherwise).
//
// Only a GTS_PDFA1 intent counts. ISO 32000-2, Table 401 and ISO 19005-4,
// 6.2.3 keep GTS_PDFA1 as the PDF/A output-intent subtype for every part, so a
// GTS_PDFX or ISO_PDFE1 intent leaves the device colour spaces uncovered no
// matter which profile it carries.
//
// # Corpus evidence
//
//   - verapdf 6-2-3-t01-fail-a and -fail-c: veraPDF still reports 6.2.4.3 on
//     both, whose sole intent is GTS_PDFX with an RGB profile.
func hasMatchingOutputIntent(ctx context.Context, doc *document.Document, arr ast.PdfArray, needsCMYK bool) bool {
	for _, entry := range arr {
		oiDict, err := document.ResolveDict(ctx, doc, entry)
		if err != nil || oiDict == nil {
			continue
		}
		if subtype, _ := document.ResolveName(ctx, doc, oiDict["S"]); subtype != "GTS_PDFA1" {
			continue
		}
		dpObj, ok := oiDict["DestOutputProfile"]
		if !ok {
			continue
		}
		stream, err := document.ResolveStream(ctx, doc, dpObj)
		if err != nil {
			continue
		}
		// The colour space sits at a fixed offset into the profile (ISO
		// 15076-1, 7.2), so the stream has to be decoded: reading the raw body
		// of a FlateDecode profile compares deflate output against "CMYK" and
		// never matches, and litho installs a second OutputIntent beside the
		// one already covering the document.
		data, err := doc.DecodeStream(ctx, stream)
		if err != nil || len(data) < 20 {
			continue
		}
		colorSpace := string(data[16:20])
		if needsCMYK && colorSpace == "CMYK" {
			return true
		}
		if !needsCMYK && colorSpace == "RGB " {
			return true
		}
	}
	return false
}

func matchOutputIntent(v compliance.Violation) bool {
	// ISO 19005-1, 6.5.3 test 3 admits an annotation colour array under an RGB
	// PDF/A-1 output intent, which this fixer installs in a file that carries
	// none. See [addAnnotColorIntent].
	if matchAnnotColor(v) {
		return true
	}
	// A device-colour transparency group blending space is repaired structurally
	// by fixGroupCS (drop /CS), not by installing an OutputIntent: installing one
	// for the group can perturb a multi-scope file's existing intent coverage.
	if contextContains(v, "transparency group") {
		return false
	}
	return clausePrefix(v, "6.2.3.2", "6.2.3.3", "6.2.4.3") // device CS without OutputIntent
}

// matchOutputIntentCleanup covers structural OutputIntent violations:
// invalid ICC profile, duplicate DestOutputProfile mismatch, and the
// forbidden DestOutputProfileRef key (PDF/A-2 §6.2.3 / PDF/A-4 §6.2.3).
// ISO 19005-1 numbers the same-profile rule 6.2.2 t2, so it is claimed by
// rule rather than by the clause prefix the later parts share.
func matchOutputIntentCleanup(v compliance.Violation) bool {
	if v.Rule == compliance.Rule_19005_1_6_2_2_t02 {
		return true
	}
	if !clausePrefix(v, "6.2.3") {
		return false
	}
	return contextContains(v, "ICC profile") ||
		contextContains(v, "DestOutputProfile") ||
		contextContains(v, "DestOutputProfileRef")
}

// fixOutputIntentCleanup normalises the OutputIntents array:
//
//  1. Strips forbidden /DestOutputProfileRef entries.
//  2. Replaces the anchor intent's /DestOutputProfile with the embedded sRGB
//     profile when it is too short to carry an ICC header.
//  3. Re-points every other intent's /DestOutputProfile at the anchor's when
//     they diverge, satisfying ISO 19005-1, 6.2.2 t2 and ISO 19005-2/3/4,
//     6.2.3 t2: every intent carrying the key names the same indirect object.
//
// The anchor is the first GTS_PDFA1 intent carrying a destination profile
// (see [outputIntentAnchor]), so the PDF/A reading of the document keeps its
// colour and the other intents, GTS_PDFX or ISO_PDFE1, take its profile. Their
// /OutputConditionIdentifier stays, so a PDF/X intent whose profile differed
// now names its printing condition over the PDF/A intent's profile. Each fix
// says whether the replaced profile differed from the anchor's.
//
// # Corpus evidence
//
//   - All corpora: three files name more than one destination profile object
//     in their catalog's /OutputIntents, verapdf PDF_A-2b and PDF_A-4
//     6-2-3-t02-fail-a and litho outputintent_2b, each with the GTS_PDFA1
//     intent first. Before the fixer claimed ISO 19005-1, 6.2.2 t2, the three
//     kept it through a PDF/A-1a and 1b remediation on litho and veraPDF.
//   - litho twoprofile_1b, a GTS_PDFX CMYK intent before a GTS_PDFA1 RGB one
//     over DeviceRGB content: unifying on the first entry left the content
//     uncovered, and the next round replaced the whole array with a stock sRGB
//     intent, dropping the PDF/X intent and the source's RGB profile.
func fixOutputIntentCleanup(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalogRef, ok := catalogRefForFixer(ctx, doc)
	if !ok {
		return nil, nil
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, nil
	}
	oiObj, ok := catalogDict["OutputIntents"]
	if !ok {
		return nil, nil
	}
	arr, err := document.ResolveArray(ctx, doc, oiObj)
	if err != nil || len(arr) == 0 {
		return nil, nil //nolint:nilerr // An unresolvable /OutputIntents is null (ISO 32000-1, 7.3.10): no intent to repair.
	}

	var fixes []string
	changedArr := false
	newArr := make(ast.PdfArray, len(arr))
	intents := make([]cleanupIntent, len(arr))

	for i, entry := range arr {
		newArr[i] = entry
		ref, isRef := entry.(ast.PdfIndirectRef)
		var entryDict ast.PdfDict
		if isRef && ref.ObjectNum > 0 {
			entryDict = resolveFromAllocOrDoc(ctx, alloc, doc, ref)
		}
		if entryDict == nil {
			if d, err := document.ResolveDict(ctx, doc, entry); err == nil {
				entryDict = d
			}
		}
		if entryDict == nil {
			continue
		}
		it := cleanupIntent{dict: make(ast.PdfDict, len(entryDict))}
		if isRef && ref.ObjectNum > 0 {
			it.ref = ref
		}
		maps.Copy(it.dict, entryDict)
		if _, has := it.dict["DestOutputProfileRef"]; has {
			delete(it.dict, "DestOutputProfileRef")
			it.changed = true
			fixes = append(fixes, fmt.Sprintf("removed /DestOutputProfileRef from OutputIntent[%d]", i))
		}
		intents[i] = it
	}

	if a := outputIntentAnchor(ctx, doc, intents); a >= 0 {
		anchor := &intents[a]
		anchorProfile, _ := anchor.dict["DestOutputProfile"].(ast.PdfIndirectRef)
		anchorData := decodedProfile(ctx, alloc, doc, anchorProfile)
		// Measure the decoded profile: the 128-byte ICC header is a property
		// of the profile, not of its compressed body.
		if anchorData != nil && len(anchorData) < 128 {
			// Replace with a stock sRGB profile when the existing stream is
			// too short to be a valid ICC header.
			anchorProfile = alloc.Alloc(ast.NewStream(ast.PdfDict{"N": ast.PdfInt(3)}, sRGBICCProfile))
			anchorData = sRGBICCProfile
			anchor.dict["DestOutputProfile"] = anchorProfile
			anchor.changed = true
			fixes = append(fixes, fmt.Sprintf("replaced too-short ICC profile on OutputIntent[%d]", a))
		}
		for i := range intents {
			it := &intents[i]
			dp, has := it.dict["DestOutputProfile"]
			if dpRef, isRef := dp.(ast.PdfIndirectRef); i == a || it.dict == nil || !has || (isRef && dpRef == anchorProfile) {
				continue
			}
			fixes = append(fixes, fmt.Sprintf("unified /DestOutputProfile on OutputIntent[%d] (/S /%s) with OutputIntent[%d] (/S /%s): %s",
				i, intentSubtype(ctx, doc, it.dict), a, intentSubtype(ctx, doc, anchor.dict),
				profileDifference(anchorData, decodedProfile(ctx, alloc, doc, dp))))
			it.dict["DestOutputProfile"] = anchorProfile
			it.changed = true
		}
	}

	for i, it := range intents {
		if !it.changed {
			continue
		}
		if it.ref.ObjectNum > 0 {
			alloc.AllocAt(it.ref.ObjectNum, it.dict)
		} else {
			newArr[i] = it.dict
			changedArr = true
		}
	}

	if changedArr {
		newCat := make(ast.PdfDict, len(catalogDict))
		maps.Copy(newCat, catalogDict)
		newCat["OutputIntents"] = newArr
		alloc.AllocAt(catalogRef.ObjectNum, newCat)
	}

	if len(fixes) == 0 {
		return nil, nil
	}
	return fixes, nil
}

// cleanupIntent is one /OutputIntents entry as [fixOutputIntentCleanup]
// rewrites it: a copy of its dictionary, the object it lives at (zero for an
// intent written directly in the array), and whether the copy changed. dict is
// nil for an entry that does not resolve to a dictionary.
type cleanupIntent struct {
	dict    ast.PdfDict
	ref     ast.PdfIndirectRef
	changed bool
}

// outputIntentAnchor returns the index of the intent whose destination profile
// every other intent is unified on, or -1 when no intent names one by
// reference.
//
// It is the first GTS_PDFA1 intent: that is the intent PDF/A reads the
// document's device colour through (ISO 19005-1, 6.2.3.3; ISO 19005-2/3/4,
// 6.2.4.3), and ISO 32000-2, Table 401 keeps GTS_PDFA1 as the PDF/A subtype
// for every part. Keeping its profile leaves the colour of the PDF/A reading
// unchanged, where unifying on the first entry gave a PDF/A intent listed after
// a GTS_PDFX one the PDF/X profile, possibly of another colour space. A file
// whose intents are all of other subtypes falls back to the first intent
// naming a profile, the choice the fixer made before.
func outputIntentAnchor(ctx context.Context, doc *document.Document, intents []cleanupIntent) int {
	first := -1
	for i, it := range intents {
		if _, ok := it.dict["DestOutputProfile"].(ast.PdfIndirectRef); !ok {
			continue
		}
		if intentSubtype(ctx, doc, it.dict) == "GTS_PDFA1" {
			return i
		}
		if first < 0 {
			first = i
		}
	}
	return first
}

// intentSubtype returns an output intent's /S, or "" when it names none.
func intentSubtype(ctx context.Context, doc *document.Document, intent ast.PdfDict) string {
	s, err := document.ResolveName(ctx, doc, intent["S"])
	if err != nil {
		return ""
	}
	return string(s)
}

// decodedProfile returns the decoded bytes of the ICC profile stream obj
// names, or nil when it does not resolve or decode. A profile an earlier fixer
// of this run replaced is read from the allocator, where it is still pending.
func decodedProfile(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, obj ast.PdfObject) []byte {
	var stream ast.PdfStream
	ref, isRef := obj.(ast.PdfIndirectRef)
	pending, isPending := alloc.Pending()[ref.ObjectNum].(ast.PdfStream)
	switch {
	case isRef && isPending:
		stream = pending
	default:
		s, err := document.ResolveStream(ctx, doc, obj)
		if err != nil {
			return nil
		}
		stream = s
	}
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		return nil
	}
	return data
}

// profileDifference describes, for a unification fix, how the profile an
// intent loses relates to the anchor's: the colour of content that intent
// characterises changes only when the bytes differ.
func profileDifference(anchor, replaced []byte) string {
	switch {
	case anchor == nil || replaced == nil:
		return "a profile litho cannot compare"
	case bytes.Equal(anchor, replaced):
		return "an identical profile"
	case len(anchor) >= 20 && len(replaced) >= 20:
		return fmt.Sprintf("a different %q profile replaced by a %q one", replaced[16:20], anchor[16:20])
	default:
		return "a different profile"
	}
}

// maxPlausibleObjNum caps the object numbers fixOutputIntent will tolerate
// before treating the cross-reference index as structurally implausible.
// Anti-DoS guard, not spec-mandated: real documents stay far below 100000
// objects (the ISO 32000-1 architectural limit is 8388607), so a higher
// number signals a damaged index that could corrupt newly added objects.
const maxPlausibleObjNum = 100000

// fixOutputIntent adds a GTS_PDFA1 output intent covering the device colour
// space the violations flag, and injects a Default colour space into resource
// dictionaries for the one that intent leaves uncovered. Under PDF/A-1 it then
// adds an sRGB intent to a file still carrying none when an annotation colour
// array needs one (see [addAnnotColorIntent]).
//
// # Corpus evidence
//
//   - verapdf 6-2-3-3-t01-fail-b: its sole intent is a GTS_PDFA1 CMYK profile
//     over DeviceRGB content, and treating any intent as coverage left it
//     still failing after remediation.
func fixOutputIntent(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, opts Options, violations []compliance.Violation, profile string) ([]string, error) {
	// Skip structurally damaged documents where embedding a large ICC
	// profile risks corruption during serialization.
	if doc.Rebuilt {
		return nil, nil
	}

	// Skip documents with implementation-limit or structural issues that
	// can corrupt newly added objects during serialization. The whole index
	// is read, the objects the raw-byte augmentation added included: this is
	// about the numbers new objects are allocated past, which count them
	// (see reserveDefinedObjects), not a repair of what validation judges.
	for objNum, entry := range doc.XRefEntries() {
		if entry.Offset > doc.FileSize() || int64(objNum) > maxPlausibleObjNum {
			return nil, nil
		}
	}

	// The device colour decision reads the device colour violations alone. A
	// run the annotation colour rule alone brings here has no device colour to
	// cover, and the RGB default of that decision would replace a CMYK intent
	// the source carries.
	device := slices.DeleteFunc(slices.Clone(violations), matchAnnotColor)
	annotColor := len(device) < len(violations)

	var fixes []string
	if len(device) > 0 || !annotColor {
		deviceFixes, err := fixDeviceColorIntent(ctx, alloc, doc, device, compliance.PDFAPart(profile))
		if err != nil {
			return nil, err
		}
		fixes = deviceFixes
	}
	if annotColor {
		if fix := addAnnotColorIntent(ctx, alloc, doc, opts, profile); fix != "" {
			fixes = append(fixes, fix)
		}
	}
	return fixes, nil
}

// addAnnotColorIntent installs the sRGB GTS_PDFA1 output intent under PDF/A-1
// when the catalog, as output-intent's device colour decision left it, carries
// no output intent and some annotation the run keeps carries a colour array.
// It returns the fix description, or "" when it installs nothing.
//
// ISO 19005-1, 6.5.3 forbids an annotation's /C and /IC arrays unless the
// destination profile of the PDF/A-1 output intent (6.2.2) is an RGB one, so
// without an intent annot-color drops them. The sRGB intent keeps them at no
// cost to the content. That decision ran first and installs an intent for
// every device colour space a violation flags (6.2.3.3), so a file still
// carrying none paints no device colour this round, and the sRGB intent is the
// one it would receive for DeviceRGB.
//
// A file that carries an intent keeps it, and annot-color resolves 6.5.3
// against it. A CMYK intent the content needs is the true conflict, where
// dropping the arrays changes the least, and an intent the source declares
// names its intended output condition, which a border colour does not justify
// replacing. A file whose kept annotations carry no colour array gets nothing:
// the intent would serve no rule.
//
// # Corpus evidence
//
//   - All corpora: 22 files at PDF/A-1a and 1b carry no output intent and
//     paint no device colour (govdocs 14, pdfjs 5, verapdf 2, pdfcpu 1).
//     annot-color dropped all 339 of their annotation colour arrays. With the
//     sRGB intent they keep every one, 19 of the files in aggressive mode, and
//     each reports the same rules as before on litho and veraPDF.
func addAnnotColorIntent(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, opts Options, profile string) string {
	if compliance.PDFAPart(profile) != 1 {
		return ""
	}
	catalogRef, ok := catalogRefForFixer(ctx, doc)
	if !ok {
		return ""
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return ""
	}
	if len(liveArray(ctx, alloc, doc, catalogDict["OutputIntents"])) > 0 {
		return ""
	}
	if !annotColorPresent(ctx, alloc, doc, catalogDict, opts, profile) {
		return ""
	}
	// The catalog carries no output intent, so none is kept to report.
	addOutputIntent(ctx, alloc, doc, catalogRef, catalogDict, false, 1)
	return "added sRGB OutputIntent to catalog for annotation colour arrays"
}

// fixDeviceColorIntent is the device colour decision of [fixOutputIntent]:
// it adds the output intent covering the device colour space the violations
// flag, and injects a Default colour space for the one that intent leaves
// uncovered. part is the PDF/A part the run targets (see [addOutputIntent]).
func fixDeviceColorIntent(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, violations []compliance.Violation, part int) ([]string, error) {
	catalogRef, ok := catalogRefForFixer(ctx, doc)
	if !ok {
		return nil, fmt.Errorf("trailer missing /Root")
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, fmt.Errorf("resolving catalog")
	}

	// Classify the colorspaces flagged as needing coverage by the
	// violations. When both CMYK and RGB are flagged only one can be
	// covered by the catalog-level OutputIntent; the other needs a
	// Default{RGB,CMYK} injected into resource dicts.
	needsCMYK := false
	needsRGB := false
	for _, v := range violations {
		ctxStr := strings.ToLower(v.Context)
		if strings.Contains(ctxStr, "devicecmyk") {
			needsCMYK = true
		}
		if strings.Contains(ctxStr, "devicergb") {
			needsRGB = true
		}
	}
	if !needsCMYK && !needsRGB {
		needsRGB = true
	}

	// The destination profiles of the intents as the source lists them, read
	// before an added intent replaces a GTS_PDFA1 one or unification re-points
	// the others: a CMYK one among them is the profile a staged DefaultCMYK
	// reuses (see [reusableDefaultCMYK]).
	sourceProfiles := intentProfiles(ctx, alloc, doc, catalogDict)

	// Inspect existing OutputIntents to avoid duplicating coverage.
	hasCMYKOI, hasRGBOI := false, false
	if oiObj, ok := catalogDict["OutputIntents"]; ok {
		if arr, err := document.ResolveArray(ctx, doc, oiObj); err == nil && len(arr) > 0 {
			hasCMYKOI = hasMatchingOutputIntent(ctx, doc, arr, true)
			hasRGBOI = hasMatchingOutputIntent(ctx, doc, arr, false)
		}
	}

	var fixes []string
	oiIsCMYK := hasCMYKOI
	oiIsRGB := hasRGBOI

	// Decide whether to add a new catalog-level OutputIntent. Skip when
	// the document already has one that covers a needed colorspace.
	addOI := false
	addCMYK := false
	if needsCMYK && !hasCMYKOI && !hasRGBOI {
		addOI = true
		addCMYK = true
	} else if needsRGB && !hasRGBOI {
		// An existing CMYK intent does not license DeviceRGB: ISO 19005-1,
		// 6.2.3.3 admits DeviceRGB only when the output intent's destination
		// profile is an RGB one. Treating any intent as coverage left a file
		// whose sole intent is a GTS_PDFA1 CMYK profile over DeviceRGB
		// content still failing after remediation.
		addOI = true
		addCMYK = false
	}

	if addOI {
		var kept []string
		catalogDict, kept = addOutputIntent(ctx, alloc, doc, catalogRef, catalogDict, addCMYK, part)
		if addCMYK {
			fixes = append(fixes, "added CMYK OutputIntent to catalog")
			oiIsCMYK = true
		} else {
			fixes = append(fixes, "added sRGB OutputIntent to catalog")
			oiIsRGB = true
		}
		fixes = append(fixes, kept...)
	}

	// Inject a Default{RGB,CMYK} for the colorspace the OutputIntent
	// does not cover. Validators only report a single colorspace
	// violation even when a page mixes DeviceCMYK and DeviceRGB, so
	// pre-stage the paired default whenever an OI was added.
	var defaultColor ast.PdfName
	var profile func() ast.PdfStream
	switch {
	case oiIsCMYK && !oiIsRGB:
		defaultColor, profile = "DefaultRGB", sRGBICCStream
	case oiIsRGB && !oiIsCMYK:
		defaultColor, profile = "DefaultCMYK", cmykICCStream
	default:
		return fixes, nil
	}
	// Allocate the ICC profile only when a resource dictionary actually takes
	// it. Allocating up front left a 2.5 KB orphan stream in the output
	// whenever every page already carried the default, so remediating an
	// already-remediated file grew it by that much on every pass while
	// reporting no fixes at all.
	var iccRef ast.PdfIndirectRef
	reused := false
	if defaultColor == "DefaultCMYK" {
		iccRef, reused = reusableDefaultCMYK(ctx, alloc, doc, catalogDict, sourceProfiles, part)
	}
	csArr := func() ast.PdfArray {
		if iccRef.ObjectNum == 0 {
			iccRef = alloc.Alloc(profile())
		}
		return ast.PdfArray{ast.PdfName("ICCBased"), iccRef}
	}
	if n, err := injectDefaultColorSpace(ctx, alloc, doc, catalogDict, defaultColor, csArr); err == nil && n > 0 {
		fix := fmt.Sprintf("injected /%s into %d page(s)", string(defaultColor), n)
		if reused {
			fix += fmt.Sprintf(", over the document's CMYK profile %d 0 R", iccRef.ObjectNum)
		}
		fixes = append(fixes, fix)
	}

	return fixes, nil
}

// addOutputIntent makes a GTS_PDFA1 output intent carrying a CMYK profile,
// or an RGB one when cmyk is false, the catalog's PDF/A output intent, and
// returns the rewritten catalog with a fix description for each intent it
// keeps. part is the PDF/A part the run targets.
//
// ISO 32000-1, 14.11.5 admits several output intents, one per output
// condition, so the intents of other subtypes the catalog lists, GTS_PDFX or
// ISO_PDFE1, stay after the new one. A GTS_PDFA1 intent already there is
// replaced: the caller adds an intent only when none covers the device colour
// the content paints, and a second GTS_PDFA1 intent would name a second PDF/A
// output condition. The new intent goes first, where the readers of the
// first entry's profile ([firstOutputIntentProfile]) find it.
//
// ISO 19005-1, 6.2.2 and ISO 19005-2/3/4, 6.2.3 t2 then require every intent
// carrying a /DestOutputProfile to name the same indirect object. When a kept
// intent's profile is one PDF/A admits as the destination profile for the
// colour space the new intent covers (see [reusableIntentProfile]), the new
// intent names that object, with the kept intent's output condition, so
// neither intent's reading changes. Otherwise the new intent carries litho's
// stock profile and every kept intent naming a profile is pointed at it, the
// unification [fixOutputIntentCleanup] applies: the PDF/A profile stays and
// the kept intents keep their /OutputConditionIdentifier.
//
// An ICCBased colour space whose profile the new intent now repeats violates
// ISO 19005-4, 6.2.4.2, so the same pass replaces every page's copy with the
// device space the intent covers.
//
// # Corpus evidence
//
//   - All corpora: 12 files list a GTS_PDFX intent and no GTS_PDFA1 one
//     (govdocs 1, pdfjs 5, pdfcpu 1, pdf20 1, verapdf 4). Replacing the array
//     dropped the PDF/X intent from all 264 of their outputs at the eleven
//     PDF/A levels, default and aggressive; every output now keeps it, and
//     each reports the rule set it reported before on litho.
//   - litho pdfxonly_2b and pdfxnoprofile_2b: remediated at the eleven PDF/A
//     levels, every output keeps the PDF/X intent and reports the same rule
//     set on litho and veraPDF.
func addOutputIntent(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, catalogRef ast.PdfIndirectRef, catalogDict ast.PdfDict, cmyk bool, part int) (ast.PdfDict, []string) {
	kept := keptOutputIntents(ctx, alloc, doc, liveArray(ctx, alloc, doc, catalogDict["OutputIntents"]))

	var outputIntent ast.PdfDict
	iccRef, source, reused := reusableIntentProfile(ctx, alloc, doc, kept, cmyk, part)
	if reused {
		outputIntent = ast.PdfDict{
			"Type":              ast.PdfName("OutputIntent"),
			"S":                 ast.PdfName("GTS_PDFA1"),
			"DestOutputProfile": iccRef,
		}
		for _, key := range []ast.PdfName{"OutputConditionIdentifier", "OutputCondition", "RegistryName", "Info"} {
			if v, ok := source[key]; ok {
				outputIntent[key] = v
			}
		}
		if _, ok := outputIntent["OutputConditionIdentifier"]; !ok {
			// ISO 32000-1, 14.11.5, Table 365 makes the identifier required;
			// "Custom" names a condition the profile alone characterizes.
			outputIntent["OutputConditionIdentifier"] = ast.PdfString("Custom")
		}
	} else {
		iccStream, conditionID := cmykICCStream(), cmykCondition
		if !cmyk {
			iccStream, conditionID = sRGBICCStream(), "sRGB IEC61966-2.1"
		}
		iccRef = alloc.Alloc(iccStream)

		// ISO 32000-2 Table 401 and ISO 19005-4 §6.2.3 keep GTS_PDFA1 as
		// the PDF/A output-intent subtype for every part; a "GTS_PDFA"
		// subtype does not exist, so an intent carrying it does not satisfy
		// the device-colour requirement of §6.2.4.3.
		outputIntent = ast.PdfDict{
			"Type":                      ast.PdfName("OutputIntent"),
			"S":                         ast.PdfName("GTS_PDFA1"),
			"OutputConditionIdentifier": ast.PdfString(conditionID),
			"DestOutputProfile":         iccRef,
		}
		if cmyk {
			outputIntent["RegistryName"] = ast.PdfString(cmykRegistry)
		}
	}
	oiRef := alloc.Alloc(outputIntent)

	intents := ast.PdfArray{oiRef}
	var notes []string
	for _, it := range kept {
		subtype := intentSubtype(ctx, doc, it.dict)
		dp, has := it.dict["DestOutputProfile"]
		if dpRef, isRef := dp.(ast.PdfIndirectRef); !has || (isRef && dpRef == iccRef) || entryResolvesToNull(ctx, doc, dp) {
			note := fmt.Sprintf("kept OutputIntent[%d] (/S /%s) beside the GTS_PDFA1 intent", len(intents), subtype)
			if has && !entryResolvesToNull(ctx, doc, dp) {
				note += ", which shares its destination profile"
			}
			notes = append(notes, note)
			intents = append(intents, it.entry)
			continue
		}
		notes = append(notes, fmt.Sprintf("kept OutputIntent[%d] (/S /%s) beside the GTS_PDFA1 intent, its /DestOutputProfile unified with the GTS_PDFA1 one: %s",
			len(intents), subtype, profileDifference(decodedProfile(ctx, alloc, doc, iccRef), decodedProfile(ctx, alloc, doc, dp))))
		repointed := make(ast.PdfDict, len(it.dict))
		maps.Copy(repointed, it.dict)
		repointed["DestOutputProfile"] = iccRef
		if it.ref.ObjectNum > 0 {
			alloc.AllocAt(it.ref.ObjectNum, repointed)
			intents = append(intents, it.ref)
		} else {
			intents = append(intents, repointed)
		}
	}

	newCatalog := make(ast.PdfDict, len(catalogDict)+1)
	maps.Copy(newCatalog, catalogDict)
	newCatalog["OutputIntents"] = intents
	alloc.AllocAt(catalogRef.ObjectNum, newCatalog)

	newCat := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if newCat == nil {
		return newCatalog, notes
	}
	if profile := firstOutputIntentProfile(ctx, alloc, doc, newCat); profile != nil {
		visited := make(map[int]bool)
		var entries []annotPageEntry
		if pagesObj, ok := newCat["Pages"]; ok {
			collectPageRefs(ctx, alloc, doc, pagesObj, &entries, make(map[int]bool))
		}
		for _, pe := range entries {
			profiles := profileSet{
				outputIntent: profile,
				blending:     blendingProfile(ctx, alloc, doc, pe.dict),
			}
			dedupICCInResources(ctx, alloc, doc, pe, profiles.all(), visited)
			dedupICCInXObjects(ctx, alloc, doc, pe, profiles, visited)
		}
	}
	return newCatalog, notes
}

// keptIntent is one /OutputIntents entry [addOutputIntent] keeps: the entry
// as the array lists it, the object it lives at (zero for an intent written
// directly in the array) and its dictionary as this run sees it.
type keptIntent struct {
	entry ast.PdfObject
	ref   ast.PdfIndirectRef
	dict  ast.PdfDict
}

// keptOutputIntents returns the entries of an /OutputIntents array that
// [addOutputIntent] keeps: every output intent dictionary whose /S is not
// GTS_PDFA1. An entry that names no dictionary is no output intent (ISO
// 32000-1, 14.11.5) and is dropped, as replacing the whole array dropped it.
func keptOutputIntents(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, arr ast.PdfArray) []keptIntent {
	var kept []keptIntent
	for _, entry := range arr {
		it := keptIntent{entry: entry}
		if ref, ok := entry.(ast.PdfIndirectRef); ok && ref.ObjectNum > 0 {
			it.ref = ref
			it.dict = resolveFromAllocOrDoc(ctx, alloc, doc, ref)
		}
		if it.dict == nil {
			d, err := document.ResolveDict(ctx, doc, entry)
			if err != nil || d == nil {
				continue
			}
			it.dict = d
		}
		if intentSubtype(ctx, doc, it.dict) == "GTS_PDFA1" {
			continue
		}
		kept = append(kept, it)
	}
	return kept
}

// iccProfileHeaderLen is the size of an ICC profile header (ISO 15076-1, 7.2).
const iccProfileHeaderLen = 128

// reusableIntentProfile returns the destination profile of the first kept
// intent that PDF/A admits as the profile of a GTS_PDFA1 intent covering CMYK
// (cmyk) or RGB, with that intent's dictionary, and false when none does.
//
// The profile must carry a full header, a printer or monitor device class, the
// colour space the intent covers, the /N that colour space takes, and an ICC
// major version of 2 under PDF/A-1 or of 2 or 4 under the later parts: the
// tests ISO 19005-1, 6.2.2 t1 and 6.2.3.2 t2 and ISO 19005-2/3/4, 6.2.3 t1
// apply to a destination profile. A profile written directly in the intent has
// no object number for the other intents to share (6.2.3 t2), so only an
// indirect one qualifies.
func reusableIntentProfile(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, kept []keptIntent, cmyk bool, part int) (ast.PdfIndirectRef, ast.PdfDict, bool) {
	for _, it := range kept {
		ref, ok := it.dict["DestOutputProfile"].(ast.PdfIndirectRef)
		if !ok || ref.ObjectNum == 0 {
			continue
		}
		if admittedDestinationProfile(ctx, alloc, doc, ref, cmyk, part) {
			return ref, it.dict, true
		}
	}
	return ast.PdfIndirectRef{}, nil, false
}

// admittedDestinationProfile reports whether the indirect ICC profile stream
// ref names is one PDF/A admits as the destination profile of a GTS_PDFA1
// intent covering CMYK (cmyk) or RGB: the tests [reusableIntentProfile] lists.
// The stream is read as this run left it, so a profile an earlier fixer
// rewrote is judged by its replacement.
func admittedDestinationProfile(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, ref ast.PdfIndirectRef, cmyk bool, part int) bool {
	colorSpace, components := "RGB ", 3
	if cmyk {
		colorSpace, components = "CMYK", 4
	}
	stream, ok := resolveStreamFromAllocOrDoc(ctx, alloc, doc, ref, ref)
	if !ok {
		return false
	}
	if n, err := document.ResolveInt(ctx, doc, stream.Dict["N"]); err != nil || int(n) != components {
		return false
	}
	data := decodedProfile(ctx, alloc, doc, ref)
	if len(data) < iccProfileHeaderLen || string(data[16:20]) != colorSpace {
		return false
	}
	if class := string(data[12:16]); class != "prtr" && class != "mntr" {
		return false
	}
	version := data[8]
	return version == 2 || (version == 4 && part != 1)
}

// reusableDefaultCMYK returns the CMYK destination profile of an output intent
// the source lists, as the profile of the DefaultCMYK [fixDeviceColorIntent]
// stages beside an RGB intent, and false when none can serve, in which case
// the stock profile does. profiles are the destination profiles of the
// intents the source lists ([intentProfiles]), read before the run replaced or
// re-pointed any entry, so a PDF/X CMYK profile unification leaves
// unreferenced is still found.
//
// Reusing it keeps the producer's characterisation of its CMYK, which the
// stock profile replaces with a generic one, and spares the output a second
// CMYK profile. The profile must be one PDF/A admits as a CMYK destination
// profile ([admittedDestinationProfile]): those tests are a subset of the ones
// ISO 19005-1, 6.2.3.2 and ISO 19005-2/3/4, 6.2.4.2 t1 apply to an ICCBased
// profile, so it is admitted there too. Under PDF/A-4 a profile a transparency
// group blends in is skipped, since ISO 19005-4, 6.2.4.2 t3 forbids an
// ICCBased space identical to it (see [blendsInProfile]).
//
// The profiles are tried in array order, so the choice is deterministic.
//
// # Corpus evidence
//
//   - All corpora: 107 files list an intent over a CMYK profile (verapdf 73,
//     litho 31, pdfjs 2, govdocs 1). Remediated at the eleven PDF/A levels,
//     default and aggressive, only litho pdfxnoprofile_2b stages a DefaultCMYK
//     over it, in all 22 cells, each output about 28 KB smaller, and no cell
//     of the 2354 changes the rule set litho reports. veraPDF reports the same
//     rule set as before in all 22.
func reusableDefaultCMYK(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, catalog ast.PdfDict, profiles []ast.PdfIndirectRef, part int) (ast.PdfIndirectRef, bool) {
	for _, ref := range profiles {
		if !admittedDestinationProfile(ctx, alloc, doc, ref, true, part) {
			continue
		}
		if part == 4 && blendsInProfile(ctx, alloc, doc, catalog, decodedProfile(ctx, alloc, doc, ref)) {
			continue
		}
		return ref, true
	}
	return ast.PdfIndirectRef{}, false
}

// intentProfiles returns the indirect /DestOutputProfile of every output
// intent the catalog lists, in array order. A profile written directly in its
// intent has no object number a colour space could name, and is left out.
func intentProfiles(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, catalog ast.PdfDict) []ast.PdfIndirectRef {
	var refs []ast.PdfIndirectRef
	for _, entry := range liveArray(ctx, alloc, doc, catalog["OutputIntents"]) {
		intent := liveDictValue(ctx, alloc, doc, entry)
		if ref, ok := intent["DestOutputProfile"].(ast.PdfIndirectRef); ok && ref.ObjectNum > 0 {
			refs = append(refs, ref)
		}
	}
	return refs
}

// blendsInProfile reports whether a transparency group of the document blends
// in an ICC profile identical to profile, as [compliance.ICCProfilesEqual]
// compares them: the group of a page, of a Form XObject or tiling pattern its
// resources reach, nested to [maxTransparencyResourceDepth], or of an
// annotation's normal appearance. An unreadable object counts as no group.
func blendsInProfile(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, catalog ast.PdfDict, profile []byte) bool {
	if len(profile) == 0 {
		return false
	}
	var pages []annotPageEntry
	collectPageRefs(ctx, alloc, doc, catalog["Pages"], &pages, make(map[int]bool))
	seen := make(map[int]bool)
	for _, pe := range pages {
		if ctx.Err() != nil {
			return false
		}
		if groupBlendsIn(ctx, alloc, doc, pe.dict, profile) {
			return true
		}
		if holder, ok := resourcesHolder(ctx, alloc, doc, pe); ok &&
			resourcesBlendIn(ctx, alloc, doc, holder.dict["Resources"], profile, seen, 0) {
			return true
		}
		for _, annot := range liveArray(ctx, alloc, doc, pe.dict["Annots"]) {
			if document.VisitedBefore(annot, seen) {
				continue
			}
			ap := liveDictValue(ctx, alloc, doc, liveDictValue(ctx, alloc, doc, annot)["AP"])
			if appearanceBlendsIn(ctx, alloc, doc, ap["N"], profile, seen) {
				return true
			}
		}
	}
	return false
}

// appearanceBlendsIn reports whether a normal appearance, one form or a
// subdictionary of appearance states each naming one (ISO 32000-1, 12.5.5),
// blends in profile ([formBlendsIn]).
func appearanceBlendsIn(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, normal ast.PdfObject, profile []byte, seen map[int]bool) bool {
	if normal == nil {
		return false
	}
	ref, _ := normal.(ast.PdfIndirectRef)
	if _, isStream := resolveStreamFromAllocOrDoc(ctx, alloc, doc, ref, normal); isStream {
		return formBlendsIn(ctx, alloc, doc, normal, profile, seen, 0)
	}
	if document.VisitedBefore(normal, seen) {
		return false
	}
	states := liveDictValue(ctx, alloc, doc, normal)
	for _, name := range slices.Sorted(maps.Keys(states)) {
		if formBlendsIn(ctx, alloc, doc, states[name], profile, seen, 0) {
			return true
		}
	}
	return false
}

// groupBlendsIn reports whether the /Group of dict blends in profile
// ([blendingProfile]).
func groupBlendsIn(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, dict ast.PdfDict, profile []byte) bool {
	blending := blendingProfile(ctx, alloc, doc, dict)
	return len(blending) > 0 && compliance.ICCProfilesEqual(blending, profile)
}

// resourcesBlendIn reports whether a Form XObject or tiling pattern the
// resource dictionary res names, or one nested in theirs, declares a group
// blending in profile. The depth cap is [maxTransparencyResourceDepth], the
// cap of the transparency fixer's walk over the same nesting.
func resourcesBlendIn(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, res ast.PdfObject, profile []byte, seen map[int]bool, depth int) bool {
	if res == nil || ctx.Err() != nil || depth > maxTransparencyResourceDepth || document.VisitedBefore(res, seen) {
		return false
	}
	resDict := liveDictValue(ctx, alloc, doc, res)
	for _, category := range []ast.PdfName{"XObject", "Pattern"} {
		sub := liveDictValue(ctx, alloc, doc, resDict[category])
		for _, name := range slices.Sorted(maps.Keys(sub)) {
			if formBlendsIn(ctx, alloc, doc, sub[name], profile, seen, depth+1) {
				return true
			}
		}
	}
	return false
}

// formBlendsIn reports whether the stream obj names, a Form XObject or a
// tiling pattern, declares a group blending in profile, or reaches one through
// its own /Resources.
func formBlendsIn(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, obj ast.PdfObject, profile []byte, seen map[int]bool, depth int) bool {
	if obj == nil || document.VisitedBefore(obj, seen) {
		return false
	}
	ref, _ := obj.(ast.PdfIndirectRef)
	stream, ok := resolveStreamFromAllocOrDoc(ctx, alloc, doc, ref, obj)
	if !ok {
		return false
	}
	if groupBlendsIn(ctx, alloc, doc, stream.Dict, profile) {
		return true
	}
	return resourcesBlendIn(ctx, alloc, doc, stream.Dict["Resources"], profile, seen, depth)
}

// injectDefaultColorSpace walks the page tree plus every reachable Form
// XObject and Pattern resource dict, adding /Default{RGB,CMYK,Gray} where
// missing. Patterns and Form XObjects have their own /Resources that PDF/A
// §6.2.3.3 treats as independent scopes, so a page-only fix leaves
// "used in pattern/form" violations behind. A shading pattern has none, so
// its shading takes the Default space in place of the device family itself.
//
// # Corpus evidence
//
//   - litho resinherit_2b.pdf: the page whose /Parent names the root was given
//     the root's /Resources with the default space, so its text named /F5,
//     which nothing defined any more, and the page whose /Resources is null
//     was given none, so it kept 6.2.2 t2 after a PDF/A-2b remediation, on
//     litho and veraPDF, default and aggressive. Each now gets its node's,
//     and /F5, an unembedded font, adds a check to the embedding rule no
//     repair meets (6.2.11.4.1 t1, and 6.3.4 t1 at PDF/A-1b).
func injectDefaultColorSpace(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, catalog ast.PdfDict, key ast.PdfName, csArr func() ast.PdfArray) (int, error) {
	pagesObj, ok := catalog["Pages"]
	if !ok {
		return 0, nil
	}
	var pages []annotPageEntry
	collectPageRefs(ctx, alloc, doc, pagesObj, &pages, make(map[int]bool))

	visited := make(map[int]bool)
	count := 0
	for _, pe := range pages {
		if ctx.Err() != nil {
			return count, ctx.Err()
		}
		// A page with no /Resources of its own inherits them from an ancestor
		// (ISO 32000-2, 7.7.3.4). Writing a fresh dictionary here would SHADOW
		// the inherited one, making every inherited font and XObject
		// unreachable, so the inherited dictionary is the base to patch. Only
		// a page that truly inherits nothing gets a new one.
		pe.dict = liveTreeNode(ctx, alloc, doc, pe)
		node, ok := resourcesNode(ctx, alloc, doc, pe)
		if !ok {
			newRes := ast.PdfDict{"ColorSpace": ast.PdfDict{key: csArr()}}
			newPage := make(ast.PdfDict, len(pe.dict)+1)
			maps.Copy(newPage, pe.dict)
			newPage["Resources"] = newRes
			if writeTreeNode(ctx, alloc, doc, pe, newPage) {
				count++
			}
			continue
		}
		patched := patchResourceColorSpace(ctx, alloc, doc, node.dict["Resources"], key, csArr, visited)
		if patched.changed {
			newPage := make(ast.PdfDict, len(pe.dict)+1)
			maps.Copy(newPage, pe.dict)
			newPage["Resources"] = patched.value
			if writeTreeNode(ctx, alloc, doc, pe, newPage) {
				count++
			}
		}
	}
	return count, nil
}

// patchResult is the return shape of patchResourceColorSpace: the updated
// resource dict (inline or indirect) and whether anything changed.
type patchResult struct {
	value   ast.PdfObject
	changed bool
}

// patchResourceColorSpace ensures the given /Resources object defines
// `key` inside its ColorSpace dict and recurses into every Form XObject
// and Pattern referenced by XObject/Pattern entries. Returns the value
// to assign back to the parent (unchanged when nothing needed patching).
func patchResourceColorSpace(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, resources ast.PdfObject, key ast.PdfName, csArr func() ast.PdfArray, visited map[int]bool) patchResult {
	var resDict ast.PdfDict
	var resRef ast.PdfIndirectRef
	if ref, ok := resources.(ast.PdfIndirectRef); ok {
		resRef = ref
		if visited[ref.ObjectNum] {
			return patchResult{value: resources}
		}
		visited[ref.ObjectNum] = true
		resDict = resolveFromAllocOrDoc(ctx, alloc, doc, ref)
	}
	if resDict == nil {
		if d, err := document.ResolveDict(ctx, doc, resources); err == nil {
			resDict = d
		}
	}
	if resDict == nil {
		return patchResult{value: resources}
	}

	changed := false
	newRes := make(ast.PdfDict, len(resDict)+1)
	maps.Copy(newRes, resDict)

	// Read alloc-first: the copy below is inlined into newRes, and
	// fixOutputIntent's dedup pass has already rewritten an indirect
	// /ColorSpace dictionary at its own object number. Copying the document's
	// version would restore the duplicated ICCBased space and orphan that
	// write. An unreadable /ColorSpace resource yields a nil map, handled by the
	// nil check below.
	csMap := liveDictValue(ctx, alloc, doc, resDict["ColorSpace"])
	if csMap == nil || csMap[key] == nil {
		base := csMap
		if base == nil {
			base = ast.PdfDict{}
		}
		newCS := make(ast.PdfDict, len(base)+1)
		maps.Copy(newCS, base)
		newCS[key] = csArr()
		newRes["ColorSpace"] = newCS
		changed = true
	}

	changedDown := recurseColorSpacePatch(ctx, alloc, doc, newRes, "XObject", key, csArr, visited)
	changedDown = recurseColorSpacePatch(ctx, alloc, doc, newRes, "Pattern", key, csArr, visited) || changedDown
	changedDown = patchType3FontColorSpace(ctx, alloc, doc, newRes, key, csArr, visited) || changedDown
	if changedDown {
		changed = true
	}

	if !changed {
		return patchResult{value: resources}
	}
	if resRef.ObjectNum > 0 {
		alloc.AllocAt(resRef.ObjectNum, newRes)
		return patchResult{value: resRef, changed: true}
	}
	return patchResult{value: newRes, changed: true}
}

// patchType3FontColorSpace ensures every Type 3 font in the /Font resource
// sub-dict defines `key` in its own /Resources ColorSpace, creating the
// Resources dict when absent. CharProcs are a sub-scope like Form XObjects:
// a page-level DefaultCS never covers their operators (ISO 32000-2
// §8.6.5.7), so the Default must live on the font's own /Resources to
// take effect inside the glyph descriptions.
func patchType3FontColorSpace(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, newRes ast.PdfDict, key ast.PdfName, csArr func() ast.PdfArray, visited map[int]bool) bool {
	fontObj, ok := newRes["Font"]
	if !ok {
		return false
	}
	fontDict, err := document.ResolveDict(ctx, doc, fontObj)
	if err != nil || fontDict == nil {
		return false
	}
	changed := false
	// Sorted: the walk marks what it visits, so the order decides which font
	// reaches a shared /Resources first and is rewritten (see
	// [recurseColorSpacePatch]).
	for _, name := range slices.Sorted(maps.Keys(fontDict)) {
		ref, isRef := fontDict[name].(ast.PdfIndirectRef)
		if !isRef || ref.ObjectNum == 0 {
			continue
		}
		if visited[ref.ObjectNum] {
			continue
		}
		visited[ref.ObjectNum] = true

		fd := resolveFromAllocOrDoc(ctx, alloc, doc, ref)
		if fd == nil {
			continue
		}
		if subtype, _ := document.ResolveName(ctx, doc, fd["Subtype"]); subtype != "Type3" {
			continue
		}

		resObj, hasRes := fd["Resources"]
		if !hasRes {
			newFont := make(ast.PdfDict, len(fd)+1)
			maps.Copy(newFont, fd)
			newFont["Resources"] = ast.PdfDict{"ColorSpace": ast.PdfDict{key: csArr()}}
			alloc.AllocAt(ref.ObjectNum, newFont)
			changed = true
			continue
		}
		patched := patchResourceColorSpace(ctx, alloc, doc, resObj, key, csArr, visited)
		if !patched.changed {
			continue
		}
		newFont := make(ast.PdfDict, len(fd)+1)
		maps.Copy(newFont, fd)
		newFont["Resources"] = patched.value
		alloc.AllocAt(ref.ObjectNum, newFont)
		changed = true
	}
	return changed
}

// recurseColorSpacePatch walks a named resource sub-dict (XObject or
// Pattern) and updates each entry that carries its own /Resources, and the
// shading of each shading pattern, which carries none (see
// [patchShadingPatternSpace]).
//
// The entries are walked in sorted name order. visited marks each object the
// walk reaches, so of two entries sharing one indirect /Resources only the
// first patches it and is rewritten, and the second is left as it was. Walked
// in Go's map order, which of the two that was changed from run to run
// (CONTRIBUTING "Deterministic output").
//
// # Corpus evidence
//
//   - litho fontcopies_2b.pdf: page 15 draws /X1 and /X2, two identical forms
//     sharing /Resources 67 0 R, and a PDF/A-1b remediation rewrote form 48
//     instead of form 47 in 7 of 40 runs.
func recurseColorSpacePatch(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, newRes ast.PdfDict, name ast.PdfName, key ast.PdfName, csArr func() ast.PdfArray, visited map[int]bool) bool {
	subObj, ok := newRes[name]
	if !ok {
		return false
	}
	subDict := liveDictValue(ctx, alloc, doc, subObj)
	if subDict == nil {
		return false
	}
	changed := false
	var directPatterns ast.PdfDict
	for _, entryName := range slices.Sorted(maps.Keys(subDict)) {
		ref, isRef := subDict[entryName].(ast.PdfIndirectRef)
		if !isRef && name == "Pattern" {
			// A shading pattern written directly into /Pattern is rewritten
			// in the copy of the sub-dictionary below.
			if pattern, ok := subDict[entryName].(ast.PdfDict); ok {
				if newPattern, _ := patchShadingPatternSpace(ctx, alloc, doc, pattern, key, csArr, visited); newPattern != nil {
					if directPatterns == nil {
						directPatterns = make(ast.PdfDict, len(subDict))
						maps.Copy(directPatterns, subDict)
					}
					directPatterns[entryName] = newPattern
				}
			}
			continue
		}
		if !isRef || ref.ObjectNum == 0 {
			continue
		}
		if visited[ref.ObjectNum] {
			continue
		}
		visited[ref.ObjectNum] = true

		// Resolve the raw object: XObject Form and TilingPattern entries
		// are streams, ShadingPattern entries are dicts, so pre-resolve
		// and branch on type. Read alloc-first: fixOutputIntent's dedup pass
		// has already rewritten a form whose inline /ColorSpace duplicated the
		// new intent, and the rebuilt object is written back at ref below, so
		// the document's copy would restore the duplicate.
		rawObj, pending := alloc.Pending()[ref.ObjectNum]
		if !pending {
			resolved, err := doc.Resolve(ctx, ref)
			if err != nil || resolved == nil {
				continue
			}
			rawObj = resolved
		}
		if stream, isStream := rawObj.(ast.PdfStream); isStream {
			resObj, hasRes := stream.Dict["Resources"]
			if !hasRes {
				continue
			}
			patched := patchResourceColorSpace(ctx, alloc, doc, resObj, key, csArr, visited)
			if !patched.changed {
				continue
			}
			newDict := make(ast.PdfDict, len(stream.Dict)+1)
			maps.Copy(newDict, stream.Dict)
			newDict["Resources"] = patched.value
			raw, err := stream.Bytes(ctx)
			if err != nil {
				// Form XObject body unreadable: skip the Resources
				// patch rather than rewriting the stream with empty
				// content. The validator will keep flagging the
				// underlying issue, but at least the appearance stream
				// stays intact.
				doc.Logger.Debug("remediation: skipping Form XObject Resources rewrite, body read failed",
					slog.Int("obj", ref.ObjectNum), slog.Any("err", err))
				continue
			}
			alloc.AllocAt(ref.ObjectNum, ast.NewStream(newDict, raw))
			changed = true
			continue
		}
		if target, ok := rawObj.(ast.PdfDict); ok {
			if name == "Pattern" {
				newPattern, wrote := patchShadingPatternSpace(ctx, alloc, doc, target, key, csArr, visited)
				if newPattern != nil {
					alloc.AllocAt(ref.ObjectNum, newPattern)
				}
				if wrote {
					changed = true
					continue
				}
			}
			resObj, hasRes := target["Resources"]
			if !hasRes {
				continue
			}
			patched := patchResourceColorSpace(ctx, alloc, doc, resObj, key, csArr, visited)
			if !patched.changed {
				continue
			}
			newTarget := make(ast.PdfDict, len(target)+1)
			maps.Copy(newTarget, target)
			newTarget["Resources"] = patched.value
			alloc.AllocAt(ref.ObjectNum, newTarget)
			changed = true
		}
	}
	if directPatterns != nil {
		if subRef, ok := subObj.(ast.PdfIndirectRef); ok && subRef.ObjectNum > 0 {
			alloc.AllocAt(subRef.ObjectNum, directPatterns)
		} else {
			newRes[name] = directPatterns
		}
		changed = true
	}
	return changed
}
