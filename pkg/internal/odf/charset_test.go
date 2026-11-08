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
