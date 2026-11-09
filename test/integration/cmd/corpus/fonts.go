package main

import (
	"archive/tar"
	"archive/zip"
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
	"os/exec"
	"path"
	"path/filepath"
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
}

// fontArchive is a release archive and the members it keeps.
type fontArchive struct {
	url, sha256, shaPin string
	// kind is "tar.gz", "tar.bz2", "zip" or "deb".
	kind string
	// members are the paths kept, each installed by its base name; empty
	// keeps every TrueType and OpenType face and LICENSE file anywhere in
	// the archive.
	members []string
	// licence is the member installed as LICENSE.
	licence string
}

// fontReleases lists the fixture fonts the converter features read, at the
// releases versions.env pins (see the comments there for what each draws).
func fontReleases(pins map[string]string) ([]fontRelease, error) {
	var missing []string
	pin := func(k string) string {
		v := pins[k]
		if v == "" {
			missing = append(missing, k)
		}
		return v
	}
	archive := func(dir, key, kind string, members []string, licence string) fontRelease {
		a := &fontArchive{url: pin(key + "_URL"), sha256: pin(key + "_SHA256"), shaPin: key + "_SHA256", kind: kind, members: members, licence: licence}
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
	carlito, caladea := pin("CARLITO_FONTS_VERSION"), pin("CALADEA_FONTS_VERSION")
	crosextra := func(family, version string) []string {
		prefix := "fonts-crosextra-" + strings.ToLower(family) + "-" + version + "/fonts/ttf/" + family
		return []string{prefix + "-Regular.ttf", prefix + "-Bold.ttf", prefix + "-Italic.ttf", prefix + "-BoldItalic.ttf"}
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
	notoCore := "./usr/share/fonts/truetype/noto/"
	out := []fontRelease{
		archive("liberation-"+pin("LIBERATION_FONTS_VERSION"), "LIBERATION_FONTS", "tar.gz", nil, ""),
		archive("dejavu-"+pin("DEJAVU_FONTS_VERSION"), "DEJAVU_FONTS", "tar.bz2", nil, ""),
		cjk,
		otc,
		weasy,
		archive("noto-sans-thai-"+pin("NOTO_SANS_THAI_VERSION"), "NOTO_SANS_THAI", "zip",
			[]string{"NotoSansThai/hinted/ttf/NotoSansThai-Regular.ttf", "NotoSansThai/hinted/ttf/NotoSansThai-Bold.ttf"}, "OFL.txt"),
		archive("noto-sans-devanagari-"+pin("NOTO_SANS_DEVANAGARI_VERSION"), "NOTO_SANS_DEVANAGARI", "zip",
			[]string{"NotoSansDevanagari/hinted/ttf/NotoSansDevanagari-Regular.ttf", "NotoSansDevanagari/hinted/ttf/NotoSansDevanagari-Bold.ttf"}, "OFL.txt"),
		archive("carlito-"+carlito, "CARLITO_FONTS", "tar.gz", crosextra("Carlito", carlito), "fonts-crosextra-carlito-"+carlito+"/OFL.txt"),
		archive("caladea-"+caladea, "CALADEA_FONTS", "tar.gz", crosextra("Caladea", caladea), "fonts-crosextra-caladea-"+caladea+"/OFL.txt"),
		archive("noto-core-"+pin("NOTO_SANS_CORE_VERSION"), "NOTO_SANS_CORE", "deb", []string{
			notoCore + "NotoSans-Regular.ttf", notoCore + "NotoSans-Bold.ttf", notoCore + "NotoSans-Italic.ttf", notoCore + "NotoSans-BoldItalic.ttf",
			notoCore + "NotoSerif-Regular.ttf", notoCore + "NotoSerif-Bold.ttf", notoCore + "NotoSerif-Italic.ttf", notoCore + "NotoSerif-BoldItalic.ttf",
		}, "./usr/share/doc/fonts-noto-core/copyright"),
		archive("opensymbol-"+pin("OPENSYMBOL_VERSION"), "OPENSYMBOL", "deb",
			[]string{"./usr/share/fonts/truetype/libreoffice/opens___.ttf"}, "./usr/share/doc/fonts-opensymbol/copyright"),
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("not pinned: %s", strings.Join(missing, ", "))
	}
	return out, nil
}

// fetchFonts installs every fixture font release under dir that is not
// there at its pinned release, the releases at once.
func fetchFonts(ctx context.Context, client *http.Client, pins map[string]string, dir string) error {
	releases, err := fontReleases(pins)
	if err != nil {
		return err
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
		if err := unpackFonts(ctx, a, data, files); err != nil {
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

// unpackFonts adds to files, by base name, the archive's members: the
// faces it keeps and its licence as LICENSE.
func unpackFonts(ctx context.Context, a *fontArchive, data []byte, files map[string][]byte) error {
	keep := func(name string) (string, bool) {
		if a.licence != "" && name == a.licence {
			return "LICENSE", true
		}
		if len(a.members) > 0 {
			for _, m := range a.members {
				if name == m {
					return path.Base(name), true
				}
			}
			return "", false
		}
		base := path.Base(name)
		ext := strings.ToLower(path.Ext(base))
		return base, ext == ".ttf" || ext == ".otf" || base == "LICENSE"
	}
	var err error
	switch a.kind {
	case "zip":
		err = unpackZip(data, keep, files)
	case "tar.gz":
		var r io.Reader
		if r, err = gzip.NewReader(bytes.NewReader(data)); err == nil {
			err = unpackTar(r, keep, files)
		}
	case "tar.bz2":
		err = unpackTar(bzip2.NewReader(bytes.NewReader(data)), keep, files)
	case "deb":
		err = unpackDeb(ctx, data, keep, files)
	default:
		err = fmt.Errorf("unknown archive kind %q", a.kind)
	}
	if err != nil {
		return err
	}
	want := len(a.members)
	if a.licence != "" {
		want++
	}
	if got := len(files); want > 0 && got < want {
		return fmt.Errorf("the archive holds %d of the %d members kept", got, want)
	}
	return nil
}

func unpackZip(data []byte, keep func(string) (string, bool), files map[string][]byte) error {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("open the zip archive: %w", err)
	}
	for _, f := range zr.File {
		name, ok := keep(f.Name)
		if !ok {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("open %s: %w", f.Name, err)
		}
		body, err := io.ReadAll(io.LimitReader(rc, maxFetchBytes))
		rc.Close()
		if err != nil {
			return fmt.Errorf("read %s: %w", f.Name, err)
		}
		files[name] = body
	}
	return nil
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

// unpackDeb reads a Debian package: an ar archive whose data.tar.xz holds
// the installed files. The standard library reads no xz, so the system's tar
// unpacks it, as the image does, its members kept.
func unpackDeb(ctx context.Context, data []byte, keep func(string) (string, bool), files map[string][]byte) error {
	payload, err := arMember(data, "data.tar.xz")
	if err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "litho-deb-*")
	if err != nil {
		return fmt.Errorf("create a staging directory: %w", err)
	}
	defer os.RemoveAll(tmp)
	cmd := exec.CommandContext(ctx, "tar", "-xJf", "-", "-C", tmp)
	cmd.Stdin = bytes.NewReader(payload)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("unpack data.tar.xz: %w: %s", err, out)
	}
	return filepath.WalkDir(tmp, func(p string, d os.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		rel, err := filepath.Rel(tmp, p)
		if err != nil {
			return err
		}
		name, ok := keep("./" + filepath.ToSlash(rel))
		if !ok {
			return nil
		}
		body, err := os.ReadFile(p) //nolint:gosec // a file this function unpacked
		if err != nil {
			return fmt.Errorf("read %s: %w", rel, err)
		}
		files[name] = body
		return nil
	})
}

// arMember returns the named member of an ar archive (the common format of
// a Debian package: a magic line, then a 60-byte header before each member).
func arMember(data []byte, name string) ([]byte, error) {
	const magic, header = "!<arch>\n", 60
	if !bytes.HasPrefix(data, []byte(magic)) {
		return nil, errors.New("not an ar archive")
	}
	for off := len(magic); off+header <= len(data); {
		h := data[off : off+header]
		var size int
		if _, err := fmt.Sscanf(strings.TrimSpace(string(h[48:58])), "%d", &size); err != nil || size < 0 || off+header+size > len(data) {
			return nil, errors.New("malformed ar header")
		}
		body := data[off+header : off+header+size]
		if strings.TrimRight(strings.TrimSpace(string(h[:16])), "/") == name {
			return body, nil
		}
		off += header + size + size%2
	}
	return nil, fmt.Errorf("the package holds no %s", name)
}
