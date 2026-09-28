package remediation

import (
	"context"
	"crypto/sha256"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/typography/cff"
	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
)

// Reasons a supplied program is embedded whole, which the fix description
// names.
const (
	wholeFSType     = "its OS/2 fsType forbids subsetting"
	wholeShared     = "its font descriptor serves a font given no subset"
	wholeFormFont   = "the interactive form's default resources name the font"
	wholeUnreadable = "the program could not be subset"
	wholeRejected   = "validation rejects the subset"
)

// queuedFont is a font [fontEmbedder.embed] planned, which
// [fontEmbedder.finish] subsets and writes.
type queuedFont struct {
	// order is where [fixFontEmbed] met the font, which its report keeps.
	order int
	// objNum is the font's object number in the document the run read, 0 for
	// a font written inline at site. writeTo is the number finish writes the
	// font at: objNum, or the new object an inline font moves to.
	objNum   int
	site     *inlineFontSite
	writeTo  int
	font     ast.PdfDict
	baseFont string
	fdNum    int
	// before is what validation found in the unembedded font, shown what the
	// document shows with it.
	before []compliance.Violation
	shown  []ast.PdfString
	plan   *embedPlan
	// whole identifies the supplied program, so the fonts given the same one
	// share its subset.
	whole programKey
}

// finish writes the fonts [fontEmbedder.embed] queued, each over a subset of
// its program or the whole program ([fontEmbedder.subsetQueued]), and returns
// what it did with each. A font written inline first moves to a new object,
// which its resource dictionary names in its place ([inlineFontSite.replace]),
// before any font is written: one whose resource dictionary cannot take the
// reference is refused ([ErrFontUnwritable]) and left as it is.
func (e *fontEmbedder) finish(ctx context.Context, uses fontUses) ([]fontOutcome, error) {
	var outcomes []fontOutcome
	placed := make([]*queuedFont, 0, len(e.queued))
	for _, q := range e.queued {
		q.writeTo = q.objNum
		if q.site != nil {
			// embed found the site writable, and nothing written since
			// touched a resource dictionary.
			q.writeTo = e.alloc.Alloc(ast.PdfNull{}).ObjectNum
			if !q.site.replace(ctx, e.alloc, e.doc, ast.PdfIndirectRef{ObjectNum: q.writeTo}) {
				outcomes = append(outcomes, fontOutcome{
					order: q.order, baseFont: q.baseFont,
					refused: fmt.Errorf("%w: the resource dictionary holding it could not be written", ErrFontUnwritable),
				})
				continue
			}
		}
		placed = append(placed, q)
	}
	e.queued = placed
	if err := e.subsetQueued(ctx, uses); err != nil {
		return nil, err
	}
	for _, q := range e.queued {
		e.write(ctx, q.writeTo, q.font, q.plan)
		outcomes = append(outcomes, fontOutcome{order: q.order, baseFont: q.baseFont, objNum: q.objNum, fix: q.describe()})
	}
	return outcomes, nil
}

// describe returns the fix description of the font as finish writes it: the
// program, subset or whole and why, and where the font is.
func (q *queuedFont) describe() string {
	plan := q.plan
	kind := "TrueType program as /FontFile2"
	if plan.program.format == outlinesCFF {
		kind = "CFF program as /FontFile3"
	}
	where := fmt.Sprintf("object %d", q.writeTo)
	if q.site != nil {
		where = fmt.Sprintf("written inline as /%s, now object %d", string(q.site.name), q.writeTo)
	}
	if plan.subsetGlyphs > 0 {
		return fmt.Sprintf("embedded %s (subset of %d glyphs, %s, %d bytes) for font %s (%s)",
			plan.program.psName, plan.subsetGlyphs, kind, len(plan.program.data), q.baseFont, where)
	}
	return fmt.Sprintf("embedded %s (whole %s, %d bytes, %s) for font %s (%s)",
		plan.program.psName, kind, len(plan.program.data), plan.whole, q.baseFont, where)
}

// subsetQueued gives the queued fonts subsets of their programs. The fonts
// given the same program share one subset, holding .notdef, the glyphs each
// of them shows and the components those draw, so the program is written
// once, as a whole program shared by several fonts is. A subset carries the
// subset tag ISO 32000-1, 9.6.4 gives a font whose program holds only part of
// its glyphs, and, at PDF/A-1, the /CharSet of a Type 1 subset and the
// /CIDSet of a CIDFont subset ISO 19005-1, 6.3.5 requires. Each font is
// judged over the subset as it was over the whole program, and one the rules
// reject over the subset keeps the whole program.
//
// A program stays whole when its OS/2 fsType sets the No subsetting bit
// (OpenType specification, OS/2 table), when the font's descriptor serves a
// font the document shows that does not share the subset, which would draw
// its own glyphs from the same program ([fontEmbedder.sharingWhole]), and
// when the interactive form's default resources name the font, since a reader
// filling a field draws characters no page shows (ISO 32000-1, 12.7.3.3).
//
// # Corpus evidence
//
//   - govdocs: the 785 files that show an unembedded font, remediated with
//     the Liberation 2.1.5 and URW base35 families at PDF/A-2b and PDF/A-1b:
//     4124 of the 4133 programs embedded are subsets, 7 stay whole for a
//     form's default resources and 2 for a descriptor shared with a font
//     given no subset. No rule gains a finding in any file, and the 680
//     outputs that embed a program fall from 806 to 432 MB at PDF/A-2b (817
//     to 444 MB at PDF/A-1b), none of them growing.
//   - litho: the 72 files that show an unembedded font, at PDF/A-2b: 65 of
//     the 68 programs embedded are subsets, 3 stay whole for a form's default
//     resources, no rule gains a finding, and the 54 outputs that embed a
//     program fall from 3.4 to 1.2 MB.
//   - govdocs thread_000_000187.pdf, thread_400_400566.pdf and
//     thread_000_000336.pdf: give one program to many fonts, and grew by up
//     to 718 KB when each font took a subset of its own, which is why the
//     fonts given one program share one subset.
//   - govdocs: 40 of those outputs judged by veraPDF 1.30.2 at PDF/A-2b and
//     PDF/A-1b, and 20 remediated at PDF/UA-1, fail every rule as often as
//     with the whole programs. pdf.js places the 15393 text items of the
//     first three pages of the 40 PDF/A-2b outputs where it placed them with
//     the whole programs, and pdf.js and poppler render those pages pixel for
//     pixel alike.
func (e *fontEmbedder) subsetQueued(ctx context.Context, uses fontUses) error {
	groups := make(map[programKey][]*queuedFont)
	var order []programKey
	for _, q := range e.queued {
		switch {
		case q.plan.program.noSubset:
			q.plan.whole = wholeFSType
		case e.formFonts(ctx)[q.objNum]:
			q.plan.whole = wholeFormFont
		default:
			if _, seen := groups[q.whole]; !seen {
				order = append(order, q.whole)
			}
			groups[q.whole] = append(groups[q.whole], q)
		}
	}
	for _, key := range order {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("subsetting fonts: %w", err)
		}
		members := e.sharingWhole(ctx, groups[key], uses)
		if len(members) == 0 {
			continue
		}
		var gids []uint16
		for _, q := range members {
			gids = append(gids, q.plan.gids...)
		}
		prog := members[0].plan.program
		sub, glyphs, err := subsetProgram(ctx, prog, gids)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return fmt.Errorf("subsetting fonts: %w", ctxErr)
			}
			e.doc.Logger.Debug("remediation: font-embed: cannot subset", "program", prog.psName, "error", err)
			for _, q := range members {
				q.plan.whole = wholeUnreadable
			}
			continue
		}
		for _, q := range members {
			subPlan, err := q.plan.rebuild(ctx, sub)
			if err == nil {
				e.markSubset(subPlan)
				err = e.judge(ctx, subPlan, q.before, q.shown)
			}
			if err != nil {
				e.doc.Logger.Debug("remediation: font-embed: subset rejected", "program", prog.psName, "font", q.baseFont, "error", err)
				q.plan.whole = wholeRejected
				continue
			}
			subPlan.subsetGlyphs = glyphs
			q.plan = subPlan
		}
	}
	return nil
}

// sharingWhole returns the members of a group of fonts given one program that
// may share its subset, and keeps the program whole for the others: a font
// whose descriptor also serves a font the group does not hold, which draws
// its own glyphs from the descriptor's program.
func (e *fontEmbedder) sharingWhole(ctx context.Context, members []*queuedFont, uses fontUses) []*queuedFont {
	inGroup := make(map[int]int)
	for _, q := range members {
		if q.fdNum != 0 {
			inGroup[q.fdNum]++
		}
	}
	users := e.descriptorUsers(ctx, uses)
	out := make([]*queuedFont, 0, len(members))
	for _, q := range members {
		if q.fdNum != 0 && users[q.fdNum] > inGroup[q.fdNum] {
			q.plan.whole = wholeShared
			continue
		}
		out = append(out, q)
	}
	return out
}

// subsetProgram returns prog cut down to .notdef, the glyphs of gids and what
// they draw, read as a supplied program, and its glyph count. The descriptor
// metrics stay those of the whole program, which an OpenType program with CFF
// outlines states in tables its bare CFF subset does not carry.
func subsetProgram(ctx context.Context, prog *suppliedProgram, gids []uint16) (*suppliedProgram, int, error) {
	var data []byte
	var newGID map[uint16]uint16
	var err error
	if prog.format == outlinesTrueType {
		data, newGID, err = truetype.Subset(ctx, prog.data, gids)
	} else {
		data, newGID, err = cff.Subset(ctx, prog.data, gids)
	}
	if err != nil {
		return nil, 0, fmt.Errorf("subsetting %s: %w", prog.psName, err)
	}
	sub, err := readSuppliedProgram(ctx, data, int64(len(data)))
	if err != nil {
		return nil, 0, fmt.Errorf("reading the subset of %s: %w", prog.psName, err)
	}
	sub.psName = prog.psName
	sub.bbox = prog.bbox
	sub.ascent, sub.descent, sub.capHeight = prog.ascent, prog.descent, prog.capHeight
	sub.italicAngle, sub.fixedPitch, sub.bold = prog.italicAngle, prog.fixedPitch, prog.bold
	return sub, len(newGID), nil
}

// markSubset tags the font's name as a subset's, and gives the descriptor of
// a PDF/A-1 subset the /CharSet or /CIDSet ISO 19005-1, 6.3.5 requires: a
// Type 1 font over a CFF program lists every glyph name the program holds but
// .notdef (ISO 32000-1, Table 122), and a CIDFont identifies every CID its
// /CIDToGIDMap maps, as validation reads the map ([compliance.CIDToGIDStreamEntries]).
func (e *fontEmbedder) markSubset(plan *embedPlan) {
	name, ok := plan.descriptor["FontName"].(ast.PdfName)
	if !ok {
		return
	}
	tag := subsetTag(plan.program.data)
	tagged := ast.PdfName(tag + "+" + string(name))
	plan.descriptor["FontName"] = tagged
	if plan.descendant == nil {
		plan.font["BaseFont"] = tagged
	} else {
		plan.descendant["BaseFont"] = tagged
		// A Type0 font's /BaseFont conventionally joins its descendant's name
		// and its CMap's (ISO 32000-1, 9.7.6.1), and carries the same tag.
		if base, ok := plan.font["BaseFont"].(ast.PdfName); ok {
			untagged := string(base)
			if old, rest, cut := strings.Cut(untagged, "+"); cut && isSubsetTag(old) {
				untagged = rest
			}
			if strings.HasPrefix(untagged, string(name)) {
				plan.font["BaseFont"] = ast.PdfName(tag + "+" + untagged)
			}
		}
	}
	if compliance.PDFAPart(e.profile) != 1 {
		return
	}
	if plan.descendant == nil && plan.program.format == outlinesCFF {
		plan.descriptor["CharSet"] = ast.PdfString(charSet(plan.program))
	}
	if plan.descendant != nil {
		plan.cidSet = cidSetOf(plan.cidToGID)
		plan.descriptor["CIDSet"] = ast.NewStream(ast.PdfDict{"Length": ast.PdfInt(len(plan.cidSet))}, plan.cidSet)
	}
}

// subsetTag returns the six uppercase letters ISO 32000-1, 9.6.4 prefixes a
// subset's name with, derived from the program so the same subset always
// takes the same tag and different subsets different ones.
func subsetTag(program []byte) string {
	sum := sha256.Sum256(program)
	var tag [6]byte
	for i := range tag {
		tag[i] = 'A' + sum[i]%26
	}
	return string(tag[:])
}

// charSet returns the /CharSet of prog: the name of every glyph but .notdef,
// in glyph order.
func charSet(prog *suppliedProgram) string {
	byGID := make(map[uint16]string, len(prog.names))
	for name, gid := range prog.names {
		byGID[gid] = name
	}
	var sb strings.Builder
	for _, gid := range slices.Sorted(maps.Keys(byGID)) {
		if name := byGID[gid]; name != ".notdef" {
			sb.WriteString("/" + name)
		}
	}
	return sb.String()
}

// cidSetOf returns the /CIDSet identifying CIDs 1 to the last a /CIDToGIDMap
// stream of cidToGID maps, one bit each, the high bit of the first byte for
// CID 0 (ISO 32000-1, Table 124).
func cidSetOf(cidToGID []byte) []byte {
	n := len(cidToGID) / 2
	out := make([]byte, (n+7)/8)
	for cid := 1; cid < n; cid++ {
		out[cid/8] |= 0x80 >> (cid % 8)
	}
	return out
}

// descriptorUsers returns how many of the fonts the document shows name each
// font descriptor, by object number, counted once per run before any font is
// rewritten.
func (e *fontEmbedder) descriptorUsers(ctx context.Context, uses fontUses) map[int]int {
	if e.fdUsers != nil {
		return e.fdUsers
	}
	e.fdUsers = make(map[int]int)
	for objNum := range uses.codes {
		font := resolveFromAllocOrDoc(ctx, e.alloc, e.doc, ast.PdfIndirectRef{ObjectNum: objNum})
		if font == nil {
			continue
		}
		if fd := e.descriptorNumber(ctx, font, nameOf(ctx, e.doc, font["Subtype"])); fd != 0 {
			e.fdUsers[fd]++
		}
	}
	// A font written inline may name a descriptor an indirect font names too,
	// and draws its glyphs from the same program.
	for _, use := range uses.inline {
		if fd := e.descriptorNumber(ctx, use.font, nameOf(ctx, e.doc, use.font["Subtype"])); fd != 0 {
			e.fdUsers[fd]++
		}
	}
	return e.fdUsers
}

// formFonts returns the object numbers of the fonts the interactive form's
// default resources name (ISO 32000-1, Table 218, /DR).
func (e *fontEmbedder) formFonts(ctx context.Context) map[int]bool {
	if e.drFonts != nil {
		return e.drFonts
	}
	e.drFonts = make(map[int]bool)
	ref, ok := catalogRefForFixer(ctx, e.doc)
	if !ok {
		return e.drFonts
	}
	catalog := resolveFromAllocOrDoc(ctx, e.alloc, e.doc, ref)
	form := liveDictValue(ctx, e.alloc, e.doc, catalog["AcroForm"])
	dr := liveDictValue(ctx, e.alloc, e.doc, form["DR"])
	for _, v := range liveDictValue(ctx, e.alloc, e.doc, dr["Font"]) {
		if r, ok := v.(ast.PdfIndirectRef); ok {
			e.drFonts[r.ObjectNum] = true
		}
	}
	return e.drFonts
}
