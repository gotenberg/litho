package compliance

import "testing"

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
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := findStreamDataStart([]byte(tc.buf))
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
