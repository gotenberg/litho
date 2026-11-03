package pdf

import (
	"bytes"
	"context"
	"testing"
)

// TestConvertColors_LaterConversionReadsTheIntentAnEarlierOneSet pins that a
// conversion to the output intent finds the intent an earlier conversion on
// the same Writer wrote, whether both run on one Writer or in two Pipeline
// stages. The second conversion read the catalog of the source alone and
// failed with ErrNoOutputIntent.
func TestConvertColors_LaterConversionReadsTheIntentAnEarlierOneSet(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	src := recolorPDF("")
	for _, tc := range []struct {
		name string
		run  func(t *testing.T) []byte
	}{
		{"one Writer", func(t *testing.T) []byte {
			w := NewWriter(openColorPDF(t, src))
			t.Cleanup(func() { _ = w.Close() })
			if _, err := w.ConvertColors(ctx, CMYKColorTarget()); err != nil {
				t.Fatalf("ConvertColors to CMYK: %v", err)
			}
			report, err := w.ConvertColors(ctx, OutputIntentColorTarget())
			if err != nil {
				t.Fatalf("ConvertColors to the output intent: %v", err)
			}
			if report.TargetProfile != ProfileSourceOutputIntent {
				t.Errorf("the second conversion's profile came from %v, want the output intent", report.TargetProfile)
			}
			var out bytes.Buffer
			if err := w.WriteTo(ctx, &out); err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			return out.Bytes()
		}},
		{"two Pipeline stages", func(t *testing.T) []byte {
			var out bytes.Buffer
			_, err := NewPipeline().Open(openColorPDF(t, src)).
				Apply(func(ctx context.Context, w *Writer) error {
					_, err := w.ConvertColors(ctx, CMYKColorTarget())
					return err
				}).
				Apply(func(ctx context.Context, w *Writer) error {
					_, err := w.ConvertColors(ctx, OutputIntentColorTarget())
					return err
				}).
				WriteTo(ctx, &out)
			if err != nil {
				t.Fatalf("Pipeline: %v", err)
			}
			return out.Bytes()
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			out := tc.run(t)
			// The output states the intent the first conversion set, which a
			// conversion of the output finds as well.
			w := NewWriter(openColorPDF(t, out))
			t.Cleanup(func() { _ = w.Close() })
			if _, err := w.ConvertColors(ctx, OutputIntentColorTarget()); err != nil {
				t.Errorf("converting the output to its output intent: %v", err)
			}
		})
	}
}
