package main

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestUntarImageFonts_KeepsTheFontFilesUnderTheTop(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	add := func(name string, typ byte, body string) {
		t.Helper()
		h := &tar.Header{Name: name, Typeflag: typ, Mode: 0o644, Size: int64(len(body))}
		if typ == tar.TypeSymlink {
			h.Size, h.Linkname = 0, "../../etc/passwd"
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if typ == tar.TypeReg {
			if _, err := tw.Write([]byte(body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	add("fonts/", tar.TypeDir, "")
	add("fonts/truetype/dejavu/DejaVuSans.ttf", tar.TypeReg, "ttf")
	add("fonts/opentype/noto/NotoSansCJK-Regular.TTC", tar.TypeReg, "ttc")
	add("fonts/type1/urw-base35/NimbusSans-Regular.t1", tar.TypeReg, "t1")
	add("fonts/X11/encodings/large/big5.eten-0.enc.gz", tar.TypeReg, "enc")
	add("fonts/truetype/link.ttf", tar.TypeSymlink, "")
	add("fonts/../escape.ttf", tar.TypeReg, "out")
	add("other/stray.otf", tar.TypeReg, "out")
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	n, err := untarImageFonts(&buf, "fonts", dir)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("wrote %d files, want 2", n)
	}
	for rel, want := range map[string]string{
		"truetype/dejavu/DejaVuSans.ttf":        "ttf",
		"opentype/noto/NotoSansCJK-Regular.TTC": "ttc",
	} {
		got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil || string(got) != want {
			t.Errorf("%s: %q, %v", rel, got, err)
		}
	}
	var files []string
	if err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files = append(files, p)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Errorf("the directory holds %v", files)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "escape.ttf")); err == nil {
		t.Error("a member outside the top was written")
	}
}
