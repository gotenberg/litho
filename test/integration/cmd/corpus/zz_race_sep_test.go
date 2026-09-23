package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/pdf"
)

// TestRaceSeparationRemediation guards Separation canonicalisation
// determinism. The fixer once picked the canonical (alt, tintFunc) per colorant
// by iterating the /ColorSpace dict in Go's randomised map order, so
// 6-2-4-4-t03-fail-d intermittently produced different remediated bytes (and an
// occasional 6.2.4.4-t02 litho-over) across runs. This remediates that fixture
// many times concurrently and asserts byte-identical output with no 6.2.4.4
// residual every time.
func TestRaceSeparationRemediation(t *testing.T) {
	dir := "../../testdata/verapdf/pdfs"
	others, _ := filepath.Glob(filepath.Join(dir, "*6-2-4-*"))
	target, _ := filepath.Glob(filepath.Join(dir, "*6-2-4-4-t03-fail-d*"))
	if len(target) == 0 {
		t.Skip("target fixture missing")
	}
	remediate := func(path string) (string, []string) {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", nil
		}
		ctx := context.Background()
		r, err := pdf.Open(ctx, bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return "", nil
		}
		defer r.Close()
		w := pdf.NewWriter(r)
		if _, err := w.RemediatePDFA(ctx, models.PDFA2a, pdf.WithAggressiveFixes()); err != nil {
			return "", nil
		}
		var buf bytes.Buffer
		if err := w.WriteTo(ctx, &buf); err != nil {
			return "", nil
		}
		sum := sha256.Sum256(buf.Bytes())
		r2, err := pdf.Open(ctx, bytes.NewReader(buf.Bytes()), int64(buf.Len()))
		if err != nil {
			return "", nil
		}
		defer r2.Close()
		vs, _ := r2.ValidatePDFAReport(ctx, models.PDFA2a)
		ids := make([]string, 0, len(vs))
		for _, v := range vs {
			ids = append(ids, v.Clause)
		}
		return string(sum[:]), ids
	}

	var mu sync.Mutex
	shas := map[string]bool{}
	var wg sync.WaitGroup
	for range 20 {
		for _, m := range others {
			wg.Add(1)
			go func(path string) { defer wg.Done(); _, _ = remediate(path) }(m)
		}
		wg.Go(func() {
			sum, ids := remediate(target[0])
			mu.Lock()
			shas[sum] = true
			for _, id := range ids {
				if id == "6.2.4.4" {
					t.Errorf("target fired 6.2.4.4 (litho-over)")
				}
			}
			mu.Unlock()
		})
	}
	wg.Wait()

	// Non-vacuity, asserted rather than assumed. Every failure path in
	// remediate returns an empty sha, so a break anywhere in the chain (open,
	// remediate, write, reopen) collapses shas to {""}, which is still ONE
	// entry and which the determinism check below reads as success.
	//
	// Measured, not supposed: forcing remediate to produce nothing took this
	// test from 6.41s to 0.01s and it still reported PASS.
	if shas[""] {
		t.Fatal("the target never remediated, so the determinism check proves nothing")
	}
	if len(shas) > 1 {
		t.Errorf("target remediation non-deterministic: %d distinct outputs", len(shas))
	}
}
