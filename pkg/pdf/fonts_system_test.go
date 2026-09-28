package pdf

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

func TestSystemFontDirs(t *testing.T) {
	t.Parallel()
	env := func(vars map[string]string) func(string) string {
		return func(k string) string { return vars[k] }
	}
	for _, tc := range []struct {
		goos string
		vars map[string]string
		home string
		want []string
	}{
		{"darwin", nil, "/Users/u", []string{"/Users/u/Library/Fonts", "/Library/Fonts", "/System/Library/Fonts"}},
		{"darwin", nil, "", []string{"/Library/Fonts", "/System/Library/Fonts"}},
		{"linux", nil, "/home/u", []string{
			"/home/u/.local/share/fonts", "/home/u/.fonts", "/usr/local/share/fonts", "/usr/share/fonts",
		}},
		{"linux", map[string]string{"XDG_DATA_HOME": "/data/u", "XDG_DATA_DIRS": "/opt/share:relative:/usr/share"}, "/home/u", []string{
			"/data/u/fonts", "/home/u/.fonts", "/opt/share/fonts", "/usr/share/fonts", "/usr/local/share/fonts", "/usr/share/fonts",
		}},
		{"freebsd", map[string]string{"XDG_DATA_HOME": "relative"}, "", []string{"/usr/local/share/fonts", "/usr/share/fonts"}},
		{"windows", map[string]string{"LOCALAPPDATA": "C:/Users/u/AppData/Local", "WINDIR": "C:/Windows"}, "C:/Users/u", []string{
			filepath.Join("C:/Users/u/AppData/Local", "Microsoft", "Windows", "Fonts"), filepath.Join("C:/Windows", "Fonts"),
		}},
		{"windows", map[string]string{"SystemRoot": "D:/Win"}, "", []string{filepath.Join("D:/Win", "Fonts")}},
	} {
		got := systemFontDirs(tc.goos, env(tc.vars), tc.home)
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s %v home %q:\n got %q\nwant %q", tc.goos, tc.vars, tc.home, got, tc.want)
		}
	}
}

// TestWalkFontDirs pins the walk over the system's directories: it descends
// subdirectories and links, lists each file once however many links and roots
// reach it, survives a link loop and a dangling link, and skips what is not a
// font file.
func TestWalkFontDirs(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("symbolic links need privileges on Windows")
	}
	root := t.TempDir()
	other := t.TempDir()
	write := func(p string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	link := func(target, p string) {
		t.Helper()
		if err := os.Symlink(target, p); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(root, "a.ttf"))
	write(filepath.Join(root, "sub", "deep", "b.TTC"))
	write(filepath.Join(root, "notes.txt"))
	write(filepath.Join(other, "c.otf"))
	link(root, filepath.Join(root, "sub", "loop"))
	link(filepath.Join(root, "a.ttf"), filepath.Join(root, "sub", "alias.ttf"))
	link(filepath.Join(root, "missing.ttf"), filepath.Join(root, "dangling.ttf"))
	link(other, filepath.Join(root, "linked"))

	paths, err := walkFontDirs(context.Background(), slog.New(slog.DiscardHandler), []string{root, other, root, filepath.Join(root, "absent")})
	if err != nil {
		t.Fatal(err)
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	realOther, err := filepath.EvalSymlinks(other)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join(realRoot, "a.ttf"),
		filepath.Join(realOther, "c.otf"),
		filepath.Join(realRoot, "sub", "deep", "b.TTC"),
	}
	if !slices.Equal(paths, want) {
		t.Errorf("walk = %q, want %q", paths, want)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := walkFontDirs(cancelled, slog.New(slog.DiscardHandler), []string{root}); err == nil {
		t.Error("walk under a cancelled ctx: want an error")
	}
}

// TestWithSystemFonts_EmbedsFromTheDirectories remediates through the public
// option path over an injected directory, so the test depends on no font of
// the host: a face of a collection in a subdirectory serves Helvetica-Bold,
// and a file the process cannot read is skipped rather than failing the
// remediation.
func TestWithSystemFonts_EmbedsFromTheDirectories(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sub := filepath.Join(dir, "Supplemental")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	ttc := fontbuild.Collection(
		abProgram("LiberationSans", "Liberation Sans", "Regular", 667, 667),
		abProgram("LiberationSans-Bold", "Liberation Sans", "Bold", 722, 722),
	)
	if err := os.WriteFile(filepath.Join(sub, "LiberationSans.ttc"), ttc, 0o644); err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(dir, "Locked.ttf")
	if err := os.WriteFile(locked, abProgram("Locked", "Locked", "Regular", 722, 722), 0o000); err != nil {
		t.Fatal(err)
	}

	data := unembeddedFontsDoc("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold >>")
	report, after := remediateFonts(t, data, withSystemFontDirs([]string{dir}))
	if after != 0 {
		t.Fatalf("%d fonts stay unembedded: %v", after, report.FontRefusals)
	}
	if !strings.Contains(strings.Join(report.Fixed, "\n"), "embedded LiberationSans-Bold") {
		t.Errorf("want the Bold face embedded, got %v", report.Fixed)
	}

	report, after = remediateFonts(t, data, withSystemFontDirs([]string{filepath.Join(dir, "absent")}))
	if after != 1 || len(report.FontRefusals) != 1 {
		t.Errorf("no directory: %d unembedded, refusals %v, want the font refused", after, report.FontRefusals)
	}
}

// TestWithSystemFonts_ComesAfterTheCallersFonts pins the order of the
// providers: the caller's program serves the font it supplies one for, and
// the system's fonts the font it supplies none for.
func TestWithSystemFonts_ComesAfterTheCallersFonts(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for name, program := range map[string][]byte{
		"SystemSans-Bold.ttf": abProgram("SystemSans-Bold", "Helvetica", "Bold", 722, 722),
		"SystemTimes.ttf":     abProgram("SystemTimes", "Times", "Regular", 722, 667),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), program, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	callers := FontProviderFunc(func(_ context.Context, req FontRequest) ([]byte, error) {
		if req.BaseFont == "Helvetica-Bold" {
			return abProgram("CallerSans-Bold", "Caller Sans", "Bold", 722, 722), nil
		}
		return nil, nil
	})
	data := unembeddedFontsDoc(
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Times-Roman >>",
	)
	report, after := remediateFonts(t, data, withSystemFontDirs([]string{dir}), WithFontProvider(callers))
	if after != 0 {
		t.Fatalf("%d fonts stay unembedded: %v", after, report.FontRefusals)
	}
	fixed := strings.Join(report.Fixed, "\n")
	if !strings.Contains(fixed, "embedded CallerSans-Bold") || !strings.Contains(fixed, "embedded SystemTimes") || strings.Contains(fixed, "SystemSans-Bold") {
		t.Errorf("want Helvetica-Bold from the caller and Times-Roman from the system, got %v", report.Fixed)
	}
}

// TestWithSystemFonts_MintsItsOwnID pins that the derived trailer /ID records
// that the system's fonts were searched: the same file and a provider that
// supplies nothing mint another /ID with the option than without it.
func TestWithSystemFonts_MintsItsOwnID(t *testing.T) {
	t.Parallel()
	data := readLithoFixture(t, "filestruct_1b.pdf")
	ctx := context.Background()
	never := WithFontProvider(FontProviderFunc(func(context.Context, FontRequest) ([]byte, error) { return nil, nil }))
	id := func(opts ...RemediateOption) string {
		t.Helper()
		out, err := remediateFixture(ctx, data, "PDF/A-1b", opts...)
		if err != nil {
			t.Fatal(err)
		}
		r, err := Open(ctx, bytes.NewReader(out), int64(len(out)))
		if err != nil {
			t.Fatalf("reopening the output: %v", err)
		}
		defer r.Close()
		return fmt.Sprint(r.doc.Index.Trailer["ID"])
	}
	empty := t.TempDir()
	withProvider := id(never)
	withSystem := id(never, withSystemFontDirs([]string{empty}))
	if withProvider == withSystem {
		t.Errorf("with and without system fonts minted the same /ID %s", withProvider)
	}
	if again := id(never, withSystemFontDirs([]string{empty})); again != withSystem {
		t.Errorf("system fonts over the same directory minted %s, then %s", withSystem, again)
	}
}
