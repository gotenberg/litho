package filter

import (
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/plugins"
)

// mockPlugin implements plugins.FilterDecoder for testing.
type mockPlugin struct {
	name   string
	result []byte
	err    error
	called bool
	parms  map[string]any
}

func (m *mockPlugin) FilterName() string { return m.name }

func (m *mockPlugin) Decode(data []byte, parms map[string]any) ([]byte, error) {
	m.called = true
	m.parms = parms
	if m.err != nil {
		return nil, m.err
	}
	return m.result, nil
}

func TestDecoder_NilDelegatesToPackageLevel(t *testing.T) {
	// A nil Decoder should delegate to DecodeStream.
	var d *Decoder

	// Unfiltered stream should return raw bytes.
	stream := ast.NewStream(ast.PdfDict{}, []byte("hello"))
	got, err := d.DecodeStream(context.Background(), stream)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(got, []byte("hello")) {
		t.Errorf("got %q, want %q", got, "hello")
	}
}

func TestDecoder_PluginCalledForRegisteredFilter(t *testing.T) {
	p := &mockPlugin{name: "TestFilter", result: []byte("decoded")}
	d := NewDecoder([]plugins.FilterDecoder{p})

	stream := ast.NewStream(ast.PdfDict{
		"Filter": ast.PdfName("TestFilter"),
	}, []byte("raw"))

	got, err := d.DecodeStream(context.Background(), stream)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !p.called {
		t.Fatal("plugin was not called")
	}
	if !bytes.Equal(got, []byte("decoded")) {
		t.Errorf("got %q, want %q", got, "decoded")
	}
}

func TestDecoder_FallsBackToBuiltinForUnregisteredFilter(t *testing.T) {
	p := &mockPlugin{name: "TestFilter", result: []byte("decoded")}
	d := NewDecoder([]plugins.FilterDecoder{p})

	// Use FlateDecode, not registered as a plugin. Should use built-in.
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	_, _ = w.Write([]byte("hello"))
	_ = w.Close()

	stream := ast.NewStream(ast.PdfDict{
		"Filter": ast.PdfName("FlateDecode"),
	}, buf.Bytes())

	got, err := d.DecodeStream(context.Background(), stream)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(got, []byte("hello")) {
		t.Errorf("got %q, want %q", got, "hello")
	}
	if p.called {
		t.Fatal("plugin should not have been called for FlateDecode")
	}
}

func TestDecoder_PluginErrorPropagated(t *testing.T) {
	testErr := errors.New("plugin failed")
	p := &mockPlugin{name: "FailFilter", err: testErr}
	d := NewDecoder([]plugins.FilterDecoder{p})

	stream := ast.NewStream(ast.PdfDict{
		"Filter": ast.PdfName("FailFilter"),
	}, []byte("data"))

	_, err := d.DecodeStream(context.Background(), stream)
	if !errors.Is(err, testErr) {
		t.Errorf("expected %v, got %v", testErr, err)
	}
}

func TestDecoder_MarshalParmsConvertsTypes(t *testing.T) {
	p := &mockPlugin{name: "TestFilter", result: []byte("ok")}
	d := NewDecoder([]plugins.FilterDecoder{p})

	stream := ast.NewStream(ast.PdfDict{
		"Filter": ast.PdfName("TestFilter"),
		"DecodeParms": ast.PdfDict{
			"Width":  ast.PdfInt(100),
			"Height": ast.PdfReal(200.5),
			"Flag":   ast.PdfBool(true),
			"Name":   ast.PdfName("Foo"),
		},
	}, []byte("data"))

	_, err := d.DecodeStream(context.Background(), stream)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if p.parms["Width"] != 100 {
		t.Errorf("Width: got %v, want 100", p.parms["Width"])
	}
	if p.parms["Height"] != 200.5 {
		t.Errorf("Height: got %v, want 200.5", p.parms["Height"])
	}
	if p.parms["Flag"] != true {
		t.Errorf("Flag: got %v, want true", p.parms["Flag"])
	}
	if p.parms["Name"] != "Foo" {
		t.Errorf("Name: got %v, want Foo", p.parms["Name"])
	}
}

func TestDecoder_NewStreamReaderDelegatesToPackageLevelForBuiltins(t *testing.T) {
	p := &mockPlugin{name: "TestFilter", result: []byte("ok")}
	d := NewDecoder([]plugins.FilterDecoder{p})

	// FlateDecode stream. Should use the fast path (no plugin needed).
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	_, _ = w.Write([]byte("streamed"))
	_ = w.Close()

	stream := ast.NewStream(ast.PdfDict{
		"Filter": ast.PdfName("FlateDecode"),
	}, buf.Bytes())

	rc, err := d.NewStreamReader(context.Background(), stream)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer rc.Close()

	var out bytes.Buffer
	_, _ = out.ReadFrom(rc)
	if out.String() != "streamed" {
		t.Errorf("got %q, want %q", out.String(), "streamed")
	}
}

func TestNewDecoder_NilWhenEmpty(t *testing.T) {
	d := NewDecoder(nil)
	if d != nil {
		t.Error("expected nil Decoder for empty plugins")
	}

	d = NewDecoder([]plugins.FilterDecoder{})
	if d != nil {
		t.Error("expected nil Decoder for empty slice")
	}
}
