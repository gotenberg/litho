package mutations

import (
	"math"
	"strings"
	"testing"
)

// TestStampTextRejectsNonFiniteGeometry pins that a coordinate PDF has no
// spelling for is reported rather than written.
//
// ISO 32000-1, 7.3.3 defines a number as an optional sign, digits and an
// optional decimal point: there is no NaN and no infinity. Go's flag package
// parses both into a float64, and the formatter wrote them straight into the
// content stream, so `litho stamp-text -x NaN` exited 0 and produced a page
// poppler rejects with "Unknown operator 'NaN'" and then "Too few (1) args to
// 'Td'".
func TestStampTextRejectsNonFiniteGeometry(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		opts TextStampOptions
		want string
	}{
		{"NaN x", TextStampOptions{Text: "hi", X: math.NaN()}, "x"},
		{"positive infinity y", TextStampOptions{Text: "hi", Y: math.Inf(1)}, "y"},
		{"negative infinity rotation", TextStampOptions{Text: "hi", Rotation: math.Inf(-1)}, "rotation"},
		{"NaN font size", TextStampOptions{Text: "hi", FontSize: math.NaN()}, "font size"},
		{"NaN opacity", TextStampOptions{Text: "hi", Opacity: math.NaN()}, "opacity"},
	} {
		ctx, doc, pages, alloc := stampDoc(t)
		err := StampText(ctx, alloc, doc, pages, tc.opts, nil)
		if err == nil {
			t.Errorf("%s: stamping reported success", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %v does not name %q", tc.name, err, tc.want)
		}
	}
}

// TestStampTextAcceptsFiniteGeometry is the control: ordinary numbers, including
// negative and zero, must still stamp.
func TestStampTextAcceptsFiniteGeometry(t *testing.T) {
	t.Parallel()

	ctx, doc, pages, alloc := stampDoc(t)
	if err := StampText(ctx, alloc, doc, pages, TextStampOptions{
		Text:     "hi",
		X:        -10,
		Y:        0,
		FontSize: 12,
		Rotation: 45,
	}, nil); err != nil {
		t.Fatalf("StampText: %v", err)
	}
	if content := stampedContent(t, ctx, alloc); !strings.Contains(content, "(hi) Tj") {
		t.Errorf("the stamp was not drawn: %q", content)
	}
}

// TestFiniteValuesNamesTheOptionDeterministically pins that the same bad input
// always reports the same option, so the message does not depend on Go's map
// iteration order.
func TestFiniteValuesNamesTheOptionDeterministically(t *testing.T) {
	t.Parallel()

	bad := map[string]float64{
		"rotation": math.NaN(),
		"opacity":  math.NaN(),
		"x":        math.NaN(),
	}
	first := ""
	for range 12 {
		err := finiteValues(bad)
		if err == nil {
			t.Fatal("no error for a map full of NaN")
		}
		if first == "" {
			first = err.Error()
			continue
		}
		if err.Error() != first {
			t.Fatalf("the reported option changed between runs: %q then %q", first, err.Error())
		}
	}
}

// TestFiniteValuesAcceptsOrdinaryNumbers is the control for the helper itself.
func TestFiniteValuesAcceptsOrdinaryNumbers(t *testing.T) {
	t.Parallel()

	if err := finiteValues(map[string]float64{
		"x": 0, "y": -1.5, "scale": 1e6, "tiny": 1e-9,
	}); err != nil {
		t.Errorf("finiteValues rejected ordinary numbers: %v", err)
	}
}
