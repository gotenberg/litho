package main

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"
)

// TestCorpusImageTag pins what names the provisioning image: the
// Dockerfile, the files of the context its COPY and ADD instructions read,
// and the build arguments; nothing else of the context, and no file another
// stage copies.
func TestCorpusImageTag(t *testing.T) {
	t.Parallel()

	const dockerfile = "FROM base AS one\nCOPY lib/*.mjs ./lib/\nCOPY --from=one /opt/x /opt/x\nADD --chmod=644 versions.env tools /opt/\n"
	write := func(dir, rel, content string) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	tree := func(edit func(dir string)) string {
		t.Helper()
		dir := t.TempDir()
		write(dir, "Dockerfile", dockerfile)
		write(dir, "lib/a.mjs", "a")
		write(dir, "lib/b.mjs", "b")
		write(dir, "lib/notes.txt", "not copied")
		write(dir, "versions.env", "A=1\n")
		write(dir, "tools/run.sh", "echo")
		write(dir, "other/manifest.json", "{}")
		if edit != nil {
			edit(dir)
		}
		return dir
	}
	tag := func(dir string, args ...string) string {
		t.Helper()
		got, err := corpusImageTag(dir, args)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	base := tag(tree(nil), "--build-arg", "A=1")
	if !regexp.MustCompile(`^[0-9a-f]{16}$`).MatchString(base) {
		t.Fatalf("tag %q, want 16 hexadecimal digits", base)
	}
	if again := tag(tree(nil), "--build-arg", "A=1"); again != base {
		t.Errorf("the same content named %q and %q", base, again)
	}
	for name, edit := range map[string]func(string){
		"a copied file":       func(d string) { write(d, "lib/a.mjs", "a2") },
		"a file a glob adds":  func(d string) { write(d, "lib/c.mjs", "c") },
		"a copied directory":  func(d string) { write(d, "tools/more.sh", "echo") },
		"the copied versions": func(d string) { write(d, "versions.env", "A=2\n") },
		"the Dockerfile":      func(d string) { write(d, "Dockerfile", dockerfile+"RUN true\n") },
	} {
		if got := tag(tree(edit), "--build-arg", "A=1"); got == base {
			t.Errorf("changing %s kept the tag %s", name, got)
		}
	}
	for name, edit := range map[string]func(string){
		"a file no instruction copies": func(d string) { write(d, "lib/notes.txt", "changed") },
		"another corpus's file":        func(d string) { write(d, "other/manifest.json", `{"a": 1}`) },
	} {
		if got := tag(tree(edit), "--build-arg", "A=1"); got != base {
			t.Errorf("changing %s moved the tag to %s", name, got)
		}
	}
	if got := tag(tree(nil), "--build-arg", "A=3"); got == base {
		t.Errorf("another build argument kept the tag %s", got)
	}
	missing := tree(func(d string) { write(d, "Dockerfile", "COPY absent.sh /x\n") })
	if _, err := corpusImageTag(missing, nil); err == nil {
		t.Error("a source that matches no file named an image")
	}
	srcs, err := dockerfileSources([]byte(dockerfile))
	if err != nil || !slices.Equal(srcs, []string{"lib/*.mjs", "versions.env", "tools"}) {
		t.Errorf("sources %q, %v", srcs, err)
	}
}

// TestCorpusImage_NamesThisTree checks the image the committed corpus
// directory names: the repository and a content tag.
func TestCorpusImage_NamesThisTree(t *testing.T) {
	t.Parallel()

	tag, err := corpusImageTag(corpusSourceDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tag) != corpusImageTagLength {
		t.Errorf("tag %q", tag)
	}
}
