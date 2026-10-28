package image

import (
	"context"
	"log/slog"
	"maps"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/icc"
	"github.com/gotenberg/litho/pkg/internal/intconv"
)

// maxICCAlternateDepth bounds the colour space arrays a substitution of
// /Alternate spaces descends through: an ICCBased space whose /Alternate is
// another ICCBased space, or an Indexed, Separation or DeviceN space around
// one. Anti-DoS guard, not spec-mandated: an /Alternate naming its own space,
// directly or through others, would otherwise never end. Table 66 of ISO
// 32000-1 lets an alternate be any space but Pattern, so a chain is legal, and
// eight links leave room for any nesting a producer has reason to write.
const maxICCAlternateDepth = 8

// ICCAlternate returns the /Alternate of the ICCBased stream dictionary
// profile, whose /N states n components, when ISO 32000-1, 8.6.5.5, Table 66
// lets a reader use it in place of a profile it cannot use: a colour space
// other than Pattern, with n components. It also returns the alternate's
// family.
//
// It returns false when /Alternate is absent, and Table 66 then has the
// device space of n components stand in. It also returns false when the
// alternate names no colour space, names a Pattern space, or has another
// number of components, which Table 66 forbids. Poppler 26.09.0 then rejects
// the whole space and paints nothing; PDF.js ignores the entry with a warning
// and reads the device space of n components, which litho does too, so a
// malformed /Alternate costs no more than a missing one.
func ICCAlternate(ctx context.Context, doc *document.Document, profile ast.PdfDict, n int) (ast.PdfObject, string, bool) {
	altObj, ok := profile["Alternate"]
	if !ok {
		return nil, "", false
	}
	alt, err := document.ResolveObject(ctx, doc, altObj)
	if err != nil {
		doc.Logger.Debug("resolving ICCBased /Alternate", slog.Any("err", err))
		return nil, "", false
	}
	space, err := graphics.DescribeColorSpace(ctx, doc, alt)
	if err != nil || space.Family == graphics.FamilyPattern || space.Components != n {
		doc.Logger.Debug("ignoring an ICCBased /Alternate that is no colour space of /N components",
			slog.Int("n", n), slog.String("family", space.Family), slog.Int("components", space.Components), slog.Any("err", err))
		return nil, "", false
	}
	return alt, space.Family, true
}

// ICCBasedUsable reports whether litho reads an ICCBased colour space whose
// /N states n components through its profile stream profile for the intent:
// the profile reads within the size cap, parses, yields a transform, and
// describes n components (ISO 32000-1, 8.6.5.5, Table 66). Where it reports
// false, Table 66 has a reader use the space's /Alternate ([ICCAlternate]),
// or the device space of n components without one.
//
// It is the one test of a profile's usability in litho: the image decoder,
// the renderer's colours and blending spaces, recolouring and flattening all
// take a profile for unusable exactly where it reports false, so each reads
// the space in the same colours. The answer is cached per profile object.
func ICCBasedUsable(ctx context.Context, doc *document.Document, profile ast.PdfObject, n int, intent icc.Intent) bool {
	return profileTransform(ctx, doc, profile, n, intent) != nil
}

// SpaceRole is the position a colour space holds, which limits the families
// an /Alternate may substitute there.
type SpaceRole int

const (
	// RoleSpace is the colour space of an image or a colour itself, which
	// any family but Pattern may fill (ISO 32000-1, 8.6.5.5, Table 66).
	RoleSpace SpaceRole = iota
	// RoleIndexedBase is the base of an Indexed space, which shall not be
	// another Indexed or a Pattern space (8.6.6.3).
	RoleIndexedBase
	// RoleTintAlternate is the alternate of a Separation or DeviceN space,
	// which shall not be a special space (8.6.6.4, 8.6.6.5).
	RoleTintAlternate
)

// admits reports whether a space of the family may hold the position.
func (r SpaceRole) admits(family string) bool {
	switch r {
	case RoleSpace:
		return true
	case RoleIndexedBase:
		return family != graphics.FamilyIndexed
	case RoleTintAlternate:
		return family != graphics.FamilyIndexed && family != graphics.FamilySeparation && family != graphics.FamilyDeviceN
	}
	return false
}

// SubstituteICCAlternate returns the colour space cs stands for in role once
// the /Alternate of every ICCBased space whose profile litho cannot use for
// the intent ([ICCBasedUsable]) replaces it, in the positions
// [withICCAlternates] names, and whether anything was replaced. An alternate
// the role does not admit, or a chain past [maxICCAlternateDepth], leaves the
// space as it is, read in the device space of its /N. cs is not modified.
//
// The image decoder, the renderer, recolouring and flattening all substitute
// through it, so each reads an unusable profile through the same space.
func SubstituteICCAlternate(ctx context.Context, doc *document.Document, cs ast.PdfObject, intent icc.Intent, role SpaceRole) (ast.PdfObject, bool) {
	return substituteICCAlternate(ctx, doc, cs, intent, role, 0)
}

// withICCAlternates returns dict with its /ColorSpace replaced where an
// ICCBased space whose profile litho cannot use carries an /Alternate fit for
// its position ([ICCAlternate]): the space itself, the base of an Indexed
// space, or the alternate of a Separation or DeviceN space, for the
// rendering intent its colours are converted with. dict is not modified.
//
// ISO 32000-1, 8.6.5.5, Table 66 has a reader that cannot use the profile use
// /Alternate, and the device space of /N components only when it is absent.
// The device space is what every reading below does with an ICCBased space
// whose profile it cannot use, so only the alternate needs substituting. It
// takes the colour values as they are, with no conversion (Table 66), so the
// substituted space reads the samples and the colour components the ICCBased
// space was given. A profile is unusable when it cannot be parsed, has no
// transform litho builds, or describes another number of components than /N.
//
// A default colour space has already replaced a device space by then
// ([colorContext.withDefaults]), so an ICCBased default falls back too, while
// a device space an /Alternate names is not replaced again: 8.6.5.6 remaps the
// device spaces a content stream or an image selects, and Poppler parses an
// alternate with no resources either.
func withICCAlternates(ctx context.Context, doc *document.Document, dict ast.PdfDict, intent icc.Intent) ast.PdfDict {
	csObj, ok := dict["ColorSpace"]
	if !ok {
		return dict
	}
	sub, changed := substituteICCAlternate(ctx, doc, csObj, intent, RoleSpace, 0)
	if !changed {
		return dict
	}
	out := maps.Clone(dict)
	out["ColorSpace"] = sub
	return out
}

// substituteICCAlternate returns the space obj stands for in the role once
// the /Alternate of every ICCBased space litho cannot use, in the positions
// [withICCAlternates] names, replaces it, and whether anything was replaced.
func substituteICCAlternate(ctx context.Context, doc *document.Document, obj ast.PdfObject, intent icc.Intent, role SpaceRole, depth int) (ast.PdfObject, bool) {
	if depth > maxICCAlternateDepth {
		doc.Logger.Debug("ICCBased /Alternate substitution past its depth cap", slog.Int("depth", depth))
		return obj, false
	}
	resolved, err := document.ResolveObject(ctx, doc, obj)
	if err != nil {
		return obj, false
	}
	arr, ok := resolved.(ast.PdfArray)
	if !ok || len(arr) < 2 {
		return obj, false
	}
	family, err := document.ResolveName(ctx, doc, arr[0])
	if err != nil {
		return obj, false
	}
	switch family {
	case graphics.FamilyICCBased:
		alt, altFamily, ok := unusableICCAlternate(ctx, doc, arr, intent)
		if !ok || !role.admits(altFamily) {
			return obj, false
		}
		if sub, changed := substituteICCAlternate(ctx, doc, alt, intent, role, depth+1); changed {
			return sub, true
		}
		return alt, true
	case graphics.FamilyIndexed, graphics.FamilySeparation, graphics.FamilyDeviceN:
		if role != RoleSpace {
			return obj, false
		}
		pos, inner := 1, RoleIndexedBase
		if family != graphics.FamilyIndexed {
			pos, inner = 2, RoleTintAlternate
		}
		if pos >= len(arr) {
			return obj, false
		}
		sub, changed := substituteICCAlternate(ctx, doc, arr[pos], intent, inner, depth+1)
		if !changed {
			return obj, false
		}
		out := slices.Clone(arr)
		out[pos] = sub
		return out, true
	}
	return obj, false
}

// unusableICCAlternate returns the /Alternate ([ICCAlternate]) of the
// ICCBased space arr when litho cannot use its profile for the intent
// ([ICCBasedUsable]), and false when it can, or when the space states no /N
// of 1, 3 or 4 (Table 66), which leaves the readings that guess at it alone.
func unusableICCAlternate(ctx context.Context, doc *document.Document, arr ast.PdfArray, intent icc.Intent) (ast.PdfObject, string, bool) {
	stream, err := document.ResolveStream(ctx, doc, arr[1])
	if err != nil || stream.Dict == nil {
		return nil, "", false
	}
	n, err := document.ResolveInt(ctx, doc, stream.Dict["N"])
	if err != nil || (n != 1 && n != 3 && n != 4) {
		return nil, "", false
	}
	if ICCBasedUsable(ctx, doc, arr[1], intconv.Clamp(n), intent) {
		return nil, "", false
	}
	return ICCAlternate(ctx, doc, stream.Dict, intconv.Clamp(n))
}
