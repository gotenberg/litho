package pdf

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

// TestPerPageMethodsNameThePageOnce holds every per-page [Reader] method to the
// error convention of CONTRIBUTING.md: its error starts with the page, named
// once. RenderPage once read "page 9: page number out of bounds: 9 (document has 3
// pages)", and a caller adding its own prefix read the page three times.
func TestPerPageMethodsNameThePageOnce(t *testing.T) {
	t.Parallel()

	data := multiPagePDF()
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })

	ctx := context.Background()
	const want = "page 9: page number out of bounds: the document has 3 pages"
	for name, call := range map[string]func() error{
		"RenderPage":          func() error { _, err := r.RenderPage(ctx, 9); return err },
		"RenderPageReport":    func() error { _, _, err := r.RenderPageReport(ctx, 9); return err },
		"ExtractText":         func() error { _, err := r.ExtractText(ctx, 9); return err },
		"ExtractTables":       func() error { _, err := r.ExtractTables(ctx, 9); return err },
		"ExtractAnnotations":  func() error { _, err := r.ExtractAnnotations(ctx, 9); return err },
		"Page":                func() error { _, err := r.Page(ctx, 9); return err },
		"ExtractTextElements": func() error { _, err := r.ExtractTextElements(ctx, 9); return err },
		"ExtractImages":       func() error { _, err := r.ExtractImages(ctx, 9); return err },
		"DecodeImage":         func() error { _, err := r.DecodeImage(ctx, 9, "Im0"); return err },
		"ExtractLines":        func() error { _, err := r.ExtractLines(ctx, 9); return err },
		"ExtractGeoData":      func() error { _, err := r.ExtractGeoData(ctx, 9); return err },
		"ExtractMeasurements": func() error { _, err := r.ExtractMeasurements(ctx, 9); return err },
		"Extract3DModels":     func() error { _, err := r.Extract3DModels(ctx, 9); return err },
		"ExtractRedactedText": func() error { _, err := r.ExtractRedactedText(ctx, 9); return err },
		"ExtractUnseenText":   func() error { _, err := r.ExtractUnseenText(ctx, 9); return err },
		"CheckTextContrast":   func() error { _, err := r.CheckTextContrast(ctx, 9); return err },
		"ExtractColorSpaces":  func() error { _, err := r.ExtractColorSpaces(ctx, 9); return err },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := call()
			if !errors.Is(err, ErrInvalidPageNumber) {
				t.Fatalf("error = %v, want ErrInvalidPageNumber", err)
			}
			if err.Error() != want {
				t.Errorf("error = %q, want %q", err, want)
			}
		})
	}
}

// TestRenderTooLargeNamesItsFailureOnce holds a render past its budget to one
// "page N" and one sentinel: it once read "page 1: page render exceeds its
// budget: render exceeds its budget: ...".
func TestRenderTooLargeNamesItsFailureOnce(t *testing.T) {
	t.Parallel()

	data := multiPagePDF()
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)), WithLimits(Limits{MaxRenderPixels: 1000}))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })

	_, err = r.RenderPage(context.Background(), 1)
	if !errors.Is(err, ErrRenderTooLarge) || !IsInvalidInput(err) {
		t.Fatalf("error = %v, want an invalid input wrapping ErrRenderTooLarge", err)
	}
	msg := err.Error()
	if !strings.HasPrefix(msg, "page 1: page render exceeds its budget: ") {
		t.Errorf("error = %q, want it to start with the page and the sentinel", msg)
	}
	if n := strings.Count(msg, "exceeds its budget"); n != 1 {
		t.Errorf("error = %q names the budget %d times, want once", msg, n)
	}
}

// TestImageDecodeErrorsNameThePageOnce holds the Decode function
// [Reader.ExtractImages] lists to the same message as [Reader.DecodeImage]: it
// runs after the method returned, so it names its page itself.
func TestImageDecodeErrorsNameThePageOnce(t *testing.T) {
	t.Parallel()

	r := openImagePDF(t)
	ctx := context.Background()
	const prefix = `page 1: image "Bad": `

	_, err := r.DecodeImage(ctx, 1, "Bad")
	if err == nil || !strings.HasPrefix(err.Error(), prefix) || strings.Count(err.Error(), "page 1") != 1 {
		t.Errorf("DecodeImage error = %v, want it to start with %q, the page named once", err, prefix)
	}

	metas, err := r.ExtractImages(ctx, 1)
	if err != nil {
		t.Fatalf("ExtractImages: %v", err)
	}
	for _, m := range metas {
		if m.Filename != "Bad" {
			continue
		}
		_, err := m.Decode(ctx)
		if err == nil || !strings.HasPrefix(err.Error(), prefix) || strings.Count(err.Error(), "page 1") != 1 {
			t.Errorf("Decode error = %v, want it to start with %q, the page named once", err, prefix)
		}
	}
}
