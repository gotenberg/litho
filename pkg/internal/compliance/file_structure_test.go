package compliance

import (
	"bytes"
	"context"
	"testing"
)

func TestStreamLengthMatches(t *testing.T) {
	t.Parallel()
	// win is laid out as <4 pre-window bytes><gap><endstream>, so expOff is 4.
	const expOff = 4
	tests := []struct {
		name string
		win  string
		want bool
	}{
		{"LF gap (declared end + LF + endstream)", "AAAA\nendstream", true},
		{"lone CR gap", "AAAA\rendstream", true},
		{"CRLF gap (CR may be data)", "AAAA\r\nendstream", true},
		{"no gap, last byte is data", "AAAAendstream", true},
		{"no gap, last byte is LF (length counted the EOL)", "AAA\nendstream", false},
		{"no gap, last byte is CR", "AAA\rendstream", false},
		{"two-byte gap that is not CRLF", "AAAA\n\nendstream", false},
		{"gap of three (length too small)", "AAAA\n\n\nendstream", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			win := []byte(tc.win)
			esRel := bytes.Index(win, []byte("endstream"))
			if got := streamLengthMatches(win, expOff, esRel); got != tc.want {
				t.Errorf("streamLengthMatches(%q, %d, %d) = %v, want %v", tc.win, expOff, esRel, got, tc.want)
			}
		})
	}
}

func TestCheckHeaderBytes(t *testing.T) {
	t.Parallel()
	// countRule returns how many violations carry the given rule.
	countRule := func(vs []Violation, r Rule) int {
		n := 0
		for _, v := range vs {
			if v.Rule == r {
				n++
			}
		}
		return n
	}
	t1 := Rule_19005_2_6_1_2_t01
	t2 := Rule_19005_2_6_1_2_t02
	tests := []struct {
		name   string
		buf    string
		wantT1 int
		wantT2 int
	}{
		{"conformant header + binary marker", "%PDF-1.4\n%\xe2\xe3\xcf\xd3\n1 0 obj", 0, 0},
		{"missing binary marker", "%PDF-1.4\n1 0 obj", 0, 1},
		{"trailing space then no marker fires both t1 and t2", "%PDF-1.0 \r\n9 0 obj", 1, 1},
		{"trailing space, CR only, no marker", "%PDF-1.0 \r17 0 obj", 1, 1},
		{"binary marker byte not high", "%PDF-1.4\n%\xe2\x20\xcf\xd3\n1 0 obj", 0, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := checkHeaderBytes([]byte(tc.buf), "PDF/A-2b")
			if c := countRule(got, t1); c != tc.wantT1 {
				t.Errorf("t1 count = %d, want %d (got %v)", c, tc.wantT1, got)
			}
			if c := countRule(got, t2); c != tc.wantT2 {
				t.Errorf("t2 count = %d, want %d (got %v)", c, tc.wantT2, got)
			}
		})
	}
}

// TestCheckHeaderBytesPart1Lenient locks the ISO 19005-1 §6.1.2 t1 leniency:
// its predicate `/%PDF-\d\.\d/` is unanchored, so a byte after the version
// (e.g. a trailing space) does not fail the header rule at part 1, unlike the
// anchored `/^%PDF-1\.[0-7]$/` of parts 2-4. The independent t2 binary comment
// check still runs.
func TestCheckHeaderBytesPart1Lenient(t *testing.T) {
	t.Parallel()
	countRule := func(vs []Violation, r Rule) int {
		n := 0
		for _, v := range vs {
			if v.Rule == r {
				n++
			}
		}
		return n
	}
	t1 := Rule_19005_1_6_1_2_t01
	t2 := Rule_19005_1_6_1_2_t02
	tests := []struct {
		name           string
		buf            string
		wantT1, wantT2 int
	}{
		{"trailing space then binary marker is conformant at part 1", "%PDF-1.2 \r%\xe2\xe3\xcf\xd3\r\n1 0 obj", 0, 0},
		{"trailing space, no marker: t1 silent, t2 fires", "%PDF-1.0 \r\n9 0 obj", 0, 1},
		{"clean header + marker", "%PDF-1.4\n%\xe2\xe3\xcf\xd3\n1 0 obj", 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := checkHeaderBytes([]byte(tc.buf), "PDF/A-1b")
			if c := countRule(got, t1); c != tc.wantT1 {
				t.Errorf("t1 count = %d, want %d (got %v)", c, tc.wantT1, got)
			}
			if c := countRule(got, t2); c != tc.wantT2 {
				t.Errorf("t2 count = %d, want %d (got %v)", c, tc.wantT2, got)
			}
		})
	}
}

func TestFindStreamDataStart(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		buf  string
		want int
	}{
		{
			name: "stream followed by LF",
			buf:  "10 0 obj\n<< /Length 5 >>\nstream\nhello",
			want: 32, // right after "stream\n"
		},
		{
			name: "stream followed by CRLF",
			buf:  "10 0 obj\n<< /Length 5 >>\nstream\r\nhello",
			want: 33, // right after "stream\r\n"
		},
		{
			name: "no stream keyword",
			buf:  "10 0 obj\n<< /Type /Catalog >>\nendobj",
			want: -1,
		},
		{
			name: "endstream but no stream",
			buf:  "endstream\nendobj",
			want: -1,
		},
		{
			name: "endstream then stream",
			buf:  "endstream\nstream\ndata",
			want: 17, // after second "stream\n"
		},
		{
			name: "stream at start",
			buf:  "stream\ndata",
			want: 7,
		},
		{
			// A malformed keyword (followed by a space, not an EOL) still locates
			// the data immediately after "stream" so the /Length check can run.
			name: "malformed: stream followed by space",
			buf:  "stream \nx",
			want: 6,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := findStreamDataStart(context.Background(), []byte(tc.buf))
			if got != tc.want {
				t.Errorf("findStreamDataStart(%q) = %d, want %d", tc.buf, got, tc.want)
			}
		})
	}
}
