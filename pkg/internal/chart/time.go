package chart

import (
	"math"
	"time"

	"github.com/gotenberg/litho/pkg/internal/detmath"
	"github.com/gotenberg/litho/pkg/internal/numeric"
)

// timeAxis lays a date axis out as Office lays it out in time: one slot a
// day from the first date to the last, each category at its date's slot,
// major ticks every major unit from the first date (ECMA-376 Part 1,
// 21.2.2.39 dateAx).
//
// # Corpus evidence
//
//   - probe, xlsx: on 39 line charts over six dates spanning 10 to 1,100
//     days, Excel's axis holds one slot a day from the first date to the
//     last, each point at the centre of its date's slot (a point 2/1 of a
//     span drawn at 135.02 pt where the slots put it at 134.99), its major
//     tick marks on slot edges every major unit from the first date, the
//     last at the axis's end, and a label centred on each major date's
//     slot.
type timeAxis struct {
	// first is the serial day of the first slot, slots the days from the
	// first date to the last, both counted.
	first float64
	slots int
	// day is each category's day, counted from first; NaN for a category
	// with no date.
	day []float64
	// days and months are the major unit: a number of days, or of months
	// when months is positive.
	days, months int
	// fixedDays and fixedMonths are a major unit the file fixes, zero for
	// none.
	fixedDays, fixedMonths int
}

// maxTimeSlots bounds the days a date axis spans for it to be laid out in
// time. Anti-DoS guard, not spec-mandated: a wider span keeps the axis's
// categories evenly spaced; 65,536 days is 179 years, past any chart's
// span in the corpora.
const maxTimeSlots = 1 << 16

// maxTimeTicks bounds the major ticks of a date axis. Anti-DoS guard, not
// spec-mandated: a major unit the file fixes that gives more is ignored
// for the automatic one, which gives at most a few hundred.
const maxTimeTicks = 1024

// newTimeAxis returns the time layout of a date axis ax over the
// categories cats, nil when they are not all dates of whole days within
// maxTimeSlots of each other, or when ax counts in months or years.
func newTimeAxis(ax *Axis, cats Data) *timeAxis {
	if ax == nil || ax.Kind != DateAxis || (ax.BaseTimeUnit != TimeAuto && ax.BaseTimeUnit != TimeDays) || !cats.numeric() {
		return nil
	}
	t := &timeAxis{day: make([]float64, 0, min(cats.Len(), MaxPoints))}
	lo, hi := math.Inf(1), math.Inf(-1)
	for i := range min(cats.Len(), MaxPoints) {
		v, ok := cats.number(i)
		if !ok {
			t.day = append(t.day, math.NaN())
			continue
		}
		v = math.Floor(v)
		if v < 1 || v > maxSerialDay {
			return nil
		}
		t.day = append(t.day, v)
		lo, hi = math.Min(lo, v), math.Max(hi, v)
	}
	if math.IsInf(lo, 0) || hi-lo >= maxTimeSlots {
		return nil
	}
	t.first, t.slots = lo, int(hi-lo)+1 // fits int: under maxTimeSlots
	for i, v := range t.day {
		t.day[i] = v - lo
	}
	if u := ax.MajorUnit; numeric.Positive(u) && u == math.Floor(u) && u <= maxTimeSlots {
		switch ax.MajorTimeUnit {
		case TimeMonths:
			t.fixedMonths = int(u) // fits int: at most maxTimeSlots
		case TimeYears:
			t.fixedMonths = 12 * int(u) // fits int: at most 12 times maxTimeSlots
		case TimeAuto, TimeDays:
			t.fixedDays = int(u) // fits int: at most maxTimeSlots
		}
	}
	return t
}

// maxSerialDay is the last serial day a date axis reads, 9999-12-31, the
// last day a spreadsheet date holds.
const maxSerialDay = 2958465

// frac returns where category i lies along the axis, a fraction of its
// length: the centre of its date's slot when the categories lie between
// the ticks, the slot's start otherwise. A fractional i lies between the
// days of its neighbours.
func (t *timeAxis) frac(i float64, between bool) float64 {
	day := func(j int) float64 {
		if j < 0 || j >= len(t.day) {
			return math.NaN()
		}
		return t.day[j]
	}
	j := math.Floor(i)
	d := day(int(j)) // fits int: a category index under MaxPoints
	if k := i - j; k > 0 {
		if next := day(int(j) + 1); !math.IsNaN(next) && !math.IsNaN(d) { // fits int: a category index under MaxPoints
			d += float64(k * (next - d))
		}
	}
	if math.IsNaN(d) {
		d = 0
	}
	if between {
		return (d + 0.5) / float64(t.slots)
	}
	if t.slots <= 1 {
		return 0.5
	}
	return d / float64(t.slots-1)
}

// fit chooses the major unit for an axis length points long: the unit
// the file fixes when it gives at most maxTimeTicks ticks, else Office's
// automatic unit (officeTimeUnit).
func (t *timeAxis) fit(length float64) {
	t.days, t.months = t.fixedDays, t.fixedMonths
	if (t.days > 0 || t.months > 0) && len(t.majors()) < maxTimeTicks {
		return
	}
	t.days, t.months = officeTimeUnit(length / float64(t.slots))
}

// officeTimeUnit returns the major unit Office gives a date axis whose
// days are slot points apart: the days a label needs, officeTimePitch
// points over slot, rounded up to 1, 2 or 7 days, a month, two months or
// whole years, at the steps officeTimeSteps sets.
//
// # Corpus evidence
//
//   - probe, xlsx: on 39 line charts over spans of 10 to 1,100 days on
//     axes 148 to 479 pt long, Excel's unit is one day down to slots of
//     14.74 pt and two days from 10.79 pt, two days down to 7.70 pt and
//     seven from 7.05 pt, seven days down to 3.82 pt and a month from
//     3.68 pt, a month down to 0.62 pt and two from 0.42 pt, two months
//     down to 0.41 pt and a year from 0.29 pt: the unit is a function of
//     the slot alone, the days needed falling between 3.77 and 3.91 for
//     the step to a month, 23.2 and 34.6 for two months, 35.3 and 48.9
//     for a year at 14.4 pt a label.
func officeTimeUnit(slot float64) (days, months int) {
	if !numeric.Positive(slot) {
		return 1, 0
	}
	need := officeTimePitch / slot
	for _, s := range officeTimeSteps {
		if need <= s.upTo {
			return s.days, s.months
		}
	}
	return 0, 12 * int(math.Min(math.Ceil(need/365), maxTimeSlots/365)) // fits int: at most 179 years
}

// officeTimePitch is the least distance, in points, Office keeps between
// the major ticks of a date axis counted in days (officeTimeUnit): bound
// to (14.1, 14.74] by the probe's one- and two-day units.
const officeTimePitch = 14.4

// officeTimeSteps are Office's major units of a date axis by the days a
// label needs (officeTimeUnit), each taken up to upTo days; the bounds
// past two days lie inside the measured ranges.
var officeTimeSteps = [...]struct {
	upTo         float64
	days, months int
}{
	{1, 1, 0},
	{2, 2, 0},
	{3.8, 7, 0},
	{30, 0, 1},
	{45, 0, 2},
	{365, 0, 12},
}

// majors returns the days, counted from the first slot, of the major
// dates of the axis up to its end, the end included when one falls there,
// at most maxTimeTicks.
func (t *timeAxis) majors() []int {
	var out []int
	if t.months > 0 {
		base := serialDate(t.first)
		for k := 0; len(out) < maxTimeTicks; k++ {
			d := base.AddDate(0, k*t.months, 0)
			off := int(d.Sub(base).Hours()/24 + 0.5) // fits int: under maxTimeSlots days
			if off > t.slots {
				break
			}
			out = append(out, off)
		}
		return out
	}
	step := max(t.days, 1)
	for off := 0; off <= t.slots && len(out) < maxTimeTicks; off += step {
		out = append(out, off)
	}
	return out
}

// serialDate returns the date of a spreadsheet serial day.
func serialDate(serial float64) time.Time {
	return time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC).AddDate(0, 0, int(serial)) // fits int: at most maxSerialDay
}

// ticks returns the fractions along the axis of its major ticks, the
// start of each major date's slot, the axis's end among them when a major
// date falls there.
func (t *timeAxis) ticks(between bool) []float64 {
	n := float64(t.slots)
	if !between {
		n = float64(max(t.slots-1, 1))
	}
	ms := t.majors()
	out := make([]float64, 0, len(ms))
	for _, m := range ms {
		if f := float64(m) / n; f <= 1 {
			out = append(out, f)
		}
	}
	return out
}

// officeTimeTurns45 reports labels of a date axis, turned by 45 degrees,
// that Office keeps at 45 degrees: neighbours at least a line apart across
// the text. Closer ones it turns by 90 degrees.
//
// # Corpus evidence
//
//   - probe, xlsx: Excel turns the labels of a date axis by 90 degrees
//     when its major ticks lie 14.74 to 15.70 pt apart, 0.85 to 0.91 of
//     a 12.2 pt line across the text at 45 degrees, and by 45 degrees
//     when they lie 18.0 pt or more apart, 1.04 lines; a monthly axis
//     whose ticks lie 18.3 to 20.3 pt apart, 1.06 lines or more, turns
//     them by 90 degrees, which this rule does not explain.
func officeTimeTurns45(ls []tickLabel) bool {
	for i := 1; i < len(ls); i++ {
		a, b := ls[i-1], ls[i]
		r := a.b.style.Rotation * math.Pi / 180
		across := math.Abs(float64((b.cx-a.cx)*detmath.Sin(r)) + float64((b.cy-a.cy)*detmath.Cos(r)))
		line := a.b.h / float64(max(1, len(a.b.lines)))
		if across < line-0.01 {
			return false
		}
	}
	return true
}

// officeTimeCrowded reports unturned labels of a date axis that Office
// turns: neighbours closer than officeTimeGap ems of their font.
//
// # Corpus evidence
//
//   - probe, xlsx: Excel keeps the labels of a date axis level 6.15 pt
//     apart (10 pt labels 43.2 pt wide on ticks 49.35 pt apart) and turns
//     them by 45 degrees 2.5 pt apart (labels 38.1 pt wide on a February
//     40.6 pt long at the axis's level length).
func officeTimeCrowded(ls []tickLabel) bool {
	for i := 1; i < len(ls); i++ {
		a, b := ls[i-1], ls[i]
		if math.Abs(b.cx-a.cx)-float64((a.b.w+b.b.w)/2) < float64(officeTimeGap*a.b.style.Size) {
			return true
		}
	}
	return false
}

// officeTimeGap is the least distance, in ems of their font, between
// neighbouring labels of a date axis that Office keeps level: inside the
// measured bounds of 0.25 and 0.615.
const officeTimeGap = 0.5

// officeTimeSpills reports a label b of d that takes no room right of the
// plot area: a level label of a date axis Office lays out in time, which
// runs past the plot area's right edge, out of the chart if it must,
// where the plot area stays.
//
// # Corpus evidence
//
//   - probe, xlsx: a monthly axis 169.48 pt long over 61 days centres
//     its third label, 3/1/2024, 38.1 pt wide, 4.2 pt from the plot
//     area's end and keeps the plot area 11.0 pt from the chart's right
//     edge, the label running past the edge; a yearly axis over 1,100
//     days does the same with 1/1/2027.
func (rd *renderer) officeTimeSpills(d *dim, b *block) bool {
	return d.time != nil && !d.vertical && b.style.Rotation == 0 && rd.officeLayout()
}

// timeItems returns the labels of d, a date axis laid out in time: each
// major date inside the span, in the axis's format, centred on its slot
// when the categories lie between the ticks, at its start otherwise.
func (d *dim) timeItems() []item {
	t := d.time
	n, off := float64(t.slots), 0.5
	if !d.between {
		n, off = float64(max(t.slots-1, 1)), 0
	}
	var items []item
	for _, m := range t.majors() {
		if m >= t.slots {
			break
		}
		f := (float64(m) + off) / n
		if d.ax != nil && d.ax.Reverse {
			f = 1 - f
		}
		items = append(items, item{formatLabel(d.timeFormat, t.first+float64(m), d.loc), f})
	}
	return items
}

// officeTimeReserve returns how far right Office keeps room for the labels
// of d, a date axis laid out in time whose labels it turns by less than a
// quarter turn: a turned line's width, the line's height times the sine of the turn, right of
// the centre of the last slot, whether a label stands there or not; zero
// for any other axis. Its turned labels keep officePad from the chart's
// left edge, where a category axis's run into it (officeTurnedEdge).
//
// # Corpus evidence
//
//   - probe, xlsx: on nine line charts over dates whose labels Excel
//     turns by 45 degrees, slots 0.42 to 10.8 pt wide, the plot area ends
//     11.0 pt plus 2.08 to 8.39 pt from the chart's right edge, the
//     centre of the last slot 8.44 to 8.60 pt inside that room (7.48 pt
//     with slots of 10.8 pt), a 12.2 pt line turned being 8.63 pt wide;
//     and its left edge lies 18.34 pt further from the left edge than its
//     right from the right on all nine, the first label's turned box 6.5
//     pt from the edge to 0.2 pt on seven of them (5.54 and 6.29 pt with
//     slots of 10.8 and 10.2 pt).
func officeTimeReserve(d *dim) float64 {
	if d.time == nil || d.vertical || len(d.labels) == 0 {
		return 0
	}
	b := d.labels[0].b
	r := b.style.Rotation * math.Pi / 180
	if r == 0 || math.Abs(b.style.Rotation) >= 90 {
		return 0
	}
	n := float64(d.time.slots)
	if !d.between {
		n = float64(max(d.time.slots-1, 1))
	}
	last := float64(d.time.slots) - 0.5
	if !d.between {
		last = float64(d.time.slots - 1)
	}
	line := b.h / float64(max(1, len(b.lines)))
	return d.pixel(last/n) + float64(line*math.Abs(detmath.Sin(r)))
}
