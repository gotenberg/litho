package css

// MatchMedia evaluates the media query list q against m (Media Queries 4,
// 3) as an @media rule's prelude evaluates: true when any query matches,
// an empty list matching all, and a query that fails to parse, a block, a
// string or a stray semicolon included, counting as "not all" (3.2). It is
// how a document evaluates the media attribute of an element or the media
// condition of a sizes attribute (HTML Standard, 2.3.10 and 4.8.4.2.2).
func MatchMedia(q string, m Media) bool {
	l, _ := parseComponentValues(q, maxTokens)
	return evalMediaList(l, m)
}
