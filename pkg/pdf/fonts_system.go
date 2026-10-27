package pdf

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// maxFontDirDepth bounds how deep the walk of the operating system's font
// directories descends. Anti-DoS guard, not spec-mandated: it protects the
// walk's stack from a directory tree deep by construction, and font
// directories nest two or three levels (/usr/share/fonts/truetype/dejavu).
const maxFontDirDepth = 32

// WithSystemFonts embeds, for the fonts a document shows without embedding
// them, programs read from the operating system's font directories.
//
// The directories searched are:
//
//   - macOS: ~/Library/Fonts, /Library/Fonts and /System/Library/Fonts.
//   - Windows: the per-user fonts directory
//     (%LOCALAPPDATA%\Microsoft\Windows\Fonts) and %WINDIR%\Fonts.
//   - Elsewhere: $XDG_DATA_HOME/fonts (~/.local/share/fonts), ~/.fonts, the
//     fonts directory of each of $XDG_DATA_DIRS, /usr/local/share/fonts and
//     /usr/share/fonts.
//
// Each directory is searched recursively, a symbolic link followed once to
// where it leads. litho reads the files itself, in pure Go, without fontconfig
// or any other system library.
//
// The option is [WithFontFiles] over those directories, with:
//
//   - The same file formats, TrueType and OpenType Collections included.
//   - The same matching by PostScript name, family and style, and
//     metric-compatible family.
//   - The same bounds.
//   - Every check of [WithFontProvider] before a program is embedded, each font
//     left unembedded listed in [RemediationReport.FontRefusals].
//
// The directories are indexed once, at the first font a remediation asks for,
// and the index is kept for every remediation the option serves. A file the
// process cannot read is skipped, and logged at Debug. [WithFontProvider] and
// [WithFontFiles] are asked first: the system's fonts serve the fonts they
// supply no program for, or none a remediation accepts.
//
// The output then depends on the fonts installed where litho runs, which
// another host, or an update of the same one, changes. The option is off by
// default for that reason, and the trailer /ID remediation derives
// ([WithDocumentID]) records that it was set. The licence to embed a system's
// font is the caller's.
func WithSystemFonts() RemediateOption {
	home, err := os.UserHomeDir()
	if err != nil {
		// No home directory leaves the per-user directories out; the
		// system-wide ones still serve.
		home = ""
	}
	return withSystemFontDirs(systemFontDirs(runtime.GOOS, os.Getenv, home))
}

// withSystemFontDirs is [WithSystemFonts] over dirs, which tests set.
func withSystemFontDirs(dirs []string) RemediateOption {
	p := newSystemFonts(dirs)
	return func(o *remediateOpts) {
		o.systemFonts = p
	}
}

// newSystemFonts returns the provider of [WithSystemFonts] over dirs.
func newSystemFonts(dirs []string) *fontFiles {
	return &fontFiles{
		list: func(ctx context.Context, logger *slog.Logger) ([]string, error) {
			return walkFontDirs(ctx, logger, dirs)
		},
		open: func(name string) (fs.File, error) {
			return os.Open(name)
		},
		lenient: true,
	}
}

// systemFontDirs returns the font directories of the operating system goos
// names, per-user ones first, as the systems themselves let a user's font
// take precedence over a system-wide one of the same name. getenv reads the
// environment and home is the user's home directory, empty when unknown.
func systemFontDirs(goos string, getenv func(string) string, home string) []string {
	var dirs []string
	add := func(elem ...string) {
		if elem[0] == "" {
			return
		}
		dirs = append(dirs, filepath.Join(elem...))
	}
	switch goos {
	case "darwin", "ios":
		add(home, "Library", "Fonts")
		add("/Library/Fonts")
		add("/System/Library/Fonts")
	case "windows":
		add(getenv("LOCALAPPDATA"), "Microsoft", "Windows", "Fonts")
		windir := getenv("WINDIR")
		if windir == "" {
			windir = getenv("SystemRoot")
		}
		add(windir, "Fonts")
	default:
		// The XDG Base Directory Specification defaults $XDG_DATA_HOME to
		// ~/.local/share and $XDG_DATA_DIRS to /usr/local/share:/usr/share.
		if data := getenv("XDG_DATA_HOME"); filepath.IsAbs(data) {
			add(data, "fonts")
		} else if home != "" {
			add(home, ".local", "share", "fonts")
		}
		add(home, ".fonts")
		for data := range strings.SplitSeq(getenv("XDG_DATA_DIRS"), ":") {
			if filepath.IsAbs(data) {
				add(data, "fonts")
			}
		}
		add("/usr/local/share/fonts")
		add("/usr/share/fonts")
	}
	return dirs
}

// walkFontDirs lists the font files under roots, in root order and then in
// path order, within [maxFontWalkEntries] and [maxFontDirDepth]. A symbolic
// link is followed to where it leads, and each directory and file is visited
// once, by its resolved path, so a link loop or two roots naming one
// directory walk it once. A directory or link it cannot read is skipped, and
// logged at Debug.
func walkFontDirs(ctx context.Context, logger *slog.Logger, roots []string) ([]string, error) {
	w := fontDirWalk{logger: logger, dirs: map[string]bool{}, files: map[string]bool{}}
	for _, root := range roots {
		real, err := filepath.EvalSymlinks(root)
		if err != nil {
			logger.Debug("system fonts: skipping a directory", slog.String("dir", root), slog.Any("err", err))
			continue
		}
		if err := w.walk(ctx, real, 0); err != nil {
			return nil, err
		}
		if w.full {
			break
		}
	}
	return w.paths, nil
}

// fontDirWalk is the state of one [walkFontDirs].
type fontDirWalk struct {
	logger  *slog.Logger
	dirs    map[string]bool
	files   map[string]bool
	entries int
	// full reports that the walk visited [maxFontWalkEntries] entries.
	full  bool
	paths []string
}

// walk lists the font files under dir, a resolved path, depth levels below
// its root.
func (w *fontDirWalk) walk(ctx context.Context, dir string, depth int) error {
	if depth > maxFontDirDepth || w.dirs[dir] {
		return nil
	}
	w.dirs[dir] = true
	entries, err := os.ReadDir(dir)
	if err != nil {
		w.logger.Debug("system fonts: skipping a directory", slog.String("dir", dir), slog.Any("err", err))
		return nil
	}
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("walking the system font directories: %w", err)
		}
		if w.entries >= maxFontWalkEntries {
			if !w.full {
				w.full = true
				w.logger.Debug("system fonts: walk bound reached, ignoring the entries left", slog.Int("max", maxFontWalkEntries))
			}
			return nil
		}
		w.entries++
		p := filepath.Join(dir, e.Name())
		mode := e.Type()
		if mode&fs.ModeSymlink != 0 {
			real, err := filepath.EvalSymlinks(p)
			if err != nil {
				w.logger.Debug("system fonts: skipping a link", slog.String("path", p), slog.Any("err", err))
				continue
			}
			info, err := os.Stat(real)
			if err != nil {
				w.logger.Debug("system fonts: skipping a link", slog.String("path", p), slog.Any("err", err))
				continue
			}
			p, mode = real, info.Mode().Type()
		}
		switch {
		case mode.IsDir():
			if err := w.walk(ctx, p, depth+1); err != nil {
				return err
			}
		case mode.IsRegular() && (isFontFileName(e.Name()) || isFontFileName(p)) && !w.files[p]:
			w.files[p] = true
			w.paths = append(w.paths, p)
		}
	}
	return nil
}
