package sheet

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// maxFormulaLen caps the characters of a formula evaluated. Anti-DoS
// guard, not spec-mandated: Excel stops at 8,192 characters.
const maxFormulaLen = 8192

// maxFormulaDepth caps the nesting of a formula's operations and of the
// formulas its references reach. Anti-DoS guard, not spec-mandated: Calc
// stops a recursion of formula cells long before, and a written formula
// nests a few levels.
const maxFormulaDepth = 64

// maxFormulaCells caps the cells the formulas of one conversion read
// through their ranges. Anti-DoS guard, not spec-mandated: ten million
// reads take well under a second.
const maxFormulaCells = 10_000_000

// The error values a formula evaluates to, as Calc shows them.
const (
	errDiv0  = "#DIV/0!"
	errValue = "#VALUE!"
	errRef   = "#REF!"
)

// fval is a value during a formula's evaluation: a Value, a range, or the
// number format a percent literal gives the result.
type fval struct {
	v Value
	// rng is set for a reference to more than one cell.
	rng *frange
	// percent marks a result a percent literal took part in, which Calc
	// shows in a percent format.
	percent bool
	// format is the number format of the first cell a sum or a lone
	// reference read, which Calc's result takes when its cell has none.
	format string
}

// frange is a rectangle of cells of a sheet.
type frange struct {
	sheet int
	rg    Range
}

// formulaEval evaluates the formulas of a document's cells.
type formulaEval struct {
	doc   *Document
	grids []*grid
	// state is 1 for a cell being evaluated, 2 for one done; done holds
	// the results.
	state map[[3]int]uint8
	done  map[[3]int]fval
	ok    map[[3]int]bool
	reads int
	// unsupported records the functions met that are not evaluated.
	unsupported map[string]bool
}

// newFormulaEval returns an evaluator over doc, which the formulas of its
// cells and of its conditional formats read.
func newFormulaEval(doc *Document) *formulaEval {
	e := &formulaEval{doc: doc, grids: make([]*grid, len(doc.Sheets)), state: map[[3]int]uint8{}, done: map[[3]int]fval{}, ok: map[[3]int]bool{}, unsupported: map[string]bool{}}
	for i, sh := range doc.Sheets {
		if sh != nil {
			e.grids[i] = newPlainGrid(doc, sh)
		}
	}
	return e
}

// cellValue returns the value of the cell (r, c) of sheet s as a formula
// reads it: its formula's result when it has one that evaluates, else the
// value the file holds.
func (e *formulaEval) cellValue(s, r, c, depth int) fval {
	if s < 0 || s >= len(e.grids) || e.grids[s] == nil || r < 0 || c < 0 || r >= maxRows || c >= maxColumns {
		return fval{v: Value{Kind: Error, Text: errRef}}
	}
	g := e.grids[s]
	cell := g.cell(r, c)
	if cell == nil {
		return fval{}
	}
	if cell.Formula != "" && depth < maxFormulaDepth {
		k := [3]int{s, r, c}
		switch e.state[k] {
		case 2:
			return e.done[k]
		case 0:
			e.state[k] = 1
			res, ok := e.eval(cell.Formula, s, r, c, 0, 0, depth+1)
			e.state[k] = 2
			if ok && res.rng == nil {
				e.done[k] = res
				e.ok[k] = true
				return res
			}
			e.done[k] = e.stored(g, r, c, cell)
			return e.done[k]
		}
	}
	return e.stored(g, r, c, cell)
}

// stored returns the value the file holds for a cell.
func (e *formulaEval) stored(g *grid, r, c int, cell *Cell) fval {
	v := cell.Value
	if v.Kind == Text && v.Text == "" && len(cell.Runs) > 0 {
		var sb strings.Builder
		for _, run := range cell.Runs {
			sb.WriteString(run.Text)
		}
		v.Text = sb.String()
	}
	return fval{v: v, format: g.baseStyle(r, c, cell).Format}
}

// evalAt evaluates the formula f as the cell (r, c) of sheet s reads it,
// its relative references moved dr rows and dc columns.
func (e *formulaEval) evalAt(f string, s, r, c, dr, dc int) (fval, bool) {
	return e.eval(f, s, r, c, dr, dc, 0)
}

// eval evaluates the formula f of the cell (r, c) of sheet s, its
// relative references moved dr rows and dc columns, false when it holds
// what the evaluator does not compute: a function it does not know, a
// reference it cannot resolve, a syntax it does not read.
func (e *formulaEval) eval(f string, s, r, c, dr, dc, depth int) (fval, bool) {
	if len(f) > maxFormulaLen {
		return fval{}, false
	}
	f = strings.TrimSpace(f)
	for _, p := range []string{"of:", "msoxl:", "oooc:"} {
		f = strings.TrimPrefix(f, p)
	}
	f = strings.TrimPrefix(f, "=")
	p := &fparser{e: e, src: f, sheet: s, row: r, col: c, dr: dr, dc: dc, depth: depth}
	v, ok := p.comparison()
	p.space()
	if !ok || p.pos != len(p.src) {
		return fval{}, false
	}
	return v, true
}

// fparser reads and evaluates one formula as it goes.
type fparser struct {
	e        *formulaEval
	src      string
	pos      int
	sheet    int
	row, col int
	dr, dc   int
	depth    int
}

// space skips white space.
func (p *fparser) space() {
	for p.pos < len(p.src) && (p.src[p.pos] == ' ' || p.src[p.pos] == '\n' || p.src[p.pos] == '\r' || p.src[p.pos] == '\t') {
		p.pos++
	}
}

// eat consumes s when the input continues with it.
func (p *fparser) eat(s string) bool {
	p.space()
	if strings.HasPrefix(p.src[p.pos:], s) {
		p.pos += len(s)
		return true
	}
	return false
}

// comparison reads a = <> < > <= >= comparison, the loosest binding
// operators.
func (p *fparser) comparison() (fval, bool) {
	a, ok := p.concat()
	if !ok {
		return a, false
	}
	for _, op := range []string{"<>", "<=", ">=", "=", "<", ">"} {
		if p.eat(op) {
			b, ok := p.concat()
			if !ok || a.rng != nil || b.rng != nil {
				// A range compared reads as an array, which the
				// evaluator does not compute.
				return b, false
			}
			return compareValues(op, scalar(a), scalar(b)), true
		}
	}
	return a, true
}

// concat reads & concatenations.
func (p *fparser) concat() (fval, bool) {
	a, ok := p.additive()
	if !ok {
		return a, false
	}
	for p.eat("&") {
		b, ok := p.additive()
		if !ok || a.rng != nil || b.rng != nil {
			return b, false
		}
		x, y := scalar(a), scalar(b)
		if x.Kind == Error {
			a = fval{v: x}
			continue
		}
		if y.Kind == Error {
			a = fval{v: y}
			continue
		}
		a = fval{v: Value{Kind: Text, Text: textOfValue(x) + textOfValue(y)}}
	}
	return a, true
}

// additive reads + and - operations.
func (p *fparser) additive() (fval, bool) {
	a, ok := p.multiplicative()
	if !ok {
		return a, false
	}
	for {
		var op byte
		switch {
		case p.eat("+"):
			op = '+'
		case p.eat("-"):
			op = '-'
		default:
			return a, true
		}
		b, ok := p.multiplicative()
		if !ok || a.rng != nil || b.rng != nil {
			return b, false
		}
		a = arith(op, a, b)
	}
}

// multiplicative reads * and / operations.
func (p *fparser) multiplicative() (fval, bool) {
	a, ok := p.power()
	if !ok {
		return a, false
	}
	for {
		var op byte
		switch {
		case p.eat("*"):
			op = '*'
		case p.eat("/"):
			op = '/'
		default:
			return a, true
		}
		b, ok := p.power()
		if !ok || a.rng != nil || b.rng != nil {
			return b, false
		}
		a = arith(op, a, b)
	}
}

// power reads ^ operations, left to right as Calc evaluates them.
func (p *fparser) power() (fval, bool) {
	a, ok := p.unary()
	if !ok {
		return a, false
	}
	for p.eat("^") {
		b, ok := p.unary()
		if !ok || a.rng != nil || b.rng != nil {
			return b, false
		}
		a = arith('^', a, b)
	}
	return a, true
}

// unary reads a sign, then a postfix percent.
func (p *fparser) unary() (fval, bool) {
	switch {
	case p.eat("-"):
		v, ok := p.unary()
		if !ok || v.rng != nil {
			return v, false
		}
		return arith('-', fval{v: Value{Kind: Number}}, v), true
	case p.eat("+"):
		return p.unary()
	}
	v, ok := p.primary()
	if !ok {
		return v, false
	}
	for p.eat("%") {
		if v.rng != nil {
			return v, false
		}
		v = arith('/', v, fval{v: Value{Kind: Number, Number: 100}})
		v.percent = true
	}
	return v, true
}

// primary reads a number, a string, a parenthesised expression, a
// function call, a boolean or a reference.
func (p *fparser) primary() (fval, bool) {
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > maxFormulaDepth {
		return fval{}, false
	}
	p.space()
	if p.pos >= len(p.src) {
		return fval{}, false
	}
	c := p.src[p.pos]
	switch {
	case c == '(':
		p.pos++
		v, ok := p.comparison()
		if !ok || !p.eat(")") {
			return v, false
		}
		return v, true
	case c == '"':
		return p.str()
	case c >= '0' && c <= '9' || c == '.' && p.pos+1 < len(p.src) && p.src[p.pos+1] >= '0' && p.src[p.pos+1] <= '9':
		return p.number()
	case c == '[':
		return p.odfRef()
	case c == '#':
		for _, code := range []string{"#DIV/0!", "#VALUE!", "#REF!", "#NAME?", "#NUM!", "#N/A", "#NULL!"} {
			if strings.HasPrefix(p.src[p.pos:], code) {
				p.pos += len(code)
				return fval{v: Value{Kind: Error, Text: code}}, true
			}
		}
		return fval{}, false
	}
	return p.name()
}

// str reads a string literal, a doubled quote standing for one.
func (p *fparser) str() (fval, bool) {
	var sb strings.Builder
	p.pos++
	for p.pos < len(p.src) {
		if p.src[p.pos] == '"' {
			if p.pos+1 < len(p.src) && p.src[p.pos+1] == '"' {
				sb.WriteByte('"')
				p.pos += 2
				continue
			}
			p.pos++
			return fval{v: Value{Kind: Text, Text: sb.String()}}, true
		}
		sb.WriteByte(p.src[p.pos])
		p.pos++
	}
	return fval{}, false
}

// number reads a number literal.
func (p *fparser) number() (fval, bool) {
	start := p.pos
	for p.pos < len(p.src) && (p.src[p.pos] >= '0' && p.src[p.pos] <= '9' || p.src[p.pos] == '.') {
		p.pos++
	}
	if p.pos < len(p.src) && (p.src[p.pos] == 'E' || p.src[p.pos] == 'e') {
		q := p.pos + 1
		if q < len(p.src) && (p.src[q] == '+' || p.src[q] == '-') {
			q++
		}
		if q < len(p.src) && p.src[q] >= '0' && p.src[q] <= '9' {
			for q < len(p.src) && p.src[q] >= '0' && p.src[q] <= '9' {
				q++
			}
			p.pos = q
		}
	}
	n, err := strconv.ParseFloat(p.src[start:p.pos], 64)
	if err != nil {
		return fval{}, false
	}
	return fval{v: Value{Kind: Number, Number: n}}, true
}

// name reads a function call, TRUE or FALSE, or an A1 reference with its
// optional sheet ("Sheet2!A1", "'My sheet'!$A$1:B3").
func (p *fparser) name() (fval, bool) {
	start := p.pos
	sheet := p.sheet
	if p.src[p.pos] == '\'' {
		end := strings.Index(p.src[p.pos+1:], "'!")
		for end >= 0 && strings.HasPrefix(p.src[p.pos+1+end:], "''") {
			next := strings.Index(p.src[p.pos+1+end+2:], "'!")
			if next < 0 {
				end = -1
				break
			}
			end += 2 + next
		}
		if end < 0 {
			return fval{}, false
		}
		name := strings.ReplaceAll(p.src[p.pos+1:p.pos+1+end], "''", "'")
		p.pos += end + 3
		if external(name) {
			return fval{}, false
		}
		if sheet = p.e.sheetIndex(name); sheet < 0 {
			return fval{v: Value{Kind: Error, Text: errRef}}, true
		}
	}
	idStart := p.pos
	for p.pos < len(p.src) {
		r, size := utf8.DecodeRuneInString(p.src[p.pos:])
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '.' && r != '$' {
			break
		}
		p.pos += size
	}
	word := p.src[idStart:p.pos]
	if word == "" {
		return fval{}, false
	}
	if p.pos < len(p.src) && p.src[p.pos] == '!' && sheet == p.sheet && start == idStart {
		if sheet = p.e.sheetIndex(word); sheet < 0 {
			return fval{v: Value{Kind: Error, Text: errRef}}, true
		}
		p.pos++
		return p.a1Ref(sheet)
	}
	if p.eat("(") {
		// OpenFormula names Excel's later functions in a namespace
		// (ODF 1.3 Part 4, 4.15: COM.MICROSOFT.SWITCH).
		return p.call(strings.TrimPrefix(strings.ToUpper(word), "COM.MICROSOFT."))
	}
	switch strings.ToUpper(word) {
	case "TRUE":
		return fval{v: Value{Kind: Bool, Number: 1}}, true
	case "FALSE":
		return fval{v: Value{Kind: Bool}}, true
	}
	p.pos = idStart
	return p.a1Ref(sheet)
}

// external reports a quoted sheet name that names another workbook's
// sheet ("[1]Sheet1", "C:\dir\[book.xlsx]Sheet1"): the formula grammar
// brackets the workbook of an external reference (ECMA-376 Part 1,
// 18.17.2, book-prefix). Such a formula keeps its stored result: a
// computation reads nothing outside the file.
func external(name string) bool {
	return strings.ContainsAny(name, "[]")
}

// sheetIndex returns the index of the sheet named name, -1 for none.
func (e *formulaEval) sheetIndex(name string) int {
	for i, sh := range e.doc.Sheets {
		if sh != nil && strings.EqualFold(sh.Name, name) {
			return i
		}
	}
	return -1
}

// a1Ref reads an A1 cell or range reference on sheet s.
func (p *fparser) a1Ref(s int) (fval, bool) {
	r0, c0, ok := p.cellRef()
	if !ok {
		return fval{}, false
	}
	r1, c1 := r0, c0
	if p.pos < len(p.src) && p.src[p.pos] == ':' {
		p.pos++
		if r1, c1, ok = p.cellRef(); !ok {
			return fval{}, false
		}
	}
	return p.e.reference(s, r0, c0, r1, c1, p.depth), true
}

// cellRef reads one A1 cell reference, its relative parts moved by the
// parser's offset.
func (p *fparser) cellRef() (row, col int, ok bool) {
	absC := p.pos < len(p.src) && p.src[p.pos] == '$'
	if absC {
		p.pos++
	}
	col = 0
	n := 0
	for p.pos < len(p.src) && n < 4 {
		ch := p.src[p.pos] | 0x20
		if ch < 'a' || ch > 'z' {
			break
		}
		col = col*26 + int(ch-'a') + 1
		p.pos++
		n++
	}
	if n == 0 {
		return 0, 0, false
	}
	absR := p.pos < len(p.src) && p.src[p.pos] == '$'
	if absR {
		p.pos++
	}
	row, n = 0, 0
	for p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' && n < 8 {
		row = row*10 + int(p.src[p.pos]-'0')
		p.pos++
		n++
	}
	if n == 0 || row == 0 {
		return 0, 0, false
	}
	row, col = row-1, col-1
	if !absR {
		row += p.dr
	}
	if !absC {
		col += p.dc
	}
	return row, col, true
}

// odfRef reads an OpenFormula reference ("[.A1]", "[.A1:.B2]",
// "[$Sheet2.A1]", "['My sheet'.A1]"; ODF 1.3 Part 4, 5.8).
func (p *fparser) odfRef() (fval, bool) {
	end := strings.IndexByte(p.src[p.pos:], ']')
	if end < 0 {
		return fval{}, false
	}
	body := p.src[p.pos+1 : p.pos+end]
	p.pos += end + 1
	first, second, two := strings.Cut(body, ":")
	part := func(s string, sheet int) (_, row, col int, ok bool) {
		dot := strings.LastIndexByte(s, '.')
		if dot < 0 {
			return 0, 0, 0, false
		}
		if strings.Contains(s[:dot], "'#") {
			// Another document's sheet (ODF 1.3 Part 4, 5.8: an IRI in
			// quotes, '#', then the sheet), which the formula keeps
			// its stored result for.
			return 0, 0, 0, false
		}
		if name := strings.TrimPrefix(s[:dot], "$"); name != "" {
			name = strings.Trim(name, "'")
			if sheet = p.e.sheetIndex(name); sheet < 0 {
				return -1, 0, 0, true
			}
		}
		q := &fparser{e: p.e, src: s[dot+1:], dr: p.dr, dc: p.dc}
		row, col, ok = q.cellRef()
		return sheet, row, col, ok && q.pos == len(q.src)
	}
	s0, r0, c0, ok := part(first, p.sheet)
	if !ok {
		return fval{}, false
	}
	if s0 < 0 {
		return fval{v: Value{Kind: Error, Text: errRef}}, true
	}
	r1, c1 := r0, c0
	if two {
		// A second address that names no sheet is on the first one's (ODF
		// 1.3 Part 4, 5.8).
		s1, rr, cc, ok := part(second, s0)
		if !ok || s1 != s0 {
			return fval{}, false
		}
		r1, c1 = rr, cc
	}
	return p.e.reference(s0, r0, c0, r1, c1, p.depth), true
}

// reference returns the value of a reference: the cell's for one cell,
// else the range.
func (e *formulaEval) reference(s, r0, c0, r1, c1, depth int) fval {
	if r0 < 0 || c0 < 0 || r1 < 0 || c1 < 0 {
		return fval{v: Value{Kind: Error, Text: errRef}}
	}
	if r0 == r1 && c0 == c1 {
		return e.cellValue(s, r0, c0, depth)
	}
	return fval{rng: &frange{sheet: s, rg: Range{FirstRow: min(r0, r1), FirstColumn: min(c0, c1), LastRow: max(r0, r1), LastColumn: max(c0, c1)}}}
}

// call reads the arguments of the function fn, separated by commas or
// OpenFormula's semicolons, and computes it: SUM, AVERAGE, MIN, MAX and
// COUNT over numbers, ranges and booleans, as Calc does, text in a range
// skipped, an error the result.
func (p *fparser) call(fn string) (fval, bool) {
	var args []fval
	if !p.eat(")") {
		for {
			v, ok := p.comparison()
			if !ok {
				return v, false
			}
			args = append(args, v)
			if p.eat(")") {
				break
			}
			if !p.eat(",") && !p.eat(";") {
				return fval{}, false
			}
		}
	}
	switch fn {
	case "SUM", "AVERAGE", "MIN", "MAX", "COUNT":
	default:
		if v, ok := p.scalarFunction(fn, args); ok {
			return v, true
		}
		p.e.unsupported[fn] = true
		return fval{}, false
	}
	var nums []float64
	format := ""
	for _, a := range args {
		if a.rng != nil {
			var errV *Value
			p.e.eachCell(a.rng, p.depth, func(v fval) bool {
				switch v.v.Kind {
				case Number, Bool:
					nums = append(nums, v.v.Number)
					if format == "" {
						format = v.format
					}
				case Error:
					if fn != "COUNT" {
						e := v.v
						errV = &e
						return false
					}
				case Text, Empty:
				}
				return true
			})
			if errV != nil {
				return fval{v: *errV}, true
			}
			continue
		}
		v := a.v
		switch v.Kind {
		case Number, Bool:
			nums = append(nums, v.Number)
			if format == "" {
				format = a.format
			}
		case Error:
			if fn != "COUNT" {
				return fval{v: v}, true
			}
		case Text:
			if fn == "COUNT" {
				continue
			}
			n, err := strconv.ParseFloat(strings.TrimSpace(v.Text), 64)
			if err != nil {
				return fval{v: Value{Kind: Error, Text: errValue}}, true
			}
			nums = append(nums, n)
		case Empty:
		}
	}
	out := fval{v: Value{Kind: Number}, format: format}
	switch fn {
	case "SUM":
		for _, n := range nums {
			out.v.Number += n
		}
	case "AVERAGE":
		if len(nums) == 0 {
			return fval{v: Value{Kind: Error, Text: errDiv0}}, true
		}
		for _, n := range nums {
			out.v.Number += n
		}
		out.v.Number /= float64(len(nums))
	case "MIN", "MAX":
		for i, n := range nums {
			if i == 0 || (fn == "MIN") == (n < out.v.Number) {
				out.v.Number = n
			}
		}
	case "COUNT":
		out.v.Number, out.format = float64(len(nums)), ""
	}
	return out, true
}

// eachCell calls fn with the value of each cell of rg holding something,
// row by row, until fn returns false or the read cap is reached.
func (e *formulaEval) eachCell(rg *frange, depth int, fn func(fval) bool) {
	if rg.sheet < 0 || rg.sheet >= len(e.grids) || e.grids[rg.sheet] == nil {
		fn(fval{v: Value{Kind: Error, Text: errRef}})
		return
	}
	g := e.grids[rg.sheet]
	for _, r := range g.rowOrder {
		if r < rg.rg.FirstRow || r > rg.rg.LastRow {
			continue
		}
		for _, c := range g.rows[r].Cells {
			if c.Column < rg.rg.FirstColumn || c.Column > rg.rg.LastColumn {
				continue
			}
			if e.reads++; e.reads > maxFormulaCells {
				return
			}
			if !fn(e.cellValue(rg.sheet, r, c.Column, depth)) {
				return
			}
		}
	}
}

// scalar returns the value of v, a range's first cell standing for it as
// Calc's implicit intersection would not: an error.
func scalar(v fval) Value {
	if v.rng != nil {
		return Value{Kind: Error, Text: errValue}
	}
	return v.v
}

// number converts a value for arithmetic: a number or a boolean as is, the
// empty value as 0, a text holding a number as that number, any other text
// as #VALUE!.
func number(v Value) (float64, *Value) {
	switch v.Kind {
	case Number, Bool:
		return v.Number, nil
	case Empty:
		return 0, nil
	case Text:
		if n, err := strconv.ParseFloat(strings.TrimSpace(v.Text), 64); err == nil {
			return n, nil
		}
		return 0, &Value{Kind: Error, Text: errValue}
	case Error:
	}
	return 0, &v
}

// arith applies the arithmetic operator op to a and b. The result keeps
// a percent literal's mark and, for a sum or a difference, the first
// operand's number format.
func arith(op byte, a, b fval) fval {
	x, errA := number(scalar(a))
	if errA != nil {
		return fval{v: *errA}
	}
	y, errB := number(scalar(b))
	if errB != nil {
		return fval{v: *errB}
	}
	out := fval{percent: a.percent || b.percent}
	if op == '+' || op == '-' {
		out.format = a.format
		if out.format == "" {
			out.format = b.format
		}
	}
	var r float64
	switch op {
	case '+':
		r = x + y
	case '-':
		r = x - y
	case '*':
		r = x * y
	case '/':
		if y == 0 {
			return fval{v: Value{Kind: Error, Text: errDiv0}}
		}
		r = x / y
	case '^':
		r = math.Pow(x, y)
	}
	if !finite(r) {
		return fval{v: Value{Kind: Error, Text: "#NUM!"}}
	}
	out.v = Value{Kind: Number, Number: r}
	return out
}

// compareValues compares a and b by op as Calc does: numbers by value,
// texts case-insensitively, a number before a text before a boolean, the
// empty value as 0 or "".
func compareValues(op string, a, b Value) fval {
	if a.Kind == Error {
		return fval{v: a}
	}
	if b.Kind == Error {
		return fval{v: b}
	}
	rank := func(v Value) int {
		switch v.Kind {
		case Text:
			return 1
		case Bool:
			return 2
		case Number, Empty, Error:
		}
		return 0
	}
	if a.Kind == Empty && b.Kind == Text {
		a = Value{Kind: Text}
	}
	if b.Kind == Empty && a.Kind == Text {
		b = Value{Kind: Text}
	}
	cmp := rank(a) - rank(b)
	if cmp == 0 {
		if rank(a) == 1 {
			cmp = strings.Compare(strings.ToLower(a.Text), strings.ToLower(b.Text))
		} else {
			switch {
			case a.Number < b.Number:
				cmp = -1
			case a.Number > b.Number:
				cmp = 1
			}
		}
	}
	var r bool
	switch op {
	case "=":
		r = cmp == 0
	case "<>":
		r = cmp != 0
	case "<":
		r = cmp < 0
	case ">":
		r = cmp > 0
	case "<=":
		r = cmp <= 0
	case ">=":
		r = cmp >= 0
	}
	n := 0.0
	if r {
		n = 1
	}
	return fval{v: Value{Kind: Bool, Number: n}}
}

// textOfValue returns the text a value concatenates as: a number in
// Calc's standard format, a boolean as TRUE or FALSE.
func textOfValue(v Value) string {
	switch v.Kind {
	case Number:
		return strconv.FormatFloat(v.Number, 'g', 15, 64)
	case Bool:
		if v.Number != 0 {
			return "TRUE"
		}
		return "FALSE"
	case Text, Error, Empty:
	}
	return v.Text
}

// percentFormat is the format Calc gives a formula's result that a percent
// literal took part in.
//
// # Corpus evidence
//
//   - probe, xlsx: in LibreOffice 26.8.2.2, a workbook cell =-A1+10%
//     without a result, A1 holding 2, prints -190.00%.
const percentFormat = "0.00%"

// applyFormulas returns doc with the cells whose formula Calc computes as
// it loads the file holding the results computed here: the cells the
// evaluator does not compute keep their stored value, with a warning
// naming the functions it left out. A result in a General cell takes the
// format a percent literal or its first reference gives it. doc is not
// changed.
//
// # Corpus evidence
//
//   - probe, xlsx: in LibreOffice 26.8.2.2, a workbook's uncached =A1+2,
//     =SUM(A1:A3), =AVERAGE(A1:A3), =MAX(A1:A3)-MIN(A1:A3), =COUNT(A1:A3),
//     =A1/0, =A1>1, ="x"&A1 print 4, 9, 3, 2, 3, #DIV/0!, TRUE and x2
//     over 2, 3, 4, and a cached =A1+2 saved as 5 prints 5.
//   - probe, ods: in LibreOffice 26.8.2.2, a cell =[.A1]+2 saved as 5
//     prints 4, whatever program wrote the file.
func (b *builder) applyFormulas(doc *Document) *Document {
	any := false
	for _, sh := range doc.Sheets {
		if sh == nil {
			continue
		}
		for _, r := range sh.Rows {
			for _, c := range r.Cells {
				any = any || c.Formula != ""
			}
		}
	}
	if !any {
		return doc
	}
	e := newFormulaEval(doc)
	out := *doc
	out.Sheets = append([]*Sheet(nil), doc.Sheets...)
	left := 0
	for s, sh := range doc.Sheets {
		if sh == nil {
			continue
		}
		var rows []Row
		for ri, row := range sh.Rows {
			changed := false
			for ci := range row.Cells {
				c := &row.Cells[ci]
				if c.Formula == "" {
					continue
				}
				res := e.cellValue(s, row.Index, c.Column, 0)
				if !e.ok[[3]int{s, row.Index, c.Column}] {
					left++
					continue
				}
				if !changed {
					if rows == nil {
						rows = append([]Row(nil), sh.Rows...)
					}
					rows[ri].Cells = append([]Cell(nil), row.Cells...)
					changed = true
				}
				nc := &rows[ri].Cells[ci]
				nc.Value, nc.Runs = res.v, nil
				st := e.grids[s].baseStyle(row.Index, c.Column, c)
				if code := strings.TrimSpace(st.Format); res.v.Kind == Number && (code == "" || strings.EqualFold(code, "General")) {
					format := res.format
					if res.percent {
						format = percentFormat
					}
					if format != "" && !strings.EqualFold(format, "General") {
						ns := *st
						ns.Format = format
						nc.Style = &ns
					}
				}
			}
		}
		if rows != nil {
			ns := *sh
			ns.Rows = rows
			out.Sheets[s] = &ns
		}
	}
	if left > 0 {
		names := make([]string, 0, len(e.unsupported))
		for n := range e.unsupported {
			names = append(names, n)
		}
		sort.Strings(names)
		msg := fmt.Sprintf("%d formulas are not computed and print their stored results", left)
		if len(names) > 0 {
			msg += " (" + strings.Join(names, ", ") + ")"
		}
		b.warn(CodeFormula, msg)
	}
	return &out
}

// scalarFunction computes the functions of scalar arguments the
// evaluator knows besides the aggregates: ROW and COLUMN of the cell or of
// a reference, ABS, INT, MOD, ROUND, ISEVEN, ISODD, IF, AND, OR, NOT,
// HOUR, MINUTE, SECOND and SWITCH, as Calc defines them; false for
// another function or a wrong count of arguments.
//
// # Corpus evidence
//
//   - libreoffice/ods: sc/tdf130583.ods, SWITCH matches the number 1
//     with the text "1".
func (p *fparser) scalarFunction(fn string, args []fval) (fval, bool) {
	for _, a := range args {
		if a.rng != nil && fn != "ROW" && fn != "COLUMN" {
			// A range as a scalar argument reads as an array.
			return fval{}, false
		}
	}
	num := func(i int) (float64, *Value) {
		if i >= len(args) {
			return 0, nil
		}
		return number(scalar(args[i]))
	}
	val := func(x float64) (fval, bool) {
		if !finite(x) {
			return fval{v: Value{Kind: Error, Text: "#NUM!"}}, true
		}
		return fval{v: Value{Kind: Number, Number: x}}, true
	}
	boolean := func(b bool) (fval, bool) {
		if b {
			return fval{v: Value{Kind: Bool, Number: 1}}, true
		}
		return fval{v: Value{Kind: Bool}}, true
	}
	switch fn {
	case "ROW", "COLUMN":
		if len(args) > 1 {
			return fval{}, false
		}
		r, c := p.row, p.col
		if len(args) == 1 {
			if args[0].rng == nil {
				// A one-cell reference reads as its value: the
				// position is not known.
				return fval{}, false
			}
			r, c = args[0].rng.rg.FirstRow, args[0].rng.rg.FirstColumn
		}
		if r < 0 || c < 0 {
			return fval{}, false
		}
		if fn == "ROW" {
			return val(float64(r + 1))
		}
		return val(float64(c + 1))
	case "ABS", "INT", "ISEVEN", "ISODD":
		if len(args) != 1 {
			return fval{}, false
		}
		x, err := num(0)
		if err != nil {
			return fval{v: *err}, true
		}
		switch fn {
		case "ABS":
			return val(math.Abs(x))
		case "INT":
			return val(math.Floor(x))
		case "ISEVEN":
			return boolean(math.Mod(math.Trunc(x), 2) == 0)
		}
		return boolean(math.Mod(math.Trunc(x), 2) != 0)
	case "MOD", "ROUND":
		if len(args) != 2 {
			return fval{}, false
		}
		x, err := num(0)
		if err != nil {
			return fval{v: *err}, true
		}
		y, err := num(1)
		if err != nil {
			return fval{v: *err}, true
		}
		if fn == "MOD" {
			if y == 0 {
				return fval{v: Value{Kind: Error, Text: errDiv0}}, true
			}
			return val(x - y*math.Floor(x/y))
		}
		f := math.Pow(10, math.Trunc(y))
		return val(math.Round(x*f) / f)
	case "HOUR", "MINUTE", "SECOND":
		// The time of day, rounded to the second first, as Calc's
		// time functions read a serial (ScInterpreter::ScGetHour).
		if len(args) != 1 {
			return fval{}, false
		}
		x, err := num(0)
		if err != nil {
			return fval{v: *err}, true
		}
		secs := math.Round((x - math.Floor(x)) * 86400)
		switch fn {
		case "HOUR":
			return val(math.Mod(math.Floor(secs/3600), 24))
		case "MINUTE":
			return val(math.Mod(math.Floor(secs/60), 60))
		}
		return val(math.Mod(secs, 60))
	case "SWITCH":
		// SWITCH(expression; value1; result1; ...; default): the result
		// of the first value equal to the expression, else the default,
		// else #N/A.
		if len(args) < 3 {
			return fval{}, false
		}
		e := scalar(args[0])
		for i := 1; i+1 < len(args); i += 2 {
			x := scalar(args[i])
			// A number and a text compare as texts, as Calc's SWITCH
			// matches 1 with "1".
			if (e.Kind == Text) != (x.Kind == Text) && e.Kind != Error && x.Kind != Error {
				if strings.EqualFold(textOfValue(e), textOfValue(x)) {
					return args[i+1], true
				}
				continue
			}
			if c := compareValues("=", e, x); c.v.Kind == Bool && c.v.Number != 0 {
				return args[i+1], true
			}
		}
		if len(args)%2 == 0 {
			return args[len(args)-1], true
		}
		return fval{v: Value{Kind: Error, Text: "#N/A"}}, true
	case "NOT":
		if len(args) != 1 {
			return fval{}, false
		}
		x, err := num(0)
		if err != nil {
			return fval{v: *err}, true
		}
		return boolean(x == 0)
	case "AND", "OR":
		if len(args) == 0 {
			return fval{}, false
		}
		out := fn == "AND"
		for i := range args {
			x, err := num(i)
			if err != nil {
				return fval{v: *err}, true
			}
			if fn == "AND" {
				out = out && x != 0
			} else {
				out = out || x != 0
			}
		}
		return boolean(out)
	case "IF":
		if len(args) < 1 || len(args) > 3 {
			return fval{}, false
		}
		x, err := num(0)
		if err != nil {
			return fval{v: *err}, true
		}
		switch {
		case x != 0 && len(args) >= 2:
			return args[1], true
		case x != 0:
			return boolean(true)
		case len(args) == 3:
			return args[2], true
		}
		return boolean(false)
	}
	return fval{}, false
}
