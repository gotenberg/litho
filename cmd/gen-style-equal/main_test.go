package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// cssDir is the package the committed file belongs to.
var cssDir = filepath.Join("..", "..", "pkg", "internal", "css")

func TestCommittedFileIsCurrent(t *testing.T) {
	want, err := generate(cssDir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(cssDir, outName))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s is stale: run go generate ./pkg/internal/css/...", outName)
	}
}

func TestGenerateRefusesWhatItCannotRead(t *testing.T) {
	for name, src := range map[string]string{
		"unnamed struct":  "type Style struct{ A struct{ B int } }",
		"foreign struct":  "import \"strings\"\ntype Style struct{ A strings.Builder }",
		"unresolved type": "type Style struct{ A Missing }",
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "style.go"), []byte("package css\n"+src+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := generate(dir); !errors.Is(err, errUnreadable) && !errors.Is(err, errInvalidType) {
				t.Fatalf("err = %v, want a refusal", err)
			}
		})
	}
}

func TestGenerateReadsEveryKind(t *testing.T) {
	dir := t.TempDir()
	src := `package css

type Side uint8

type Inner struct{ F float32 }

type Style struct {
	B  bool
	I  int
	U  Side
	F  float64
	C  complex128
	S  string
	A  [2]Inner
	P  [3]uint8
	L  []string
	Q  *Inner
	M  map[string]int
	Fn func()
	X  any
}
`
	if err := os.WriteFile(filepath.Join(dir, "style.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	code, err := generate(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"func equalInner(a, b *Inner) bool",
		"math.Float32bits(a.F)",
		"math.Float64bits(a.F)",
		"math.Float64bits(real(a.C))",
		"if a.P != b.P {",
		"&a.L[0] != &b.L[0]",
		"if a.Q != b.Q {",
		"if a.M != nil || b.M != nil {",
		"h = mixString(h, x.S)",
		"h = mixBool(h, x.Q != nil)",
	} {
		if !bytes.Contains(code, []byte(want)) {
			t.Errorf("the generated code lacks %q", want)
		}
	}
}
