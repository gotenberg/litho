package detmath

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// exactPackages are the import patterns of the packages held to the rule:
// the public packages and every internal one, which conversions and PDF
// operations alike write their output and reach their verdicts with.
var exactPackages = []string{"./pkg/..."}

// fusingTargets are the targets the test compiles for: those litho's
// users build for on which Go fuses a product with the addition that uses
// it. level is the GOAMD64 level, which only amd64 reads: v3 is the first
// with the fused instructions.
var fusingTargets = []struct{ arch, level string }{
	{"arm64", "v1"}, {"amd64", "v3"}, {"ppc64le", "v1"}, {"s390x", "v1"},
}

// fusedSite matches the line the compiler prints for each multiply-add it
// fuses when built with -d=fmahash=vy: the positions of the operation,
// outermost inlining call first, then the hash.
var fusedSite = regexp.MustCompile(`^fmahash triggered (.+) [01]+$`)

// TestPackages_FuseNoMultiplyAdd compiles every package of
// [exactPackages] for each of [fusingTargets], on which Go fuses a product
// with the addition that uses it (The Go Programming Language
// Specification, "Arithmetic operators"), and fails on each fused site the
// compiler reports: a fused product rounds once where amd64 v1, 386 and
// arm round twice, and the PDF's bytes then depend on the machine. The
// targets do not fuse the same expressions: the conversion of a product to
// an unsigned integer is fused on ppc64le alone.
//
// A site is made exact by converting the product to its own type before
// the sum, which the specification says rounds: float64(a*b) + c. The
// product may sit in a variable, a field or the result of a function the
// compiler inlined; the report names the addition, and with an inlined
// function the call sites that led to it. A division by a power of two
// compiles to a product, and a doubling to an addition. Elementary
// functions come from this package, not from math
// ([TestPackages_TakeInexactFunctionsFromDetmath]).
//
// The test needs the go command, and skips without it or under -short.
func TestPackages_FuseNoMultiplyAdd(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles the packages once a target")
	}
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go command")
	}
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	var sites []string
	for _, target := range fusingTargets {
		args := append([]string{"build", "-o", os.DevNull, "-gcflags=github.com/gotenberg/litho/...=-d=fmahash=vy"}, exactPackages...)
		cmd := exec.CommandContext(t.Context(), goTool, args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH="+target.arch, "GOAMD64="+target.level, "CGO_ENABLED=0")
		var out bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &out
		if err := cmd.Run(); err != nil {
			t.Fatalf("go %s for %s: %v\n%s", strings.Join(args, " "), target.arch, err, out.String())
		}
		s := bufio.NewScanner(&out)
		for s.Scan() {
			if m := fusedSite.FindStringSubmatch(s.Text()); m != nil {
				sites = append(sites, target.arch+" "+target.level+": "+m[1])
			}
		}
	}
	slices.Sort(sites)
	sites = slices.Compact(sites)
	for _, site := range sites {
		t.Errorf("fused multiply-add at %s: round the product on its own, float64(a*b) + c", site)
	}
}
