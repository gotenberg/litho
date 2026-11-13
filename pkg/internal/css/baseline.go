package css

// BaselineSource is baseline-source (CSS Inline 3, 4.3): which line of an
// inline-level box with several gives its alignment baseline.
type BaselineSource uint8

// The baseline-source values. Auto takes the last baseline of an
// inline-block and the first of every other box.
const (
	BaselineAuto BaselineSource = iota
	BaselineFirst
	BaselineLast
)

// baselineProps returns baseline-source, not inherited.
//
// alignment-baseline, dominant-baseline and baseline-shift are not among
// them: CSS Inline 3, 4.4 to 4.6 applies them to inline boxes, which
// Chromium, whose departure the converter follows, leaves to SVG text:
// they are among [silentProperties]. vertical-align keeps the CSS 2.1
// grammar, not the shorthand of CSS Inline 3, 4.2, which Chromium does
// not parse.
//
// # Corpus evidence
//
//   - probe, html: Chromium's CSS.supports takes alignment-baseline:
//     central, dominant-baseline: hanging and baseline-shift: 10px, and
//     prints a span under each at the place of a plain one; it refuses
//     vertical-align: first, last middle, central and middle 10px.
//   - probe, html: an inline-block of two lines sits on its last line's
//     baseline under baseline-source: last and auto, on its first under
//     first.
func baselineProps() map[propID]propDef {
	return map[propID]propDef{
		pBaselineSource: enumProp("baseline-source", false, func(s *Style) *BaselineSource { return &s.BaselineSource }, map[string]BaselineSource{
			"auto": BaselineAuto, "first": BaselineFirst, "last": BaselineLast,
		}),
	}
}
