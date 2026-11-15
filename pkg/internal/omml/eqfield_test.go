package omml

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// TestReadField maps the switches of an EQ field (ECMA-376 Part 1,
// 17.16.5.20) to formula elements, the field set in its run's families.
func TestReadField(t *testing.T) {
	t.Parallel()
	st := RunStyle{Size: 11, Color: typeset.Color{A: 1}}
	for _, c := range []struct {
		instr, want string
	}{
		{` EQ \f(1,2) `, "(math (mfrac (mrow (mtext '1')) (mrow (mtext '2'))))"},
		{`EQ \r(3,x)`, "(math (mroot (mrow (mtext 'x')) (mrow (mtext '3'))))"},
		{`EQ \s\up8(2)`, "(math (msup (mrow) (mrow (mtext '2'))))"},
		{`EQ \b \bc\{ (a\,b)`, "(math (mrow (mo form=prefix fence=true stretchy=true '{') (mrow (mtext 'a,b')) (mo form=postfix fence=true stretchy=true '}')))"},
		{`eq \o\ac(○,印)`, "(math (mrow (mpadded width=0 (mrow (mtext '○'))) (mpadded width=0 (mrow (mtext '印'))) (mspace)))"},
		{`EQ \x(a)`, "(math (menclose notation=box (mrow (mtext 'a'))))"},
		{`EQ \x \to\le(a)`, "(math (menclose notation=top left (mrow (mtext 'a'))))"},
		{`EQ \l(a,b)`, "(math (mrow (mrow (mtext 'a')) (mo ',') (mrow (mtext 'b'))))"},
		{`EQ \i \su(k=1,n,k)`, "(math (mrow (munderover (mo largeop=true movablelimits=false '∑') (mrow (mtext 'k=1')) (mrow (mtext 'n'))) (mrow (mtext 'k'))))"},
		{`EQ \a \co2 (a,b,c,d)`, "(math (mtable (mtr (mtd (mspace height=0.85em depth=0.32em) (mrow (mtext 'a'))) (mtd (mspace height=0.85em depth=0.32em) (mrow (mtext 'b')))) " +
			"(mtr (mtd (mspace height=0.85em depth=0.32em) (mrow (mtext 'c'))) (mtd (mspace height=0.85em depth=0.32em) (mrow (mtext 'd'))))))"},
	} {
		q := ReadField(c.instr, []string{"Calibri"}, st)
		if q == nil {
			t.Errorf("%s: no equation", c.instr)
			continue
		}
		if got := tree(q.root); got != c.want {
			t.Errorf("%s:\n%s\nwant\n%s", c.instr, got, c.want)
		}
		if q.Style().Families[0] != "Calibri" {
			t.Errorf("%s: families %v", c.instr, q.Style().Families)
		}
	}
	for _, instr := range []string{"PAGE", "EQUATION", "EQ", `EQ \x(`} {
		if q := ReadField(instr, nil, st); q != nil && instr != `EQ \x(` {
			t.Errorf("%q reads as an equation", instr)
		}
	}
}

// FuzzReadField reads any EQ instruction within its bounds.
func FuzzReadField(f *testing.F) {
	f.Add(`EQ \a \al (Milk,Eggs,Detergent)`)
	f.Add(`EQ \f(\r(3,x`)
	f.Fuzz(func(_ *testing.T, instr string) {
		if q := ReadField(instr, nil, RunStyle{Size: 10}); q != nil {
			_ = q.AltText()
		}
	})
}
