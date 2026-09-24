package filter

import (
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"io"
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

// TestNewDecoder_DuplicateNameLastWins pins the resolution [NewDecoder]
// documents: two plugins claiming the same filter name leave the last one
// registered in charge.
//
// A caller that registers a replacement for a built-in plugin depends on this,
// and the map insertion order is the only thing enforcing it, so a switch to
// "first wins" would be silent.
func TestNewDecoder_DuplicateNameLastWins(t *testing.T) {
	first := &mockPlugin{name: "JPXDecode", result: []byte("first")}
	second := &mockPlugin{name: "JPXDecode", result: []byte("second")}

	d := NewDecoder([]plugins.FilterDecoder{first, second})
	if d == nil {
		t.Fatal("NewDecoder returned nil for two plugins")
	}
	if !d.Handles("JPXDecode") {
		t.Fatal("the registered filter name is not handled")
	}

	stream := ast.NewStream(ast.PdfDict{"Filter": ast.PdfName("JPXDecode")}, []byte("raw"))
	out, err := d.DecodeStream(context.Background(), stream)
	if err != nil {
		t.Fatalf("DecodeStream: %v", err)
	}
	if string(out) != "second" {
		t.Errorf("decoded %q, want %q from the last plugin registered", out, "second")
	}
	if first.called {
		t.Error("the first plugin was consulted although a later one claimed the same name")
	}
}

// TestDecoder_HandlesIsFalseForAnUnregisteredName is the control for the
// Handles guard, including the documented nil-Decoder case.
func TestDecoder_HandlesIsFalseForAnUnregisteredName(t *testing.T) {
	d := NewDecoder([]plugins.FilterDecoder{&mockPlugin{name: "JPXDecode"}})
	if d.Handles("JBIG2Decode") {
		t.Error("an unregistered filter name reported as handled")
	}
	var nilDecoder *Decoder
	if nilDecoder.Handles("JPXDecode") {
		t.Error("a nil Decoder reported a filter as handled")
	}
}

// zlibBytes returns data compressed as a FlateDecode stream body.
func zlibBytes(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		t.Fatalf("zlib write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("zlib close: %v", err)
	}
	return buf.Bytes()
}

// TestDecoder_PluginDeclineFallsBackToBuiltin pins the decline contract of
// [plugins.ErrUnsupportedFilter]: a plugin that returns it, bare or wrapped,
// hands the stream to the built-in decoder for its filter, on both the eager
// and the streaming path.
func TestDecoder_PluginDeclineFallsBackToBuiltin(t *testing.T) {
	for name, declineErr := range map[string]error{
		"bare":    plugins.ErrUnsupportedFilter,
		"wrapped": fmt.Errorf("vendor codec: %w", plugins.ErrUnsupportedFilter),
	} {
		t.Run(name, func(t *testing.T) {
			p := &mockPlugin{name: "FlateDecode", err: declineErr}
			d := NewDecoder([]plugins.FilterDecoder{p})
			stream := ast.NewStream(ast.PdfDict{"Filter": ast.PdfName("FlateDecode")}, zlibBytes(t, []byte("hello")))

			got, err := d.DecodeStream(context.Background(), stream)
			if err != nil {
				t.Fatalf("DecodeStream: %v", err)
			}
			if !p.called {
				t.Fatal("the plugin was not consulted before the built-in decoder")
			}
			if string(got) != "hello" {
				t.Errorf("DecodeStream = %q, want the built-in FlateDecode output %q", got, "hello")
			}

			rc, err := d.NewStreamReader(context.Background(), stream)
			if err != nil {
				t.Fatalf("NewStreamReader: %v", err)
			}
			defer rc.Close()
			streamed, err := io.ReadAll(rc)
			if err != nil {
				t.Fatalf("reading the stream: %v", err)
			}
			if string(streamed) != "hello" {
				t.Errorf("NewStreamReader read %q, want %q", streamed, "hello")
			}
		})
	}
}

// TestDecoder_PluginDeclineWithoutBuiltinFails pins that a declined stream
// behind a filter litho does not know fails with the error such a filter
// gets when no plugin claims it.
func TestDecoder_PluginDeclineWithoutBuiltinFails(t *testing.T) {
	stream := ast.NewStream(ast.PdfDict{"Filter": ast.PdfName("VendorDecode")}, []byte("raw"))

	unclaimed := NewDecoder([]plugins.FilterDecoder{&mockPlugin{name: "OtherDecode"}})
	_, wantErr := unclaimed.DecodeStream(context.Background(), stream)
	if !errors.Is(wantErr, ErrUnsupportedFilter) {
		t.Fatalf("control: an unclaimed unknown filter returned %v, want ErrUnsupportedFilter", wantErr)
	}

	p := &mockPlugin{name: "VendorDecode", err: plugins.ErrUnsupportedFilter}
	d := NewDecoder([]plugins.FilterDecoder{p})
	out, err := d.DecodeStream(context.Background(), stream)
	if !p.called {
		t.Fatal("the plugin was not consulted")
	}
	if !errors.Is(err, ErrUnsupportedFilter) {
		t.Fatalf("DecodeStream error = %v, want ErrUnsupportedFilter", err)
	}
	if err.Error() != wantErr.Error() {
		t.Errorf("DecodeStream error = %q, want the unclaimed-filter error %q", err, wantErr)
	}
	if out != nil {
		t.Errorf("returned %d bytes alongside the error", len(out))
	}
}

// TestDecoder_PluginErrorOnBuiltinFilterFails is the control for the decline
// path: an error that is not [plugins.ErrUnsupportedFilter] fails the stream
// even when a built-in decoder could have read it.
func TestDecoder_PluginErrorOnBuiltinFilterFails(t *testing.T) {
	testErr := errors.New("plugin failed")
	p := &mockPlugin{name: "FlateDecode", err: testErr}
	d := NewDecoder([]plugins.FilterDecoder{p})
	stream := ast.NewStream(ast.PdfDict{"Filter": ast.PdfName("FlateDecode")}, zlibBytes(t, []byte("hello")))

	out, err := d.DecodeStream(context.Background(), stream)
	if !errors.Is(err, testErr) {
		t.Fatalf("DecodeStream error = %v, want the plugin's error", err)
	}
	if out != nil {
		t.Errorf("returned %q alongside the error: the built-in decoder ran", out)
	}
}
