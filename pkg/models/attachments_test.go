package models

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestEmbeddedFile_JSONRoundTrip(t *testing.T) {
	in := EmbeddedFile{
		Name:     "attachment.txt",
		MIMEType: "text/plain",
		Open: func(_ context.Context) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("data")), nil
		},
	}
	assertRoundTripExceptOpen(t, in, func(v *EmbeddedFile) { v.Open = nil })
}
