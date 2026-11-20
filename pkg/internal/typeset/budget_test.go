package typeset_test

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/models"
)

// TestWrite_GivesBackTheStreamsCharge checks that what a write charges
// the budget for the streams it keeps in memory returns when the file is
// written, when the write spools past its threshold and when the stream
// is aborted: the budget's held bytes come back to what they were.
func TestWrite_GivesBackTheStreamsCharge(t *testing.T) {
	pages := func() []*typeset.Page {
		var out []*typeset.Page
		for range 4 {
			out = append(out, letter(run(t, sans, 12, typeset.Point{X: 20, Y: 40}, "Hello, World!")))
		}
		return out
	}
	for _, tc := range []struct {
		name  string
		spool int64
		abort bool
	}{
		{"written", -1, false},
		{"spooled", 500, false},
		{"aborted", -1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, budget, cancel := limits.WithBudget(context.Background(), models.Limits{})
			defer cancel()
			if err := budget.Charge(1000, "a conversion's own"); err != nil {
				t.Fatal(err)
			}
			before := budget.Held()
			s, err := typeset.NewStream(ctx, typeset.Options{Spool: tc.spool, TempDir: t.TempDir()}, false)
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range pages() {
				if err := s.Page(ctx, p); err != nil {
					t.Fatal(err)
				}
			}
			if tc.abort {
				if budget.Held() <= before && tc.spool < 0 {
					t.Errorf("held %d with the pages written, want more than %d", budget.Held(), before)
				}
				s.Abort()
			} else {
				var out bytes.Buffer
				if err := s.Close(ctx, &typeset.Document{}, &out); err != nil {
					t.Fatal(err)
				}
				if _, err := io.Copy(io.Discard, &out); err != nil {
					t.Fatal(err)
				}
			}
			if got := budget.Held(); got != before {
				t.Errorf("held %d after the write, want %d", got, before)
			}
		})
	}
}

// TestWrite_ChargesEachPageWritten checks that every page written costs
// the budget until the file is written, whether its streams stay in
// memory or spool: the page objects wait for the page tree at the end.
func TestWrite_ChargesEachPageWritten(t *testing.T) {
	held := func(pages int) int64 {
		ctx, budget, cancel := limits.WithBudget(context.Background(), models.Limits{})
		defer cancel()
		s, err := typeset.NewStream(ctx, typeset.Options{Spool: 0, TempDir: t.TempDir()}, false)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Abort()
		for range pages {
			if err := s.Page(ctx, letter(run(t, sans, 12, typeset.Point{X: 20, Y: 40}, "Hello, World!"))); err != nil {
				t.Fatal(err)
			}
		}
		return budget.Held()
	}
	one, ten := held(1), held(10)
	if per := (ten - one) / 9; per < 1<<10 {
		t.Errorf("a page written holds %d bytes of the budget, want at least 1 KiB", per)
	}
}
