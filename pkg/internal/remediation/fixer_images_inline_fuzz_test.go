package remediation

import (
	"bytes"
	"context"
	"testing"
)

// FuzzRespellInlineImageInterpolation asserts the respelling of inline image
// interpolation never panics on crafted bytes, leaves a stream it does not
// respell as written, changes nothing but a true into a false, keeps every
// inline image, and leaves nothing to respell a second time.
func FuzzRespellInlineImageInterpolation(f *testing.F) {
	f.Add([]byte("q BI /W 4 /H 1 /BPC 8 /CS /G /I true ID \x00\xff)\r EI Q BI /Interp#6flate true /I true /W 1 /H 1 /BPC 8 /CS /G ID \x01 EI"))
	f.Add([]byte("q BI /W 4 /H 1 /BPC 8 /CS /G /I true ID \x00")) // Truncated inside the image data.
	f.Fuzz(func(t *testing.T, data []byte) {
		ctx := context.Background()
		out, n := respellInlineImageInterpolation(ctx, data)
		if n == 0 {
			if !bytes.Equal(out, data) {
				t.Fatalf("respelled nothing in %q, but wrote %q", data, out)
			}
			return
		}
		falses := []byte("false")
		if !bytes.Equal(bytes.ReplaceAll(out, falses, []byte("true")), bytes.ReplaceAll(data, falses, []byte("true"))) {
			t.Fatalf("the respelling of %q changed more than a true: %q", data, out)
		}
		if in, got := len(inlineImageBytes(ctx, data)), len(inlineImageBytes(ctx, out)); in != got {
			t.Fatalf("the respelling of %q read %d inline images as %d: %q", data, in, got, out)
		}
		if _, again := respellInlineImageInterpolation(ctx, out); again != 0 {
			t.Fatalf("the respelling of %q left %d images to respell: %q", data, again, out)
		}
	})
}
