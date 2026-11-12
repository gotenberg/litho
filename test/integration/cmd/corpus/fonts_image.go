package main

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/gotenberg/litho/test/integration/fixture"
)

// imageFontsPath is the directory the Gotenberg image installs its fonts
// under, the one its fontconfig (/etc/fonts/fonts.conf) reads.
const imageFontsPath = "/usr/share/fonts"

// imageFontExtensions are the font files copied out of the image: the
// formats litho indexes. The image's Type 1 copies of the URW base 35 fonts
// (.pfb, .afm, .t1) duplicate its OpenType ones, and litho reads none.
var imageFontExtensions = []string{".ttf", ".otf", ".ttc", ".otc"}

// fetchImageFonts copies the font files of the image GOTENBERG_IMAGE names
// into dir/[fixture.ImageFontsDir], unless the directory records that
// image's digest. A pin that moves to another image, or a tag pushed again,
// replaces the directory whole.
func fetchImageFonts(ctx context.Context, pins map[string]string, dir string) error {
	image := pins["GOTENBERG_IMAGE"]
	if image == "" {
		return errors.New("GOTENBERG_IMAGE is not pinned")
	}
	ref, err := imageDigest(ctx, image)
	if err != nil {
		return err
	}
	stamp := ref + " " + imageFontsPath + " " + strings.Join(imageFontExtensions, ",")
	target := filepath.Join(dir, fixture.ImageFontsDir)
	if got, err := os.ReadFile(filepath.Join(target, fontPinnedFile)); err == nil && string(got) == stamp { //nolint:gosec // the fonts directory's own record
		fmt.Printf("  fonts: %s already holds the fonts of %s\n", fixture.ImageFontsDir, ref)
		return nil
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	tmp, err := os.MkdirTemp(dir, ".install-*")
	if err != nil {
		return fmt.Errorf("create a staging directory: %w", err)
	}
	defer os.RemoveAll(tmp)
	faces, err := copyImageFonts(ctx, ref, tmp)
	if err != nil {
		return fmt.Errorf("%s: %w", ref, err)
	}
	if faces == 0 {
		return fmt.Errorf("%s holds no font file under %s", ref, imageFontsPath)
	}
	if err := os.WriteFile(filepath.Join(tmp, fontPinnedFile), []byte(stamp), 0o600); err != nil {
		return fmt.Errorf("%s: %w", fixture.ImageFontsDir, err)
	}
	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("%s: remove the previous copy: %w", fixture.ImageFontsDir, err)
	}
	if err := os.Rename(tmp, target); err != nil {
		return fmt.Errorf("%s: %w", fixture.ImageFontsDir, err)
	}
	fmt.Printf("  fonts: %s holds the %d font files of %s\n", fixture.ImageFontsDir, faces, ref)
	return nil
}

// imageDigest returns image pinned to the digest it resolves to,
// repository@sha256:..., pulling the image when the host lacks it. The
// digest is the one the registry lists the tag under, the same on every
// architecture for a multi-platform image.
func imageDigest(ctx context.Context, image string) (string, error) {
	repo := image
	if i := strings.LastIndex(image, ":"); i > strings.LastIndex(image, "/") {
		repo = image[:i]
	}
	inspect := func() (string, error) {
		out, err := exec.CommandContext(ctx, "docker", "image", "inspect", "--format", "{{range .RepoDigests}}{{println .}}{{end}}", image).Output() //nolint:gosec // the pinned image
		if err != nil {
			return "", err
		}
		for line := range strings.Lines(string(out)) {
			if d := strings.TrimSpace(line); strings.HasPrefix(d, repo+"@sha256:") {
				return d, nil
			}
		}
		return "", fmt.Errorf("%s lists no digest of %s", image, repo)
	}
	ref, err := inspect()
	if err == nil {
		return ref, nil
	}
	if out, err := exec.CommandContext(ctx, "docker", "pull", image).CombinedOutput(); err != nil { //nolint:gosec // the pinned image
		return "", fmt.Errorf("pull %s: %w: %s", image, err, out)
	}
	return inspect()
}

// copyImageFonts copies the font files under imageFontsPath in the image
// ref into dir, at their paths under it (truetype/dejavu/DejaVuSans.ttf),
// and returns how many it copied. It reads them from a container it creates
// and never starts.
func copyImageFonts(ctx context.Context, ref, dir string) (int, error) {
	out, err := exec.CommandContext(ctx, "docker", "create", ref).Output() //nolint:gosec // the pinned image
	if err != nil {
		return 0, fmt.Errorf("create a container: %w", err)
	}
	id := strings.TrimSpace(string(out))
	defer exec.Command("docker", "rm", "-f", id).Run()                          //nolint:errcheck,gosec // the container this function created; a failure leaves a stopped container and changes no output
	cmd := exec.CommandContext(ctx, "docker", "cp", id+":"+imageFontsPath, "-") //nolint:gosec // the container this function created
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return 0, err
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("docker cp: %w", err)
	}
	n, err := untarImageFonts(stdout, path.Base(imageFontsPath), dir)
	if err != nil {
		_ = cmd.Process.Kill() //nolint:errcheck // the copy already failed; Wait below reaps the process
		_ = cmd.Wait()         //nolint:errcheck // the copy already failed with the error returned
		return 0, err
	}
	if err := cmd.Wait(); err != nil {
		return 0, fmt.Errorf("docker cp: %w: %s", err, stderr.String())
	}
	return n, nil
}

// untarImageFonts writes the font files of the tar stream r, whose members
// sit under top, into dir at their paths under top, and returns how many it
// wrote. A member whose path leaves top, a link and a file of another kind
// are skipped.
func untarImageFonts(r io.Reader, top, dir string) (int, error) {
	tr := tar.NewReader(r)
	written := 0
	for n := 0; ; n++ {
		if n > maxFontFiles {
			return 0, fmt.Errorf("more than %d archive members", maxFontFiles)
		}
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return written, nil
		}
		if err != nil {
			return 0, fmt.Errorf("read the font tree: %w", err)
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		rel, ok := strings.CutPrefix(path.Clean(h.Name), top+"/")
		if !ok || !filepath.IsLocal(rel) || !isImageFontFile(path.Base(rel)) {
			continue
		}
		if h.Size > maxFetchBytes {
			return 0, fmt.Errorf("%s: %d bytes, over %d", rel, h.Size, maxFetchBytes)
		}
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			return 0, err
		}
		f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) //nolint:gosec // a local path under the staging directory
		if err != nil {
			return 0, err
		}
		_, err = io.Copy(f, io.LimitReader(tr, maxFetchBytes))
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return 0, fmt.Errorf("write %s: %w", rel, err)
		}
		written++
	}
}

// isImageFontFile reports whether name is a font file copied out of the
// image, one of [imageFontExtensions].
func isImageFontFile(name string) bool {
	return slices.Contains(imageFontExtensions, strings.ToLower(path.Ext(name)))
}
