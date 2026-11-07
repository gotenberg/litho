package docx

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// This file holds the hooks that fill the layout contract fields this
// branch does not have yet: the layout cell's css.Style.LineRule,
// PageAnchor and the DecorationCloneAll box decoration break, and the
// offcrypto package's decryption. They are filled in round 2 once those
// fields land; until then each hook leaves the style as the rest of the
// converter built it, and every call site is in place.

// lineRule names a paragraph's line spacing rule for [setLineRule]: the
// ECMA-376 Part 1, 17.18.48 ST_LineSpacingRule values.
type lineRule uint8

// The line spacing rules.
const (
	ruleCSS      lineRule = iota // no rule: CSS half-leading
	ruleExact                    // exact: Writer's "exactly", baseline at 4/5 of the line
	ruleMultiple                 // auto: a multiple of the natural line height
)

// setLineRule records the line spacing rule of a paragraph's style, round
// 2: exact sets css.LineRuleExact with LineHeight in Points, auto sets
// css.LineRuleMultiple with LineHeight as the Number multiple (240ths of
// a line). Until then the converter places the lines with half-leading.
func setLineRule(_ *css.Style, _ lineRule) {}

// anchorToPage positions an absolutely positioned box against the page box
// of the page its anchor lands on, round 2: css.Style.PageAnchor, for
// drawings and frames positioned from the page. Until then they are
// positioned from their paragraph.
func anchorToPage(_ *css.Style) bool { return false }

// cloneDecorations draws a bordered paragraph's or cell's border and
// padding whole on every page it spans, as Writer does, round 2:
// css.DecorationCloneAll in BoxDecorationBreak. Until then they are
// sliced at the page cut.
func cloneDecorations(_ *css.Style) {}

// decrypt opens an encrypted package with the conversion's password, round
// 2: offcrypto.OpenOOXML(ctx, data, password), which returns a zip package
// unchanged. Until then a compound file fails in ooxml.Open with
// office.ErrEncrypted.
func decrypt(_ context.Context, data []byte, _ string) ([]byte, error) { return data, nil }
