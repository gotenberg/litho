package chart

import (
	"math"
)

// maxTicks caps the major ticks of an axis. Anti-DoS guard, not
// spec-mandated: LibreOffice allows 500 for a fixed interval, and a tick
// costs a label and a gridline.
const maxTicks = 500

// maxMinorTicks caps the minor ticks of an axis. Anti-DoS guard, not
// spec-mandated: five minor ticks per major one at the major cap.
const maxMinorTicks = maxTicks * 5

// maxMinorSteps caps the additions that walk the minor ticks. Anti-DoS
// guard, not spec-mandated: a rounded addition that moves a tick moves it
// by at least half a step, so a walk over at most maxMinorTicks steps
// ends within twice as many; the cap allows four times.
const maxMinorSteps = 4 * maxMinorTicks

// maxAutoTicks is the most major intervals the automatic scale chooses,
// LibreOffice's for a value axis.
const maxAutoTicks = 10

// scale is a resolved value axis scale.
type scale struct {
	min, max     float64
	major, minor float64
	// log is the logarithm base of a logarithmic scale, 0 for linear.
	log     float64
	reverse bool
}

// scaleOptions are the automatic scaling rules of a dimension, as
// LibreOffice's ScaleAutomatism applies them.
type scaleOptions struct {
	// zero extends a range of positive (or negative) values to zero when
	// its minimum is under 5/6 of its maximum, and else by half its span
	// toward zero.
	zero bool
	// border adds an interval when a value lies within 1/21 of the range
	// of the axis end.
	border bool
	// maxCount is the most major intervals, from 2 to 10.
	maxCount int
}

// autoScale returns the scale of an axis over the values [lo, hi], with
// the axis's fixed bounds and intervals, as LibreOffice's ScaleAutomatism
// computes it (calculateExplicitIncrementAndScaleForLinear).
func autoScale(ax *Axis, lo, hi float64, opt scaleOptions) scale {
	if math.IsNaN(lo) || math.IsNaN(hi) || math.IsInf(lo, 0) || math.IsInf(hi, 0) {
		lo, hi = 0, 1
	}
	fixMin, fixMax, fixMajor := math.NaN(), math.NaN(), math.NaN()
	sc := scale{minor: math.NaN()}
	if ax != nil {
		fixMin, fixMax, fixMajor = finite(ax.Min), finite(ax.Max), finite(ax.MajorUnit)
		sc.minor = finite(ax.MinorUnit)
		sc.reverse = ax.Reverse
		if ax.Log > 1 && ax.Log < 1e6 {
			sc.log = ax.Log
		}
	}
	if fixMajor <= 0 {
		fixMajor = math.NaN()
	}
	if sc.log > 0 {
		return logScale(sc, lo, hi, fixMin, fixMax, fixMajor)
	}
	autoMin, autoMax := math.IsNaN(fixMin), math.IsNaN(fixMax)
	if !autoMin {
		lo = fixMin
	}
	if !autoMax {
		hi = fixMax
	}
	if opt.maxCount < 2 {
		opt.maxCount = 2
	}
	opt.maxCount = min(opt.maxCount, maxAutoTicks)
	if lo > hi {
		if autoMax || !autoMin {
			hi = lo
		} else {
			lo = hi
		}
	}
	srcMin, srcMax := lo, hi
	swap := srcMin < 0 && srcMax <= 0
	if swap {
		srcMin, srcMax = -srcMax, -srcMin
		autoMin, autoMax = autoMax, autoMin
	}
	tMin, tMax := srcMin, srcMax
	if autoMin && tMin > 0 {
		if tMin == tMax || tMin/tMax < 5.0/6.0 {
			if opt.zero {
				tMin = 0
			}
		} else if opt.zero {
			tMin -= (tMax - tMin) / 2
		}
	}
	if tMin == tMax {
		switch {
		case autoMax || !autoMin:
			if tMax == 0 {
				tMax = 1
			} else {
				tMax *= 2
			}
		case tMin == 0:
			tMin = -1
		default:
			tMin /= 2
		}
	}
	base := 0.0
	switch {
	case !autoMin:
		base = tMin
	case !autoMax:
		base = tMax
	}
	autoDist := math.IsNaN(fixMajor)
	maxCount := opt.maxCount
	if !autoDist {
		maxCount = maxTicks
	}
	dist := fixMajor
	norm, mag := 0.0, 0.0
	haveNorm := false
	var aMin, aMax float64
	for range 64 {
		if autoDist {
			if !haveNorm {
				d := (tMax - tMin) / float64(maxCount)
				switch {
				case d <= 1e-307:
					norm, mag = 1, 1e-307
				case math.IsInf(d, 0):
					norm, mag = 1, math.MaxFloat64
				default:
					e := math.Floor(math.Log10(d) + 1e-12)
					mag = math.Pow(10, e)
					norm = d / mag
					switch {
					case norm <= 1:
						norm = 1
					case norm <= 2:
						norm = 2
					case norm <= 5:
						norm = 5
					default:
						norm, mag = 1, mag*10
					}
				}
				haveNorm = true
			} else {
				switch norm {
				case 1:
					norm = 2
				case 2:
					norm = 5
				default:
					norm, mag = 1, mag*10
				}
			}
			dist = norm * mag
		}
		aMin, aMax = tMin, tMax
		if autoMin {
			aMin = floorAt(aMin, dist, base)
			if opt.border && aMin != 0 && (aMax-srcMin)/(aMax-aMin) > 20.0/21.0 {
				aMin -= dist
			}
		}
		if autoMax {
			aMax = ceilAt(aMax, dist, base)
			if opt.border && aMax != 0 && (srcMax-aMin)/(aMax-aMin) > 20.0/21.0 {
				aMax += dist
			}
		}
		count := math.Floor((aMax-aMin)/dist + 1e-9)
		if count <= float64(maxCount) {
			break
		}
		autoDist = true
		maxCount = opt.maxCount
	}
	if swap {
		aMin, aMax = -aMax, -aMin
	}
	sc.min, sc.max, sc.major = aMin, aMax, dist
	if !(sc.max > sc.min) {
		sc.max = sc.min + 1
	}
	if !(sc.major > 0) || (sc.max-sc.min)/sc.major > maxTicks {
		sc.major = (sc.max - sc.min) / maxAutoTicks
	}
	return sc
}

// logScale returns the logarithmic scale of [lo, hi]: the decades the
// values span, ticks at the powers of the base.
func logScale(sc scale, lo, hi, fixMin, fixMax, fixMajor float64) scale {
	b := sc.log
	lg := func(v float64) float64 { return math.Log(v) / math.Log(b) }
	if lo <= 0 {
		lo = 1
	}
	if hi <= lo {
		hi = lo * b
	}
	lmin, lmax := math.Floor(lg(lo)+1e-9), math.Ceil(lg(hi)-1e-9)
	if lmax <= lmin {
		lmax = lmin + 1
	}
	if fixMin > 0 {
		lmin = lg(fixMin)
	}
	if fixMax > 0 {
		lmax = lg(fixMax)
	}
	if lmax <= lmin {
		lmax = lmin + 1
	}
	step := math.Max(1, math.Ceil((lmax-lmin)/maxAutoTicks))
	if fixMajor > 1 {
		step = math.Max(lg(fixMajor), 1e-3)
	}
	sc.min, sc.max, sc.major = math.Pow(b, lmin), math.Pow(b, lmax), step
	return sc
}

// finite returns v, NaN for an infinite v.
func finite(v float64) float64 {
	if math.IsInf(v, 0) {
		return math.NaN()
	}
	return v
}

// floorAt rounds v down to a multiple of dist from base.
func floorAt(v, dist, base float64) float64 {
	q := (v - base) / dist
	f := math.Floor(q)
	if q-f > 1-1e-9 {
		f++
	}
	return base + f*dist
}

// ceilAt rounds v up to a multiple of dist from base.
func ceilAt(v, dist, base float64) float64 {
	q := (v - base) / dist
	c := math.Ceil(q)
	if c-q > 1-1e-9 {
		c--
	}
	return base + c*dist
}

// frac returns where v lies along the scale, 0 at its start and 1 at its
// end, reversed when the axis runs backwards.
func (s scale) frac(v float64) float64 {
	var f float64
	if s.log > 0 {
		if v <= 0 {
			f = 0
		} else {
			l := math.Log(s.log)
			f = (math.Log(v)/l - math.Log(s.min)/l) / (math.Log(s.max)/l - math.Log(s.min)/l)
		}
	} else {
		f = (v - s.min) / (s.max - s.min)
	}
	if s.reverse {
		f = 1 - f
	}
	return f
}

// ticks returns the major tick values from the minimum to the maximum.
func (s scale) ticks() []float64 {
	var out []float64
	if s.log > 0 {
		lmin := math.Log(s.min) / math.Log(s.log)
		lmax := math.Log(s.max) / math.Log(s.log)
		for e := lmin; e <= lmax+1e-9 && len(out) <= maxTicks; e += s.major {
			out = append(out, math.Pow(s.log, e))
		}
		return out
	}
	start := ceilAt(s.min, s.major, 0)
	if math.Abs(start-s.min) > 1e-9*math.Max(1, math.Abs(s.major)) && start-s.major >= s.min-1e-9*s.major {
		start -= s.major
	}
	for i := 0; i <= maxTicks; i++ {
		v := start + float64(i)*s.major
		if v > s.max+1e-9*s.major {
			break
		}
		if math.Abs(v) < 1e-12*s.major {
			v = 0
		}
		out = append(out, v)
	}
	return out
}

// minorTicks returns the minor tick values, those between the major ones.
func (s scale) minorTicks() []float64 {
	if s.log > 0 {
		return nil
	}
	step := s.minor
	if !(step > 0) {
		step = s.major / 5
	}
	if (s.max-s.min)/step > maxMinorTicks {
		return nil
	}
	var out []float64
	// The ticks accumulate the step, which rounds differently from
	// start + i*step. A step under half the spacing of the floats near v
	// leaves v where it is, and the walk stops there.
	v := ceilAt(s.min, step, 0)
	for i := 0; i < maxMinorSteps && v <= s.max+1e-9*step; i++ {
		r := math.Abs(math.Remainder(v, s.major))
		if r > 1e-9*s.major {
			out = append(out, v)
		}
		next := v + step
		if next == v {
			break
		}
		v = next
	}
	return out
}

// clamp returns v within the scale's range.
func (s scale) clamp(v float64) float64 {
	return math.Max(s.min, math.Min(s.max, v))
}
