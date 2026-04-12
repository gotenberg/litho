package pdf

import (
	"bytes"
	"context"
	"testing"
)

// BenchmarkOpen_Small measures the cost of opening a minimal 1-page PDF
// end-to-end (xref parse + trailer decode + catalog resolution).
func BenchmarkOpen_Small(b *testing.B) {
	data := writerTestPDF()
	ctx := context.Background()
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for range b.N {
		r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
		if err != nil {
			b.Fatal(err)
		}
		_ = r.Close()
	}
}

// BenchmarkOpen_ThreePage measures the cost of opening a 3-page PDF with
// multiple font resources and content streams. This is the shape of the
// smallest realistic document the library is asked to handle.
func BenchmarkOpen_ThreePage(b *testing.B) {
	data := threePagePDF()
	ctx := context.Background()
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for range b.N {
		r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
		if err != nil {
			b.Fatal(err)
		}
		_ = r.Close()
	}
}

// BenchmarkExtractText_SinglePage measures text extraction throughput on
// one page of the threePagePDF fixture. The document is opened once
// outside the measured loop so the benchmark isolates extraction cost.
func BenchmarkExtractText_SinglePage(b *testing.B) {
	data := threePagePDF()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		b.Fatal(err)
	}
	defer r.Close()

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := r.ExtractText(ctx, 1); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkExtractText_AllPages measures text extraction across every
// page of a 3-page document. The aggregate case a Gotenberg worker
// typically encounters per request.
func BenchmarkExtractText_AllPages(b *testing.B) {
	data := threePagePDF()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		b.Fatal(err)
	}
	defer r.Close()

	n, _ := r.PageCount(ctx)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		for i := 1; i <= n; i++ {
			if _, err := r.ExtractText(ctx, i); err != nil {
				b.Fatal(err)
			}
		}
	}
}

// BenchmarkMetadata measures the cost of extracting document-level
// metadata (Info + XMP). This is a fixed-cost operation per document.
// Tracking it catches regressions in catalog resolution or XMP parsing.
func BenchmarkMetadata(b *testing.B) {
	data := threePagePDF()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		b.Fatal(err)
	}
	defer r.Close()

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := r.Metadata(ctx); err != nil {
			b.Fatal(err)
		}
	}
}
