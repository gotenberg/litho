package main

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// onePixelPNG returns a 1x1 opaque PNG, the smallest input that still exercises
// the decode and re-compress path stamp-image takes.
func onePixelPNG(t *testing.T) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 10, G: 20, B: 30, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encoding the PNG fixture: %v", err)
	}
	return buf.Bytes()
}

// pageOneImages reads the image XObjects back through the images command, the
// way listAttachments and listFormFields read their own subjects.
//
// Scanning the output bytes would not do: litho writes an incremental update,
// so the superseded page object is still present in the file.
func pageOneImages(t *testing.T, path string) []imageInfo {
	t.Helper()

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{"images", "-f", "json", path}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("images %s: exit %d, stderr: %s", path, rc, stderr.String())
	}
	var res imagesResult
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		t.Fatalf("decoding images JSON %q: %v", stdout.String(), err)
	}
	for _, p := range res.Pages {
		if p.Page == 1 {
			return p.Images
		}
	}
	return nil
}

// stampImageFails runs stamp-image and asserts it refused: non-zero exit,
// something on stderr, and no output file left behind.
func stampImageFails(t *testing.T, out string, args []string) {
	t.Helper()

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), args, &stdout, &stderr); rc == exitOK {
		t.Fatalf("stamp-image %v succeeded; stderr: %s", args, stderr.String())
	}
	if stderr.Len() == 0 {
		t.Errorf("stamp-image %v failed silently: nothing on stderr", args)
	}
	if _, err := os.Stat(out); err == nil {
		t.Errorf("%s exists: a failed stamp-image must not leave an output file", out)
	}
}

// TestStampImage_EmbedsTheImageAndDetectsTheMIMEType pins the success path and
// the -mime default in one go.
//
// mimeFromExt supplies the type when -mime is absent, so a .png that reaches
// the writer proves both the file read and the detection. The fixture carries
// no images of its own, which makes the "none before, one after" precondition
// real rather than decorative.
func TestStampImage_EmbedsTheImageAndDetectsTheMIMEType(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())
	dir := t.TempDir()
	logo := filepath.Join(dir, "logo.png")
	if err := os.WriteFile(logo, onePixelPNG(t), 0o600); err != nil {
		t.Fatalf("writing the PNG fixture: %v", err)
	}
	out := filepath.Join(dir, "stamped.pdf")

	if before := pageOneImages(t, src); len(before) != 0 {
		t.Fatalf("precondition: the fixture already carries %d image(s): %+v", len(before), before)
	}

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{
		"stamp-image", "-image", logo, "-x", "10", "-y", "10", "-o", out, src,
	}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("stamp-image: exit %d, stderr: %s", rc, stderr.String())
	}

	after := pageOneImages(t, out)
	if len(after) != 1 {
		t.Fatalf("page 1 reports %d image(s), want 1: %+v", len(after), after)
	}
	if after[0].Width != 1 || after[0].Height != 1 {
		t.Errorf("image is %dx%d, want 1x1: the stamped XObject is not the fixture",
			after[0].Width, after[0].Height)
	}
}

// TestStampImage_RejectsAnUnreadableImageFile covers the -image read path: the
// named file does not exist, so os.ReadFile fails before any PDF work.
func TestStampImage_RejectsAnUnreadableImageFile(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())
	dir := t.TempDir()
	out := filepath.Join(dir, "stamped.pdf")

	stampImageFails(t, out, []string{
		"stamp-image", "-image", filepath.Join(dir, "absent.png"), "-o", out, src,
	})
}

// TestStampImage_RejectsAnUnsupportedMIMEType covers a different failure point
// from the one above.
//
// The file reads cleanly here; what fails is [pdf.Writer.StampImage], which
// accepts only image/jpeg and image/png. A handler that read the bytes and
// passed whatever -mime said straight through would satisfy the missing-file
// test and fail this one.
func TestStampImage_RejectsAnUnsupportedMIMEType(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())
	dir := t.TempDir()
	logo := filepath.Join(dir, "logo.png")
	if err := os.WriteFile(logo, onePixelPNG(t), 0o600); err != nil {
		t.Fatalf("writing the PNG fixture: %v", err)
	}
	out := filepath.Join(dir, "stamped.pdf")

	stampImageFails(t, out, []string{
		"stamp-image", "-image", logo, "-mime", "text/plain", "-o", out, src,
	})
}

// TestStampImage_MissingImageIsAUsageError pins the required-flag check:
// -image, -o and the input file are all required.
func TestStampImage_MissingImageIsAUsageError(t *testing.T) {
	src := writeTempPDF(t, untaggedTextPDF())
	out := filepath.Join(t.TempDir(), "stamped.pdf")

	stampImageFails(t, out, []string{"stamp-image", "-o", out, src})
}
