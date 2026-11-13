package main

import (
	"archive/tar"
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// fontPinnedFile records, in a font family's directory, the release it holds:
// its URLs and hashes. A directory whose record matches the pins is left
// alone.
const fontPinnedFile = ".pinned"

// maxFontFiles bounds the files one font archive may unpack. Anti-DoS guard,
// not a property of any release: the largest pinned archive holds a few
// hundred files.
const maxFontFiles = 10000

// fontDownload is one file of a release, fetched whole: a font or a licence.
// An empty sha256 pin leaves the file unchecked (a licence the release does
// not hash).
type fontDownload struct {
	url, shaPin, name string
}

// fontRelease is one fixture font family at its pinned release: the
// directory it installs under the fonts directory, what it is fetched from,
// and how the faces and the licence come out of it.
type fontRelease struct {
	dir   string
	stamp string
	files []fontDownload
	// archive, when set, is fetched and unpacked by unpack instead.
	archive *fontArchive
	// plainAgent sends Go's default User-Agent in place of
	// [fetchUserAgent]: the release's host needs no client name.
	plainAgent bool
}

// fontArchive is a release archive, of which every TrueType and OpenType
// face and LICENSE file is kept, each installed by its base name.
type fontArchive struct {
	url, sha256, shaPin string
	// kind is "tar.gz" or "tar.bz2".
	kind string
}

// fontReleases lists the font releases versions.env pins (see the comments
// there for what each draws): the faces the litho/text builder converts
// with, the WeasyPrint test font, and the variable families a converter
// fixture installs by name (installedFonts in its options.json). The
// converter features draw with the Gotenberg image's fonts otherwise (see
// [fetchImageFonts]).
func fontReleases(pins map[string]string) ([]fontRelease, error) {
	var missing []string
	pin := func(k string) string {
		v := pins[k]
		if v == "" {
			missing = append(missing, k)
		}
		return v
	}
	archive := func(dir, key, kind string) fontRelease {
		a := &fontArchive{url: pin(key + "_URL"), sha256: pin(key + "_SHA256"), shaPin: key + "_SHA256", kind: kind}
		return fontRelease{dir: dir, stamp: a.url + " " + a.sha256, archive: a}
	}
	noto := pin("NOTO_CJK_SANS_VERSION")
	base := "https://github.com/notofonts/noto-cjk/raw/Sans" + noto
	cjk := fontRelease{dir: "noto-cjk-sans-" + noto, stamp: base}
	for _, region := range []string{"JP", "KR", "SC", "TC"} {
		key := "NOTO_SANS_" + region + "_SHA256"
		cjk.stamp += " " + pin(key)
		file := "NotoSans" + region + "-Regular.otf"
		cjk.files = append(cjk.files, fontDownload{url: base + "/Sans/SubsetOTF/" + region + "/" + file, shaPin: key, name: file})
	}
	cjk.files = append(cjk.files, fontDownload{url: base + "/LICENSE", name: "LICENSE"})
	otcURL := base + "/Sans/OTC/NotoSansCJK-Regular.ttc"
	otc := fontRelease{
		dir:   "noto-cjk-sans-otc-" + noto,
		stamp: otcURL + " " + pin("NOTO_SANS_CJK_OTC_SHA256"),
		files: []fontDownload{
			{url: otcURL, shaPin: "NOTO_SANS_CJK_OTC_SHA256", name: "NotoSansCJK-Regular.ttc"},
			{url: base + "/LICENSE", name: "LICENSE"},
		},
	}
	// The weasyprint corpus's test font, read from the release tag its
	// fixtures come from; the directory keeps no version, the draw feature
	// reads it by name.
	weasyURL := "https://raw.githubusercontent.com/Kozea/WeasyPrint/v" + pin("WEASYPRINT_VERSION") + "/tests/resources/weasyprint.otf"
	weasy := fontRelease{
		dir:   "weasyprint",
		stamp: weasyURL + " " + pin("WEASYPRINT_FONT_SHA256"),
		files: []fontDownload{{url: weasyURL, shaPin: "WEASYPRINT_FONT_SHA256", name: "weasyprint.otf"}},
	}
	// The variable families, each read from its repository at a pinned
	// commit, the licence hash-checked with the faces. The directories keep
	// no version: an options.json names them.
	roboto := githubRaw + "google/fonts/" + pin("ROBOTO_FLEX_COMMIT") + "/ofl/robotoflex/"
	sourceSans := githubRaw + "adobe-fonts/source-sans/" + pin("SOURCE_SANS_3_COMMIT") + "/"
	variable := []fontRelease{
		pinnedFiles("roboto-flex",
			fontDownload{roboto + "RobotoFlex%5BGRAD,XOPQ,XTRA,YOPQ,YTAS,YTDE,YTFI,YTLC,YTUC,opsz,slnt,wdth,wght%5D.ttf", "ROBOTO_FLEX_SHA256", "RobotoFlex.ttf"},
			fontDownload{roboto + "OFL.txt", "ROBOTO_FLEX_LICENSE_SHA256", "OFL.txt"}),
		pinnedFiles("source-sans-3",
			fontDownload{sourceSans + "VF/SourceSans3VF-Upright.otf", "SOURCE_SANS_3_SHA256", "SourceSans3VF-Upright.otf"},
			fontDownload{sourceSans + "LICENSE.md", "SOURCE_SANS_3_LICENSE_SHA256", "LICENSE.md"}),
	}
	for i := range variable {
		for _, f := range variable[i].files {
			variable[i].stamp += " " + pin(f.shaPin)
		}
	}
	out := slices.Concat([]fontRelease{
		archive("liberation-"+pin("LIBERATION_FONTS_VERSION"), "LIBERATION_FONTS", "tar.gz"),
		archive("dejavu-"+pin("DEJAVU_FONTS_VERSION"), "DEJAVU_FONTS", "tar.bz2"),
		cjk,
		otc,
		weasy,
	}, variable)
	if len(missing) > 0 {
		return nil, fmt.Errorf("not pinned: %s", strings.Join(missing, ", "))
	}
	return out, nil
}

// pinnedFiles is a release of whole files, each hash-checked, fetched with
// Go's default User-Agent; its stamp starts with the URLs, the caller adds
// the hashes.
func pinnedFiles(dir string, files ...fontDownload) fontRelease {
	r := fontRelease{dir: dir, files: files, plainAgent: true}
	for i, f := range files {
		if i > 0 {
			r.stamp += " "
		}
		r.stamp += f.url
	}
	return r
}

// fetchFonts installs every fixture font release under dir that is not
// there at its pinned release, the releases at once; only, when set, names
// the release directories to install, the others left alone.
func fetchFonts(ctx context.Context, client *http.Client, pins map[string]string, dir string, only []string) error {
	releases, err := fontReleases(pins)
	if err != nil {
		return err
	}
	if len(only) > 0 {
		var known []string
		for _, r := range releases {
			known = append(known, r.dir)
		}
		for _, name := range only {
			if !slices.Contains(known, name) {
				return fmt.Errorf("no font release %q: want one of %s", name, strings.Join(known, ", "))
			}
		}
		releases = slices.DeleteFunc(releases, func(r fontRelease) bool { return !slices.Contains(only, r.dir) })
	}
	errs := make([]error, len(releases))
	var wg sync.WaitGroup
	for i, r := range releases {
		wg.Go(func() {
			errs[i] = installFontRelease(ctx, client, pins, dir, r)
		})
	}
	wg.Wait()
	return errors.Join(errs...)
}

// installFontRelease installs one release unless its directory records it.
func installFontRelease(ctx context.Context, client *http.Client, pins map[string]string, dir string, r fontRelease) error {
	target := filepath.Join(dir, r.dir)
	if got, err := os.ReadFile(filepath.Join(target, fontPinnedFile)); err == nil && string(got) == r.stamp { //nolint:gosec // the fonts directory's own record
		fmt.Printf("  fonts: %s already at the pinned release\n", r.dir)
		return nil
	}
	if r.plainAgent {
		base := client.Transport
		if base == nil {
			base = http.DefaultTransport
		}
		client = &http.Client{Transport: plainAgentTransport{base}, Timeout: client.Timeout}
	}
	files := make(map[string][]byte)
	for _, f := range r.files {
		data, err := fetchVerified(ctx, client, f.url, pins[f.shaPin], f.shaPin)
		if err != nil {
			return fmt.Errorf("%s: %w", r.dir, err)
		}
		files[f.name] = data
	}
	if a := r.archive; a != nil {
		data, err := fetchVerified(ctx, client, a.url, a.sha256, a.shaPin)
		if err != nil {
			return fmt.Errorf("%s: %w", r.dir, err)
		}
		if err := unpackFonts(a, data, files); err != nil {
			return fmt.Errorf("%s: %w", r.dir, err)
		}
	}
	tmp, err := os.MkdirTemp(dir, ".install-*")
	if err != nil {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
		if tmp, err = os.MkdirTemp(dir, ".install-*"); err != nil {
			return fmt.Errorf("create a staging directory: %w", err)
		}
	}
	defer os.RemoveAll(tmp)
	faces := 0
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(tmp, name), data, 0o600); err != nil {
			return fmt.Errorf("%s: write %s: %w", r.dir, name, err)
		}
		if ext := strings.ToLower(path.Ext(name)); ext == ".ttf" || ext == ".otf" || ext == ".ttc" {
			faces++
		}
	}
	if err := os.WriteFile(filepath.Join(tmp, fontPinnedFile), []byte(r.stamp), 0o600); err != nil {
		return fmt.Errorf("%s: %w", r.dir, err)
	}
	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("%s: remove the previous release: %w", r.dir, err)
	}
	if err := os.Rename(tmp, target); err != nil {
		return fmt.Errorf("%s: %w", r.dir, err)
	}
	fmt.Printf("  fonts: %s installed (%d faces)\n", r.dir, faces)
	return nil
}

// plainAgentTransport sends each request without the User-Agent its caller
// set, so the client sends Go's default, Go-http-client/1.1.
type plainAgentTransport struct {
	base http.RoundTripper
}

func (t plainAgentTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Del("User-Agent")
	return t.base.RoundTrip(req)
}

// fetchVerified downloads u and checks it against the hash the pin key
// holds; an empty want leaves it unchecked.
func fetchVerified(ctx context.Context, client *http.Client, u, want, key string) ([]byte, error) {
	var data []byte
	var err error
	for range fetchRetries {
		if data, err = httpGet(ctx, client, u); err == nil || errors.Is(err, errPermanent) || ctx.Err() != nil {
			break
		}
	}
	if err != nil {
		return nil, err
	}
	if want == "" {
		return data, nil
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != want {
		return nil, fmt.Errorf("%w: %s hashes to %s, %s pins %s", errHashMismatch, path.Base(u), got, key, want)
	}
	return data, nil
}

// unpackFonts adds to files, by base name, the archive's faces and licence.
func unpackFonts(a *fontArchive, data []byte, files map[string][]byte) error {
	keep := func(name string) (string, bool) {
		base := path.Base(name)
		ext := strings.ToLower(path.Ext(base))
		return base, ext == ".ttf" || ext == ".otf" || base == "LICENSE"
	}
	switch a.kind {
	case "tar.gz":
		r, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return err
		}
		return unpackTar(r, keep, files)
	case "tar.bz2":
		return unpackTar(bzip2.NewReader(bytes.NewReader(data)), keep, files)
	}
	return fmt.Errorf("unknown archive kind %q", a.kind)
}

func unpackTar(r io.Reader, keep func(string) (string, bool), files map[string][]byte) error {
	tr := tar.NewReader(r)
	for n := 0; ; n++ {
		if n > maxFontFiles {
			return fmt.Errorf("more than %d archive members", maxFontFiles)
		}
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read the tar archive: %w", err)
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		name, ok := keep(h.Name)
		if !ok {
			continue
		}
		body, err := io.ReadAll(io.LimitReader(tr, maxFetchBytes))
		if err != nil {
			return fmt.Errorf("read %s: %w", h.Name, err)
		}
		files[name] = body
	}
}
