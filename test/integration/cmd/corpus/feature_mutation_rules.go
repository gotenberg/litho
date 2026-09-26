package main

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// ruleMatch names one rule id ("6.1.3-t02") or, when it ends in "." or "-",
// every rule of a clause and its subclauses ("6.1." for "6.1.3-t02" and
// "6.1.13-t11", "7.16-" for "7.16-t01").
type ruleMatch string

// matches reports whether id falls under m.
func (m ruleMatch) matches(id string) bool {
	s := string(m)
	if strings.HasSuffix(s, ".") || strings.HasSuffix(s, "-") {
		return strings.HasPrefix(id, s)
	}
	return id == s
}

// matchesAny reports whether id falls under any of ms.
func matchesAny(ms []ruleMatch, id string) bool {
	for _, m := range ms {
		if m.matches(id) {
			return true
		}
	}
	return false
}

// ruleAllowance is how far a mutation may move litho's rule set at one
// profile, the "except where the mutation must change it" of the mutate-*
// features. Every other change fails the cell with ruleset_changed.
type ruleAllowance struct {
	// mayAdd lists the rules the output may fail although the input does not.
	mayAdd []ruleMatch
	// mayRemove lists the rules the input fails that the output may pass.
	mayRemove []ruleMatch
	// anyRemove lets the output pass any rule the input fails, for a mutation
	// that removes content or structure by design.
	anyRemove bool
	// mustFail lists the rules the output has to fail, the change the mutation
	// is for: an encrypted PDF/A output fails the Encrypt rule.
	mustFail []string
	// mustPass lists the rules the output has to pass: an auto-tagged output
	// has a structure tree.
	mustPass []string
}

// check compares the input's rule ids with the output's and returns what
// breaks the allowance, "" when nothing does. Both inputs are sets: order and
// multiplicity do not matter.
func (a ruleAllowance) check(in, out []string) string {
	inSet := make(map[string]bool, len(in))
	for _, id := range in {
		inSet[id] = true
	}
	outSet := make(map[string]bool, len(out))
	for _, id := range out {
		outSet[id] = true
	}

	var added, removed, missing, kept []string
	for id := range outSet {
		if !inSet[id] && !matchesAny(a.mayAdd, id) && !slices.Contains(a.mustFail, id) {
			added = append(added, id)
		}
	}
	for id := range inSet {
		if !outSet[id] && !a.anyRemove && !matchesAny(a.mayRemove, id) {
			removed = append(removed, id)
		}
	}
	for _, id := range a.mustFail {
		if !outSet[id] {
			missing = append(missing, id)
		}
	}
	for _, id := range a.mustPass {
		if outSet[id] {
			kept = append(kept, id)
		}
	}

	var parts []string
	for _, p := range []struct {
		label string
		ids   []string
	}{
		{"added", added},
		{"removed", removed},
		{"does not fail", missing},
		{"still fails", kept},
	} {
		if len(p.ids) == 0 {
			continue
		}
		sort.Strings(p.ids)
		parts = append(parts, fmt.Sprintf("%s %s", p.label, strings.Join(p.ids, " ")))
	}
	return strings.Join(parts, "; ")
}

// Rule families, keyed by the numbering a profile directory uses: "A1" for
// ISO 19005-1, "A2" for ISO 19005-2 and -3 (part 3 keeps part 2's clause
// numbers), "A4" for ISO 19005-4, "UA1" for ISO 14289-1, and "UA2" for
// ISO 14289-2 and the WTPDF levels that adopt it.
const (
	familyA1  = "A1"
	familyA2  = "A2"
	familyA4  = "A4"
	familyUA1 = "UA1"
	familyUA2 = "UA2"
)

// ruleFamily returns the numbering family of a profile directory, "" for one
// it does not know.
func ruleFamily(profileDir string) string {
	switch {
	case strings.HasPrefix(profileDir, "PDF_A-1"):
		return familyA1
	case strings.HasPrefix(profileDir, "PDF_A-2"), strings.HasPrefix(profileDir, "PDF_A-3"):
		return familyA2
	case strings.HasPrefix(profileDir, "PDF_A-4"):
		return familyA4
	case profileDir == "PDF_UA-1":
		return familyUA1
	case profileDir == "PDF_UA-2", strings.HasPrefix(profileDir, "WTPDF-"):
		return familyUA2
	}
	return ""
}

// tagsContent reports whether a profile judges the structure tree: the level
// A conformance of ISO 19005 and every accessibility profile. Tagging changes
// only these profiles' rule sets.
func tagsContent(profileDir string) bool {
	switch profileDir {
	case "PDF_A-1a", "PDF_A-2a", "PDF_A-3a", "PDF_UA-1", "PDF_UA-2":
		return true
	}
	return strings.HasPrefix(profileDir, "WTPDF-")
}

// fileStructureRules are the clauses that judge the file's serialization
// rather than its content: ISO 19005-1, -2 and -4 clause 6.1 (header,
// trailer, cross-reference, stream and object syntax, implementation limits),
// the header rule veraPDF files under ISO 14289-1, 6.1, and ISO 14289-1, 7.16
// on the encryption dictionary. Every mutation writes a new revision or a new
// file, and the Writer decrypts its output unless told to encrypt, so any of
// them may pass a rule of these the input failed. None may fail a new one.
//
//nolint:gochecknoglobals // static rule table
var fileStructureRules = map[string][]ruleMatch{
	familyA1:  {"6.1."},
	familyA2:  {"6.1."},
	familyA4:  {"6.1."},
	familyUA1: {"6.1-", "7.16-"},
}

// fontEmbeddingRules require every font used for rendering to be embedded:
// ISO 19005-1, 6.3.4, ISO 19005-2, 6.2.11.4.1, ISO 19005-4, 6.2.10.4.1,
// ISO 14289-1, 7.21.4.1 and ISO 14289-2, 8.4.5.5.1. A text stamp's default
// font is a standard font litho does not embed, which [pdf.Writer.StampText]
// documents.
//
//nolint:gochecknoglobals // static rule table
var fontEmbeddingRules = map[string][]ruleMatch{
	familyA1:  {"6.3.4-t01"},
	familyA2:  {"6.2.11.4.1-t01"},
	familyA4:  {"6.2.10.4.1-t01"},
	familyUA1: {"7.21.4.1-t01"},
	familyUA2: {"8.4.5.5.1-t01"},
}

// deviceRGBRules forbid DeviceRGB without an RGB output intent or a
// DefaultRGB: ISO 19005-1, 6.2.3.3 and ISO 19005-2 and -4, 6.2.4.3. A text
// stamp paints its fill colour in DeviceRGB.
//
//nolint:gochecknoglobals // static rule table
var deviceRGBRules = map[string][]ruleMatch{
	familyA1: {"6.2.3.3-t01"},
	familyA2: {"6.2.4.3-t02"},
	familyA4: {"6.2.4.3-t02"},
}

// explicitResourcesRules require a content stream that uses resources to have
// an explicitly associated /Resources dictionary, ISO 19005-2 and -4, 6.2.2
// test 2. A stamp and a page copy write /Resources on the page, so a page
// that inherited its resources passes afterwards.
//
//nolint:gochecknoglobals // static rule table
var explicitResourcesRules = map[string][]ruleMatch{
	familyA2: {"6.2.2-t02"},
	familyA4: {"6.2.2-t02"},
}

// textLanguageRules require the language of page text to be determined,
// ISO 14289-1, 7.2 test 34. A stamp's text carries no /Lang, so a document
// with no catalog /Lang fails it once stamped.
//
// # Corpus evidence
//
//   - litho imagereach_2b: veraPDF also fails 7.2 test 34 on the stamped
//     output and not on the input, so the rule reaches the artifact's text.
//
//nolint:gochecknoglobals // static rule table
var textLanguageRules = map[string][]ruleMatch{
	familyUA1: {"7.2-t34"},
}

// structTreeRules require a structure tree rooted in the catalog's
// /StructTreeRoot: ISO 19005-2, 6.7.3.3, ISO 14289-1, 7.1 test 11 and
// ISO 14289-2, 8.2.1. [pdf.Writer.AutoTag] wires one.
//
//nolint:gochecknoglobals // static rule table
var structTreeRules = map[string][]string{
	familyA2:  {"6.7.3.3-t01"},
	familyUA1: {"7.1-t11"},
	familyUA2: {"8.2.1-t01"},
}

// markInfoRules require /MarkInfo with /Marked true: ISO 19005-2, 6.7.2.2 and
// ISO 14289-1 and -2, 6.2. AutoTag wires it with the structure tree, but it is
// a no-op on a document that already has one, so an input with a tree and no
// /MarkInfo keeps failing.
//
//nolint:gochecknoglobals // static rule table
var markInfoRules = map[string][]string{
	familyA2:  {"6.7.2.2-t01"},
	familyUA1: {"6.2-t01"},
	familyUA2: {"6.2-t01"},
}

// autoTagReachRules are rules a structure tree makes reachable, so an
// untagged input cannot fail them and its auto-tagged output can. AutoTag
// gives a figure an /Alt without a language, ISO 14289-1, 7.2 test 22, when
// the document declares none, which it documents as the caller's to set. It
// writes a PDF 1.7 structure tree, which ISO 14289-2, 8.2.5.2 test 2 rejects
// for want of a Document element in the PDF 2.0 namespace.
//
//nolint:gochecknoglobals // static rule table
var autoTagReachRules = map[string][]ruleMatch{
	familyUA1: {"7.2-t22"},
	familyUA2: {"8.2.5.2-t02"},
}

// signatureDigestRules require a signature's digest to cover the whole file,
// ISO 19005-2, 6.4.3 test 1. A full rewrite keeps an existing signature
// dictionary while moving every byte its /ByteRange names, so a signed input
// fails it once rewritten.
//
//nolint:gochecknoglobals // static rule table
var signatureDigestRules = map[string][]ruleMatch{
	familyA2: {"6.4.3-t01"},
}

// encryptionRules forbid the Encrypt key in the trailer, 6.1.3 test 2 in each
// part of ISO 19005. An encrypted PDF/A output has to fail it.
//
//nolint:gochecknoglobals // static rule table
var encryptionRules = map[string][]string{
	familyA1: {"6.1.3-t02"},
	familyA2: {"6.1.3-t02"},
	familyA4: {"6.1.3-t02"},
}

// metadataRules judge the document metadata: ISO 19005-1, 6.7, ISO 19005-2,
// 6.6, ISO 19005-4, 6.7 and its 6.1.3 tests 4 and 5 on the trailer /Info, and
// the metadata stream and identification schema of ISO 14289-1, 5 and 7.1
// test 8, and ISO 14289-2, 5 and 8.11.1. [pdf.Writer.Redact] clears the
// metadata by design, because /Info and the XMP packet are where copies of
// the removed text survive.
//
//nolint:gochecknoglobals // static rule table
var metadataRules = map[string][]ruleMatch{
	familyA1:  {"6.7."},
	familyA2:  {"6.6."},
	familyA4:  {"6.7.", "6.1.3-t04", "6.1.3-t05"},
	familyUA1: {"5-", "7.1-t08"},
	familyUA2: {"5-", "8.11.1-"},
}
