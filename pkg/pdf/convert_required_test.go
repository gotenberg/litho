package pdf

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

// TestWithRequiredResources_FailsEveryLoadThatFails states, one row for
// each way a resource does not load, what [WithRequiredResources] makes
// of it: the warning the conversion reports without the option, and, with
// it, either an error wrapping [ErrConvertResourceUnavailable] that names
// the resource once, or the same warning when the resource arrived whole
// and only litho cannot use it.
func TestWithRequiredResources_FailsEveryLoadThatFails(t *testing.T) {
	t.Parallel()

	fonts := WithConvertFontDirs(convertFontDir(t))
	pic := requiredPNG(t, 10, 10)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a.png":
			_, _ = w.Write(pic) //nolint:errcheck // the test reads what arrives
		case "/plain.css":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("p { color: red }")) //nolint:errcheck // as above
		default:
			http.Error(w, "gone", http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	files := fstest.MapFS{
		"a.png":    {Data: pic},
		"bad.png":  {Data: []byte("\x89PNG\r\n\x1a\nbroken")},
		"a.ico":    {Data: []byte{0, 0, 1, 0, 1, 0, 16, 16, 0, 0}},
		"bad.ttf":  {Data: []byte("\x00\x01\x00\x00broken")},
		"face.ttf": {Data: convertTestFont()},
	}
	big := requiredBigFS{name: "big.png", size: 33 << 20}

	for _, tc := range []struct {
		name string
		src  string
		opts []HTMLOption
		// code is the warning the conversion reports without the option,
		// about the text the message holds.
		code  ConvertWarningCode
		about string
		// fails reports a load that fails; otherwise the resource arrived
		// and the option changes nothing.
		fails bool
	}{
		{"relative URL without resources", `<img src="a.png" alt=x>`, nil, WarningResourceRefused, "a.png", true},
		{"path leaving the root", `<img src="../a.png" alt=x>`, []HTMLOption{WithResources(files)}, WarningResourceRefused, "a.png", true},
		{"file URL", `<img src="file:///etc/a.png" alt=x>`, []HTMLOption{WithResources(files)}, WarningResourceRefused, "a.png", true},
		{"scheme never fetched", `<img src="ftp://example.com/a.png" alt=x>`, []HTMLOption{WithResources(files)}, WarningResourceRefused, "a.png", true},
		{"no HTTP client", `<img src="https://example.com/a.png" alt=x>`, []HTMLOption{WithResources(files)}, WarningResourceNoNetwork, "a.png", true},
		{"status outside 2xx", `<img src="` + server.URL + `/gone.png" alt=x>`, []HTMLOption{WithHTTPClient(server.Client())}, WarningResourceHTTPStatus, "404", true},
		{"request that fails", `<img src="` + closed.URL + `/a.png" alt=x>`, []HTMLOption{WithHTTPClient(&http.Client{Timeout: 5 * time.Second})}, WarningResourceFailed, "a.png", true},
		{"file that does not exist", `<img src="gone.png" alt=x>`, []HTMLOption{WithResources(files)}, WarningResourceFailed, "gone.png", true},
		{"stylesheet served as text/plain", `<link rel=stylesheet href="` + server.URL + `/plain.css"><p>x`, []HTMLOption{WithHTTPClient(server.Client())}, WarningResourceFailed, "text/plain", true},
		{"stylesheet for another medium", `<link rel=stylesheet media=screen href="gone.css"><p>x`, []HTMLOption{WithResources(files)}, WarningResourceFailed, "gone.css", true},
		{"@import", `<style>@import url(gone.css);</style><p>x`, []HTMLOption{WithResources(files)}, WarningResourceFailed, "gone.css", true},
		{"data URL that does not decode", `<img src="data:image/png;base64,%%%" alt=x>`, nil, WarningResourceFailed, "base64,%%%", true},
		{"CSS image", `<div style="background-image: url(gone.png); height: 10px"></div>`, []HTMLOption{WithResources(files)}, WarningPictureUnreadable, "gone.png", true},
		{"image of an SVG drawing", `<svg width=10 height=10><image href="gone.png" width=10 height=10 /></svg>`, []HTMLOption{WithResources(files)}, WarningPictureUnreadable, "gone.png", true},
		{"font whose file does not arrive", `<style>@font-face { font-family: F; src: url(gone.ttf) } p { font-family: F }</style><p>x`, []HTMLOption{WithResources(files)}, WarningFontNotLoaded, "gone.ttf", true},
		{"font no text uses", `<style>@font-face { font-family: F; src: url(gone.ttf) }</style><p>x`, []HTMLOption{WithResources(files)}, WarningFontNotLoaded, "gone.ttf", true},
		{"resource past the size cap", `<img src="big.png" alt=x>`, []HTMLOption{WithResources(big)}, WarningLimitReached, "big.png", true},
		{"picture that does not decode", `<img src="bad.png" alt=x>`, []HTMLOption{WithResources(files)}, WarningPictureUnreadable, "bad.png", false},
		{"picture format litho does not read", `<img src="a.ico" alt=x>`, []HTMLOption{WithResources(files)}, WarningPictureFormatUnsupported, "ICO", false},
		{"picture past the pixel cap", `<img src="a.png" alt=x>`, []HTMLOption{WithResources(files), WithConvertLimits(Limits{MaxImagePixels: 50})}, WarningLimitReached, "a.png", false},
		{"font program that does not parse", `<style>@font-face { font-family: F; src: url(bad.ttf) } p { font-family: F }</style><p>x`, []HTMLOption{WithResources(files)}, WarningFontNotLoaded, "bad.ttf", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			convert := func(required bool) (*ConvertReport, error) {
				opts := append([]HTMLOption{fonts, WithRequiredResources(required)}, tc.opts...)
				return FromHTML(strings.NewReader(tc.src), opts...).WriteTo(context.Background(), io.Discard)
			}
			rep, err := convert(false)
			if err != nil {
				t.Fatalf("without the option: %v", err)
			}
			if !requiredHasWarning(rep, tc.code, tc.about) {
				t.Errorf("without the option: no %s warning about %q in %+v", tc.code, tc.about, rep.Warnings)
			}
			rep, err = convert(true)
			if !tc.fails {
				if err != nil {
					t.Fatalf("a resource that arrived failed the conversion: %v", err)
				}
				if !requiredHasWarning(rep, tc.code, tc.about) {
					t.Errorf("with the option: no %s warning about %q in %+v", tc.code, tc.about, rep.Warnings)
				}
				return
			}
			if !errors.Is(err, ErrConvertResourceUnavailable) {
				t.Fatalf("got %v, want an error wrapping ErrConvertResourceUnavailable", err)
			}
			if !IsInvalidInput(err) {
				t.Errorf("IsInvalidInput(%v) is false", err)
			}
			if n := strings.Count(err.Error(), tc.about); n != 1 {
				t.Errorf("the error %q names %q %d times, want once", err, tc.about, n)
			}
		})
	}
}

// TestWithRequiredResources_FirstFailureEndsTheConversion checks that the
// conversion stops at the first resource that does not load, in document
// order, and that a font with a source that loads after one that does not
// is no failure (CSS Fonts 4, 4.3.1).
func TestWithRequiredResources_FirstFailureEndsTheConversion(t *testing.T) {
	t.Parallel()

	fonts := WithConvertFontDirs(convertFontDir(t))
	files := fstest.MapFS{"face.ttf": {Data: convertTestFont()}}
	src := `<style>@font-face { font-family: F; src: url(gone.ttf), url(face.ttf) } p { font-family: F }</style>
<p>x<img src="first.png" alt=a><img src="second.png" alt=b>`
	_, err := FromHTML(strings.NewReader(src), fonts, WithResources(files), WithRequiredResources(true)).WriteTo(context.Background(), io.Discard)
	if !errors.Is(err, ErrConvertResourceUnavailable) {
		t.Fatalf("got %v, want an error wrapping ErrConvertResourceUnavailable", err)
	}
	if !strings.Contains(err.Error(), "first.png") || strings.Contains(err.Error(), "second.png") || strings.Contains(err.Error(), "gone.ttf") {
		t.Errorf("the error %q names another resource than the first that failed", err)
	}
}

// TestWithRequiredResources_Markdown checks that a Markdown source takes
// the option as the HTML it renders to does.
func TestWithRequiredResources_Markdown(t *testing.T) {
	t.Parallel()

	fonts := WithConvertFontDirs(convertFontDir(t))
	_, err := FromMarkdown(strings.NewReader("# Title\n\n![a](gone.png)\n"), fonts, WithResources(fstest.MapFS{}), WithRequiredResources(true)).WriteTo(context.Background(), io.Discard)
	if !errors.Is(err, ErrConvertResourceUnavailable) || !strings.Contains(err.Error(), "gone.png") {
		t.Fatalf("got %v, want an error wrapping ErrConvertResourceUnavailable naming gone.png", err)
	}
}

// TestWithRequiredResources_OffWritesTheSameBytes checks that the option
// off is the default: a document whose resources all load writes the same
// bytes with it on, off or absent.
func TestWithRequiredResources_OffWritesTheSameBytes(t *testing.T) {
	t.Parallel()

	fonts := WithConvertFontDirs(convertFontDir(t))
	files := fstest.MapFS{"a.png": {Data: requiredPNG(t, 4, 4)}, "a.css": {Data: []byte("p { color: blue }")}}
	src := `<link rel=stylesheet href=a.css><p>x<img src=a.png alt=a>`
	date := WithDate(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	var outs [3]bytes.Buffer
	for i, opts := range [][]HTMLOption{nil, {WithRequiredResources(false)}, {WithRequiredResources(true)}} {
		opts = append([]HTMLOption{fonts, date, WithResources(files)}, opts...)
		if _, err := FromHTML(strings.NewReader(src), opts...).WriteTo(context.Background(), &outs[i]); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(outs[0].Bytes(), outs[1].Bytes()) || !bytes.Equal(outs[0].Bytes(), outs[2].Bytes()) {
		t.Error("the option changed the bytes of a document whose resources all load")
	}
}

// requiredHasWarning reports a warning of code whose message holds about.
func requiredHasWarning(rep *ConvertReport, code ConvertWarningCode, about string) bool {
	for _, w := range rep.Warnings {
		if w.Code == code && strings.Contains(w.Message, about) {
			return true
		}
	}
	return false
}

// requiredPNG returns a white PNG of w by h pixels.
func requiredPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 0xff
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// requiredBigFS serves one file of size zero bytes, read as it is asked
// for, so a test reaches the resource cap without holding the file.
type requiredBigFS struct {
	name string
	size int64
}

func (f requiredBigFS) Open(name string) (fs.File, error) {
	if name != f.name {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return &requiredBigFile{name: name, left: f.size}, nil
}

// requiredBigFile is the file of [requiredBigFS].
type requiredBigFile struct {
	name string
	left int64
}

func (f *requiredBigFile) Stat() (fs.FileInfo, error) { return fstest.MapFS{f.name: {}}.Stat(f.name) }
func (*requiredBigFile) Close() error                 { return nil }
func (f *requiredBigFile) Read(p []byte) (int, error) {
	if f.left <= 0 {
		return 0, io.EOF
	}
	n := min(int64(len(p)), f.left)
	clear(p[:n])
	f.left -= n
	return int(n), nil // fits int: at most len(p)
}
