package text

import "context"

// Levels returns the embedding level of each character of the paragraph
// text whose base direction is base (UAX #9, X1 to I2): even levels run
// left to right, odd ones right to left. A text of no right-to-left
// character in a left-to-right paragraph is all at level 0. ctx's error
// stops it between two pieces of a long text.
func Levels(ctx context.Context, text []rune, base Direction) ([]int8, error) {
	at := make([]int, len(text))
	for i := range at {
		at[i] = i
	}
	return resolveLevels(ctx, text, at, base)
}
