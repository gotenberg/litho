package filter

import (
	"bytes"
	"errors"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func TestDecodeCrypt(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		parms   ast.PdfDict
		want    []byte
		wantErr bool
		errIs   error
	}{
		{
			name:  "identity passthrough - no parms",
			data:  []byte("hello"),
			parms: nil,
			want:  []byte("hello"),
		},
		{
			name:  "identity passthrough - explicit Identity",
			data:  []byte("hello"),
			parms: ast.PdfDict{"Name": ast.PdfName("Identity")},
			want:  []byte("hello"),
		},
		{
			name:  "empty data - identity",
			data:  []byte{},
			parms: nil,
			want:  []byte{},
		},
		{
			name:    "unsupported sub-filter",
			data:    []byte("hello"),
			parms:   ast.PdfDict{"Name": ast.PdfName("StdCF")},
			wantErr: true,
			errIs:   ErrUnsupportedFilter,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeCrypt(tt.data, tt.parms)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errIs != nil && !errors.Is(err, tt.errIs) {
					t.Errorf("expected error wrapping %v, got: %v", tt.errIs, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !bytes.Equal(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
