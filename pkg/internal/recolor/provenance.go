package recolor

import (
	"bytes"
	"slices"
	"sync"

	"github.com/gotenberg/litho/pkg/internal/graphics/icc"
)

// Where a profile a conversion read came from, as [Report.DeviceProfiles]
// and [IntentFallback.Profile] name it.
const (
	// ProfileGiven is the profile [Options] give the device space.
	ProfileGiven = "given"
	// ProfileTarget is the target's profile, which a device space of the
	// target's own family means when [Options] give it none.
	ProfileTarget = "target"
	// ProfileBuiltin is litho's built-in profile of the device space.
	ProfileBuiltin = "builtin"
	// ProfileBuiltinFallback is the built-in profile standing in for the one
	// [Options] give, which the engine builds no transform from.
	ProfileBuiltinFallback = "builtin-fallback"
	// ProfileDefaultSpace is a Default colour space of the resources (ISO
	// 32000-1, 8.6.5.6) read in place of the device space.
	ProfileDefaultSpace = "default-space"
	// ProfileDocument is a profile the document embeds, an ICCBased space's.
	ProfileDocument = "document"
)

// IntentFallback is a profile a conversion read through another table than
// the one for its rendering intent.
type IntentFallback struct {
	// Target is true for the target's profile, false for a source's.
	Target bool
	// Space is the profile's data colour space signature (ICC.1:2010, 7.2.6),
	// such as [SpaceCMYK].
	Space string
	// Profile says where the profile came from: [ProfileGiven],
	// [ProfileBuiltin], [ProfileTarget] or [ProfileDocument].
	Profile string
	// Table is the table it was read through.
	Table icc.Fallback
}

// fallbackReporter is a [Converter] that knows which tables it read, as
// [icc.Link] does.
type fallbackReporter interface {
	Fallbacks() (src, dst icc.Fallback)
}

// notingPlan returns p with a converter that records, on its first
// conversion, that the device space was converted through a profile of the
// given provenance. A plan that converts nothing, the target's own space
// among them, is returned as it is: the report names what was converted, and
// the initial DeviceGray of every content stream is planned whether or not
// anything paints in it.
func (s *session) notingPlan(p *spacePlan, space, provenance string) *spacePlan {
	if p.kind != planConvert {
		return p
	}
	cp := *p
	cp.conv = &notingConverter{next: p.conv, note: func() { s.noteDeviceProfile(space, provenance) }}
	return &cp
}

// notingConverter runs note once, before its first conversion.
type notingConverter struct {
	next Converter
	once sync.Once
	note func()
}

func (c *notingConverter) Convert(in, out []float64) {
	c.once.Do(c.note)
	c.next.Convert(in, out)
}

// noteDeviceProfile records that the walk converted the device space through
// a profile of the given provenance, once per provenance.
func (s *session) noteDeviceProfile(space, provenance string) {
	s.noteMu.Lock()
	defer s.noteMu.Unlock()
	if s.report.DeviceProfiles == nil {
		s.report.DeviceProfiles = make(map[string][]string)
	}
	list := s.report.DeviceProfiles[space]
	if i, found := slices.BinarySearch(list, provenance); !found {
		s.report.DeviceProfiles[space] = slices.Insert(list, i, provenance)
	}
}

// noteFallbacks records the tables conv read in place of the intent's, for
// the transform from end to the target. It runs once per transform, and the
// target's fallback is recorded once per session.
func (s *session) noteFallbacks(end Endpoint, conv Converter) {
	var r fallbackReporter
	switch c := conv.(type) {
	case fallbackReporter:
		r = c
	case xyzLink:
		r = c.link
	default:
		return
	}
	src, dst := r.Fallbacks()
	if src != icc.NoFallback {
		s.report.IntentFallbacks = append(s.report.IntentFallbacks,
			IntentFallback{Space: end.Space, Profile: s.provenance(end), Table: src})
	}
	if dst != icc.NoFallback && !s.targetFellBack {
		s.targetFellBack = true
		s.report.IntentFallbacks = append(s.report.IntentFallbacks,
			IntentFallback{Target: true, Space: s.t.end.Space, Profile: ProfileTarget, Table: dst})
	}
}

// provenance says where a source endpoint's profile came from.
func (s *session) provenance(end Endpoint) string {
	var given []byte
	switch end.Space {
	case SpaceGray:
		given = s.opts.DeviceGray
	case SpaceRGB:
		given = s.opts.DeviceRGB
	case SpaceCMYK:
		given = s.opts.DeviceCMYK
	}
	switch {
	case given != nil && bytes.Equal(end.Profile, given):
		return ProfileGiven
	case bytes.Equal(end.Profile, s.t.end.Profile):
		return ProfileTarget
	case end.Profile != nil && bytes.Equal(end.Profile, defaultProfile(end.Space, nil)):
		return ProfileBuiltin
	}
	return ProfileDocument
}
