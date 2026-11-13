package css

// RubyPosition is the ruby-position property (CSS Ruby 1, 4.1): the side
// of its base an annotation sits on.
type RubyPosition uint8

// The ruby-position values. Chromium parses over and under alone; it
// refuses alternate and inter-character, which CSS Ruby 1, 4.1 also
// defines, so litho refuses them too.
const (
	// RubyOver puts the annotation over its base, the initial value.
	RubyOver RubyPosition = iota
	// RubyUnder puts the annotation under its base.
	RubyUnder
)

// RubyAlign is the ruby-align property (CSS Ruby 1, 4.2): how the
// content of a ruby base or annotation fills the room its column gives
// it.
type RubyAlign uint8

// The ruby-align values.
const (
	// RubySpaceAround spreads the extra room over the content's
	// justification opportunities and half as much at each end, the
	// initial value.
	RubySpaceAround RubyAlign = iota
	// RubyStart puts the content at the start.
	RubyStart
	// RubyCenter centres the content.
	RubyCenter
	// RubySpaceBetween spreads the extra room over the content's
	// justification opportunities alone.
	RubySpaceBetween
)

// RubyOverhang is the ruby-overhang property (CSS Ruby 1, 4.3, in the
// editor's draft): whether an annotation wider than its base may hang
// over the text beside it.
type RubyOverhang uint8

// The ruby-overhang values Chromium parses. The draft's spaces value is
// refused, as Chromium refuses it.
const (
	// RubyOverhangAuto lets an annotation hang over the text beside its
	// ruby, the initial value.
	RubyOverhangAuto RubyOverhang = iota
	// RubyOverhangNone keeps an annotation inside its ruby.
	RubyOverhangNone
)

// rubyProps returns the properties of CSS Ruby 1: ruby-position,
// ruby-align and ruby-overhang, all inherited (4.1 to 4.3).
func rubyProps() map[propID]propDef {
	return map[propID]propDef{
		pRubyPosition: enumProp("ruby-position", true, func(s *Style) *RubyPosition { return &s.RubyPosition }, map[string]RubyPosition{
			"over": RubyOver, "under": RubyUnder,
		}),
		pRubyAlign: enumProp("ruby-align", true, func(s *Style) *RubyAlign { return &s.RubyAlign }, map[string]RubyAlign{
			"space-around": RubySpaceAround, "start": RubyStart, "center": RubyCenter, "space-between": RubySpaceBetween,
		}),
		pRubyOverhang: enumProp("ruby-overhang", true, func(s *Style) *RubyOverhang { return &s.RubyOverhang }, map[string]RubyOverhang{
			"auto": RubyOverhangAuto, "none": RubyOverhangNone,
		}),
	}
}
