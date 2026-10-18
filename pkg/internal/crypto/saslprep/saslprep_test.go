package saslprep

import (
	"errors"
	"testing"
)

// TestRFC4013Examples runs the seven examples of RFC 4013, 3.
func TestRFC4013Examples(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		want    string
		wantErr error
	}{
		{"soft hyphen mapped to nothing", "I­X", "IX", nil},
		{"no transformation", "user", "user", nil},
		{"case preserved", "USER", "USER", nil},
		{"NFKC of ISO 8859-1 input", "ª", "a", nil},
		{"NFKC matches example 1", "Ⅸ", "IX", nil},
		{"prohibited character", "\u0007", "", ErrProhibited},
		{"bidirectional check", "ا1", "", ErrBidi},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			for _, prep := range []func(string) (string, error){Query, Stored} {
				got, err := prep(tt.in)
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("error = %v, want %v", err, tt.wantErr)
				}
				if got != tt.want {
					t.Errorf("got %q, want %q", got, tt.want)
				}
			}
		})
	}
}

func TestPrepare(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		in        string
		query     string
		queryErr  error
		storedErr error
	}{
		{"empty", "", "", nil, nil},
		{"printable ASCII unchanged", "p@ss w0rd~", "p@ss w0rd~", nil, nil},
		{"no-break space mapped to space", "a b", "a b", nil, nil},
		{"ideographic space mapped to space", "a　b", "a b", nil, nil},
		{"zero width space mapped to space before B.1", "a​b", "a b", nil, nil},
		{"zero width joiner mapped to nothing", "a‍b", "ab", nil, nil},
		{"variation selector mapped to nothing", "a️b", "ab", nil, nil},
		{"fullwidth folded by NFKC", "ＡＢ", "AB", nil, nil},
		{"ligature folded by NFKC", "ﬁ", "fi", nil, nil},
		{"composed by NFKC", "é", "é", nil, nil},
		{"tab prohibited", "a\tb", "", ErrProhibited, ErrProhibited},
		{"delete prohibited", "a\u007Fb", "", ErrProhibited, ErrProhibited},
		{"C1 control prohibited", "a\u0085b", "", ErrProhibited, ErrProhibited},
		{"private use prohibited", "a", "", ErrProhibited, ErrProhibited},
		{"noncharacter prohibited", "a﷐", "", ErrProhibited, ErrProhibited},
		{"replacement character prohibited", "a�", "", ErrProhibited, ErrProhibited},
		{"ideographic description prohibited", "⿰", "", ErrProhibited, ErrProhibited},
		{"left-to-right mark prohibited", "a‎", "", ErrProhibited, ErrProhibited},
		{"language tag prohibited", "a\U000E0001", "", ErrProhibited, ErrProhibited},
		{"right-to-left only", "אב", "אב", nil, nil},
		{"right-to-left around a digit", "ا1ب", "ا1ب", nil, nil},
		{"right-to-left mixed with left-to-right", "אaב", "", ErrBidi, ErrBidi},
		{"right-to-left not first", "1א", "", ErrBidi, ErrBidi},
		{"emoji unassigned in Unicode 3.2", "pw\U0001F600", "pw\U0001F600", nil, ErrUnassigned},
		{"invalid UTF-8", "a\xffb", "", ErrInvalidUTF8, ErrInvalidUTF8},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := Query(tt.in)
			if !errors.Is(err, tt.queryErr) {
				t.Fatalf("Query error = %v, want %v", err, tt.queryErr)
			}
			if got != tt.query {
				t.Errorf("Query = %q, want %q", got, tt.query)
			}
			if _, err := Stored(tt.in); !errors.Is(err, tt.storedErr) {
				t.Errorf("Stored error = %v, want %v", err, tt.storedErr)
			}
		})
	}
}

// TestTablesSorted pins that every generated table is sorted and disjoint,
// the precondition of [in]'s binary search.
func TestTablesSorted(t *testing.T) {
	t.Parallel()

	for name, table := range map[string][]runeRange{
		"unassigned": unassigned, "mappedToNothing": mappedToNothing, "nonASCIISpace": nonASCIISpace,
		"prohibited": prohibited, "randALCat": randALCat, "lCat": lCat,
	} {
		if len(table) == 0 {
			t.Errorf("%s is empty", name)
		}
		for i, r := range table {
			if r.hi < r.lo || (i > 0 && r.lo <= table[i-1].hi) {
				t.Errorf("%s: range %d {%04X, %04X} out of order", name, i, r.lo, r.hi)
			}
		}
	}
}
