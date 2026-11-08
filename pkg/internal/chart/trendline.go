package chart

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/compose"
)

// trendSamples is how many points a curved trendline is drawn through.
const trendSamples = 100

// maxPolynomialOrder caps a polynomial trendline's degree: Office and
// LibreOffice offer 2 to 6.
const maxPolynomialOrder = 6

// trendPoints returns the points a trendline t fits through: the series'
// valid x and y values in order.
func trendPoints(xs, ys []float64) ([]float64, []float64) {
	var fx, fy []float64
	for i := range min(len(xs), len(ys)) {
		if math.IsNaN(xs[i]) || math.IsNaN(ys[i]) || math.IsInf(xs[i], 0) || math.IsInf(ys[i], 0) {
			continue
		}
		fx, fy = append(fx, xs[i]), append(fy, ys[i])
	}
	return fx, fy
}

// trendCurve returns the points of trendline t fitted to xs and ys, in
// data units, nil when the fit is undefined: LibreOffice's regression
// curve calculators (chart2's LinearRegressionCurveCalculator and its
// siblings) by least squares, a moving average by its windows.
func trendCurve(t Trendline, xs, ys []float64) [][2]float64 {
	xs, ys = trendPoints(xs, ys)
	if len(xs) < 2 {
		return nil
	}
	if t.Kind == TrendMovingAverage {
		return movingAverage(t, xs, ys)
	}
	f := fitTrend(t, xs, ys)
	if f == nil {
		return nil
	}
	lo, hi := xs[0], xs[0]
	for _, x := range xs {
		lo, hi = math.Min(lo, x), math.Max(hi, x)
	}
	if t.Backward > 0 && !math.IsInf(t.Backward, 0) {
		lo -= t.Backward
	}
	if t.Forward > 0 && !math.IsInf(t.Forward, 0) {
		hi += t.Forward
	}
	if t.Kind == TrendLogarithmic || t.Kind == TrendPower {
		lo = math.Max(lo, hi/1e6)
		if !(hi > 0) {
			return nil
		}
	}
	n := trendSamples
	if t.Kind == TrendLinear {
		n = 2
	}
	var out [][2]float64
	for i := range n {
		x := lo + (hi-lo)*float64(i)/float64(n-1)
		if y := f(x); !math.IsNaN(y) && !math.IsInf(y, 0) {
			out = append(out, [2]float64{x, y})
		}
	}
	return out
}

// fitTrend returns the curve of a least-squares fit of t's kind to xs and
// ys, nil when the data cannot define one.
func fitTrend(t Trendline, xs, ys []float64) func(float64) float64 {
	switch t.Kind {
	case TrendLinear:
		c := polyFit(xs, ys, 1, t.HasIntercept, t.Intercept)
		if c == nil {
			return nil
		}
		return func(x float64) float64 { return c[0] + c[1]*x }
	case TrendPolynomial:
		order := min(max(t.Order, 2), maxPolynomialOrder)
		c := polyFit(xs, ys, order, t.HasIntercept, t.Intercept)
		if c == nil {
			return nil
		}
		return func(x float64) float64 {
			y := 0.0
			for i := len(c) - 1; i >= 0; i-- {
				y = y*x + c[i]
			}
			return y
		}
	case TrendExponential:
		// ln y = ln a + b x over the positive values.
		var px, py []float64
		for i := range xs {
			if ys[i] > 0 {
				px, py = append(px, xs[i]), append(py, math.Log(ys[i]))
			}
		}
		c := polyFit(px, py, 1, t.HasIntercept && t.Intercept > 0, math.Log(math.Max(t.Intercept, math.SmallestNonzeroFloat64)))
		if c == nil {
			return nil
		}
		return func(x float64) float64 { return math.Exp(c[0] + c[1]*x) }
	case TrendLogarithmic:
		var px, py []float64
		for i := range xs {
			if xs[i] > 0 {
				px, py = append(px, math.Log(xs[i])), append(py, ys[i])
			}
		}
		c := polyFit(px, py, 1, false, 0)
		if c == nil {
			return nil
		}
		return func(x float64) float64 { return c[0] + c[1]*math.Log(x) }
	case TrendPower:
		var px, py []float64
		for i := range xs {
			if xs[i] > 0 && ys[i] > 0 {
				px, py = append(px, math.Log(xs[i])), append(py, math.Log(ys[i]))
			}
		}
		c := polyFit(px, py, 1, false, 0)
		if c == nil {
			return nil
		}
		return func(x float64) float64 { return math.Exp(c[0]) * math.Pow(x, c[1]) }
	case TrendMovingAverage:
	}
	return nil
}

// polyFit returns the coefficients c[0] + c[1] x + ... of the
// least-squares polynomial of degree order through xs and ys, its
// constant forced to c0 when forced, nil when the points do not determine
// it.
func polyFit(xs, ys []float64, order int, forced bool, c0 float64) []float64 {
	first := 0
	if forced {
		first = 1
	}
	k := order + 1 - first
	if len(xs) < k || k < 1 {
		return nil
	}
	// The normal equations: sum over the points of x^(i+j) for the
	// unknown powers first..order.
	a := make([][]float64, k)
	for i := range a {
		a[i] = make([]float64, k+1)
	}
	for p := range xs {
		y := ys[p]
		if forced {
			y -= c0
		}
		for i := range k {
			xi := math.Pow(xs[p], float64(i+first))
			for j := range k {
				a[i][j] += xi * math.Pow(xs[p], float64(j+first))
			}
			a[i][k] += xi * y
		}
	}
	sol, ok := solve(a)
	if !ok {
		return nil
	}
	out := make([]float64, order+1)
	if forced {
		out[0] = c0
	}
	copy(out[first:], sol)
	return out
}

// solve solves the augmented linear system a by Gaussian elimination with
// partial pivoting; false for a singular one.
func solve(a [][]float64) ([]float64, bool) {
	n := len(a)
	for c := range n {
		p := c
		for r := c + 1; r < n; r++ {
			if math.Abs(a[r][c]) > math.Abs(a[p][c]) {
				p = r
			}
		}
		if math.Abs(a[p][c]) < 1e-300 {
			return nil, false
		}
		a[c], a[p] = a[p], a[c]
		for r := c + 1; r < n; r++ {
			f := a[r][c] / a[c][c]
			for j := c; j <= n; j++ {
				a[r][j] -= f * a[c][j]
			}
		}
	}
	x := make([]float64, n)
	for r := n - 1; r >= 0; r-- {
		s := a[r][n]
		for j := r + 1; j < n; j++ {
			s -= a[r][j] * x[j]
		}
		x[r] = s / a[r][r]
		if math.IsNaN(x[r]) || math.IsInf(x[r], 0) {
			return nil, false
		}
	}
	return x, true
}

// movingAverage returns the points of a moving average of xs and ys, as
// chart2's MovingAverageRegressionCurveCalculator places them: each
// window of Period points ending at a point, at that point's x (prior),
// at the window's average x (averaged abscissa), or with the x values
// shifted back by half the period (central).
func movingAverage(t Trendline, xs, ys []float64) [][2]float64 {
	p := max(t.Period, 2)
	if p > len(xs) {
		return nil
	}
	var avgX, avgY []float64
	for i := p - 1; i < len(xs); i++ {
		sx, sy := 0.0, 0.0
		for j := range p {
			sx += xs[i-j]
			sy += ys[i-j]
		}
		avgX, avgY = append(avgX, sx/float64(p)), append(avgY, sy/float64(p))
	}
	var px []float64
	switch t.Moving {
	case MovingAveragedAbscissa:
		px = avgX
	case MovingCentral:
		for i := p / 2; i < len(xs)-1; i++ {
			px = append(px, xs[i])
		}
	case MovingPrior:
		px = xs[p-1:]
	}
	out := make([][2]float64, 0, len(avgY))
	for i := range min(len(px), len(avgY)) {
		out = append(out, [2]float64{px[i], avgY[i]})
	}
	return out
}

// trendlines draws the trendlines of g's series against x and y, clipped
// to the plot area.
func (rd *renderer) trendlines(g *Group, x, y *dim) {
	xy := g.Kind == Scatter || g.Kind == Bubble
	var paths []compose.Fragment
	for _, s := range g.Series {
		if len(s.Trendlines) == 0 {
			continue
		}
		vals, cats := rd.data(s.Values), rd.data(s.Categories)
		n := min(vals.Len(), MaxPoints)
		xs, ys := make([]float64, n), make([]float64, n)
		for j := range n {
			xs[j], ys[j] = math.NaN(), math.NaN()
			if v, ok := vals.number(j); ok {
				ys[j] = v
			}
			xs[j] = float64(j + 1)
			if xy {
				if xv, ok := xValue(cats, j); ok {
					xs[j] = xv
				} else {
					xs[j] = math.NaN()
				}
			}
		}
		for _, t := range s.Trendlines {
			st := rd.trendStroke(g, s, t).compose()
			if st == nil {
				continue
			}
			var pts []compose.Point
			for _, p := range trendCurve(t, xs, ys) {
				if xy {
					pts = append(pts, compose.Point{X: x.valuePixel(p[0]), Y: y.valuePixel(p[1])})
				} else {
					pts = append(pts, point(x, y, p[0]-1, p[1]))
				}
			}
			if len(pts) > 1 {
				st.Join = compose.RoundJoin
				paths = append(paths, &compose.Path{Ops: polyline(pts, false), Stroke: st})
			}
		}
	}
	if len(paths) > 0 {
		rd.out = append(rd.out, &compose.Group{Clip: rectPath(x.inner), Content: paths})
	}
}

// trendStroke returns the line of trendline t of series s: its own, or a
// line in the series' colour.
func (rd *renderer) trendStroke(g *Group, s *Series, t Trendline) *Stroke {
	if t.Line != nil {
		return t.Line
	}
	c := rd.fillOf(g, s, -1).Color
	if b := rd.borderOf(g, s, -1); lined(g.Kind) && b != nil && !b.None {
		c = b.Color
	}
	return &Stroke{Color: c, Width: 0.75}
}

// trendName returns the legend text of trendline t of series s: its name,
// or its kind's and the series' (chart2's
// RegressionCurveHelper::getUINameForRegressionCurve).
func trendName(t Trendline, s *Series) string {
	if t.Name != "" {
		return t.Name
	}
	kind := "Linear"
	switch t.Kind {
	case TrendExponential:
		kind = "Exponential"
	case TrendLogarithmic:
		kind = "Logarithmic"
	case TrendPolynomial:
		kind = "Polynomial"
	case TrendPower:
		kind = "Power"
	case TrendMovingAverage:
		kind = "Moving average"
	case TrendLinear:
	}
	return kind + " (" + s.Name + ")"
}
