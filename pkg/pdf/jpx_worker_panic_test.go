package pdf

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/filter/jpx"
)

// panicRecords is a slog handler that keeps the records of recovered worker
// panics.
type panicRecords struct {
	mu   sync.Mutex
	buf  bytes.Buffer
	next slog.Handler
}

func newPanicRecords() *panicRecords {
	p := &panicRecords{}
	p.next = slog.NewJSONHandler(&lockedWriter{p: p}, &slog.HandlerOptions{Level: slog.LevelError})
	return p
}

type lockedWriter struct{ p *panicRecords }

func (w *lockedWriter) Write(b []byte) (int, error) {
	w.p.mu.Lock()
	defer w.p.mu.Unlock()
	return w.p.buf.Write(b)
}

// records returns the logged records whose message names a worker panic.
func (p *panicRecords) records(t *testing.T) []map[string]any {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []map[string]any
	for line := range strings.SplitSeq(strings.TrimSpace(p.buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("log line %q: %v", line, err)
		}
		if rec["msg"] == "recovered panic in a worker goroutine" {
			out = append(out, rec)
		}
	}
	return out
}

// TestDecodeImage_LogsAJPXWorkerPanic plants a panic in a JPEG 2000 decoding
// worker and checks the public entry point returns it as ErrInternalPanic and
// logs it at Error with the recovered value and the worker's stack: the
// worker runs below the document and has no logger of its own. It does not
// run in parallel, since the hook affects every decode in the process.
func TestDecodeImage_LogsAJPXWorkerPanic(t *testing.T) {
	data := jpxColorPDF(t, "rgb8.j2k", "/ColorSpace /DeviceRGB /BitsPerComponent 8")
	logs := newPanicRecords()
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)), WithLogger(slog.New(logs.next)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	restore := jpx.SetWorkerHookForTest(3, func(item int) {
		if item == 0 {
			panic("planted worker panic")
		}
	})
	_, err = r.DecodeImage(context.Background(), 1, "Im0")
	restore()

	if !errors.Is(err, ErrInternalPanic) {
		t.Fatalf("DecodeImage: got %v, want ErrInternalPanic", err)
	}
	if !errors.Is(err, ErrImageNotDecodable) {
		t.Errorf("DecodeImage: got %v, want ErrImageNotDecodable too", err)
	}
	recs := logs.records(t)
	if len(recs) == 0 {
		t.Fatal("the worker panic was not logged")
	}
	for _, rec := range recs {
		if rec["level"] != "ERROR" || rec["panic"] != "planted worker panic" {
			t.Errorf("record %v: want level ERROR and the recovered value", rec)
		}
		if stack, _ := rec["stack"].(string); !strings.Contains(stack, "jpx.forEach") {
			t.Errorf("record carries no worker stack: %q", stack)
		}
	}

	// The hook is gone: the same image decodes.
	if _, err := r.DecodeImage(context.Background(), 1, "Im0"); err != nil {
		t.Fatalf("DecodeImage after restore: %v", err)
	}
}
