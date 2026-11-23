package odf

import (
	"context"
	"errors"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

func TestParseXML_DeclaredEncodings(t *testing.T) {
	tests := []struct {
		name, doc, want string
		err             error
	}{
		{"us-ascii", `<?xml version="1.0" encoding="us-ascii"?><a>text</a>`, "text", nil},
		{"ASCII upper case", `<?xml version="1.0" encoding="US-ASCII"?><a>text</a>`, "text", nil},
		{"latin-1", "<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?><a>caf\xe9</a>", "café", nil},
		{"windows-1252", "<?xml version=\"1.0\" encoding=\"windows-1252\"?><a>\x80</a>", "€", nil},
		{"UTF-16 with its byte order mark", "\xff\xfe<\x00?\x00x\x00m\x00l\x00 \x00v\x00e\x00r\x00s\x00i\x00o\x00n\x00=\x00\"\x001\x00.\x000\x00\"\x00 \x00e\x00n\x00c\x00o\x00d\x00i\x00n\x00g\x00=\x00\"\x00U\x00T\x00F\x00-\x001\x006\x00\"\x00?\x00>\x00<\x00a\x00>\x00\xe9\x00<\x00/\x00a\x00>\x00", "é", nil},
		{"unknown", `<?xml version="1.0" encoding="x-unknown"?><a/>`, "", office.ErrMalformed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			el, err := ParseXML(context.Background(), []byte(tt.doc))
			if tt.err != nil {
				if !errors.Is(err, tt.err) {
					t.Fatalf("err = %v, want %v", err, tt.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := el.TextContent(); got != tt.want {
				t.Errorf("text = %q, want %q", got, tt.want)
			}
		})
	}
}
