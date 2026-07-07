package compliance

import (
	"context"
	"testing"
)

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

func TestHasEndstreamAt(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		buf  string
		want bool
	}{
		{
			name: "endstream immediately",
			buf:  "endstream",
			want: true,
		},
		{
			name: "LF then endstream",
			buf:  "\nendstream",
			want: true,
		},
		{
			name: "CRLF then endstream",
			buf:  "\r\nendstream",
			want: true,
		},
		{
			name: "CR then endstream",
			buf:  "\rendstream",
			want: true,
		},
		{
			name: "no endstream",
			buf:  "some other data",
			want: false,
		},
		{
			name: "empty buffer",
			buf:  "",
			want: false,
		},
		{
			name: "partial endstream",
			buf:  "endstrea",
			want: false,
		},
		{
			name: "endstream with trailing content",
			buf:  "endstream\nendobj",
			want: true,
		},
		{
			name: "garbage then endstream",
			buf:  "XX endstream",
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := hasEndstreamAt([]byte(tc.buf))
			if got != tc.want {
				t.Errorf("hasEndstreamAt(%q) = %v, want %v", tc.buf, got, tc.want)
			}
		})
	}
}

func TestEndstreamHasExtraSpacing(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		buf  string
		want bool
	}{
		{"EOL then space then endstream", "data\r\n endstream", true},
		{"EOL then tab then endstream", "data\n\tendstream", true},
		{"valid LF before endstream", "data\nendstream", false},
		{"valid CRLF before endstream", "data\r\nendstream", false},
		{"no EOL, data byte before endstream", "dataendstream", false},
		{"space but no preceding EOL", "data endstream", false},
		{"no endstream", "some data", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := endstreamHasExtraSpacing([]byte(tc.buf))
			if got != tc.want {
				t.Errorf("endstreamHasExtraSpacing(%q) = %v, want %v", tc.buf, got, tc.want)
			}
		})
	}
}
