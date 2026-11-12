package text

import "slices"

// wordFace is the vertical metrics, in em, Word lays the lines of a
// Microsoft face out with.
type wordFace struct {
	ascent, descent float64
}

// wordFaces maps the family key of a Microsoft face to the vertical metrics
// Word records for it, for the faces whose metric-compatible stand-in
// ([metricAliasGroups]) records others. A stand-in that records Word's own
// metrics has no entry: Carlito for Calibri, Liberation Sans for Arial,
// Liberation Serif for Times New Roman and Liberation Mono for Courier
// New.
//
// # Corpus evidence
//
//   - litho/docx paper-*: Word's PDFs record Cambria at an ascent of 0.950
//     em and a descent of 0.222 em, a line of 1.172 em; Caladea's metrics
//     make one of 1.15 em, and each Cambria heading sits 0.4 to 0.8 point
//     off Word's.
//   - poi/docx document/heading123 and document/58067: Word spaces single
//     spaced Cambria 12 point lines 14.0 to 14.25 points apart, 14.06 on
//     average, 1.172 em.
//   - libreoffice/docx, openxmlsdk/docx, poi/docx, litho/docx: Word spaces
//     single spaced Calibri 11 point lines as Carlito's 1.2207 em, Arial
//     10 point lines as Liberation Sans's 1.15 em with its line gap, Times
//     New Roman 12 point lines as Liberation Serif's 1.15 em, Courier New
//     10 point lines 1.125 to 1.15 em apart as Liberation Mono's 1.133.
var wordFaces = map[string]wordFace{
	familyKey("Cambria"): {ascent: 0.950, descent: 0.222},
}

// wordMetrics returns the vertical metrics Word lays a line of fc out with
// at size points when style asks for them ([Style.WordMetrics]) and fc
// stands in for the Microsoft face style names: the first family of the
// style that fc is, or is a metric alias of, decides. ok is false when fc
// is the named face itself or Word records the stand-in's own metrics.
func (fc *Face) wordMetrics(style Style, size float64) (m Metrics, ok bool) {
	if !style.WordMetrics || fc == nil {
		return Metrics{}, false
	}
	for _, f := range style.Families {
		key := familyKey(f)
		if slices.Contains(fc.keys, key) {
			return Metrics{}, false
		}
		aliases := metricAliases[key]
		if !slices.ContainsFunc(fc.keys, func(k string) bool { return slices.Contains(aliases, k) }) {
			continue
		}
		w, found := wordFaces[key]
		if !found {
			return Metrics{}, false
		}
		m = fc.Metrics(size)
		m.Ascent, m.Descent, m.LineGap = w.ascent*size, w.descent*size, 0
		return m, true
	}
	return Metrics{}, false
}

// MetricsFor returns the face's vertical metrics at the size of style, as
// [Face.Metrics] does, or, when style asks for Word's metrics
// ([Style.WordMetrics]) and the face stands in for a Microsoft face the
// style names, the ones Word lays that face's lines out with.
func (fc *Face) MetricsFor(style Style) Metrics {
	if m, ok := fc.wordMetrics(style, style.Size); ok {
		return m
	}
	return fc.Metrics(style.Size)
}
