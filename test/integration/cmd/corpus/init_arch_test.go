package main

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// imageArchitectures are the TARGETARCH values the corpus Dockerfile builds
// for: its JRE and pdfcpu steps each map exactly these two to a release
// archive and refuse any other.
var imageArchitectures = []string{"amd64", "arm64"}

// TestScanClaimsGOARCHFollowsTheImage pins that the scan-claims helper is
// built for the architecture the image runs, not for a fixed one.
//
// It was built for linux/amd64 whatever the image, on the reasoning that the
// verapdf/cli base image is amd64 only. The helper does not run in that
// image: it runs in the node image, which docker build makes native, so on an
// arm64 host every auto-mode provisioning ran it under emulation.
func TestScanClaimsGOARCHFollowsTheImage(t *testing.T) {
	t.Parallel()

	for _, arch := range imageArchitectures {
		got, err := scanClaimsGOARCH(arch)
		if err != nil {
			t.Errorf("scanClaimsGOARCH(%q): %v", arch, err)
			continue
		}
		if got != arch {
			t.Errorf("scanClaimsGOARCH(%q) = %q, want the image's own architecture", arch, got)
		}
	}
	// The Dockerfile refuses these, so no image of them can exist; a helper
	// built for one would be a guess.
	for _, arch := range []string{"", "arm", "386", "s390x"} {
		if got, err := scanClaimsGOARCH(arch); err == nil {
			t.Errorf("scanClaimsGOARCH(%q) = %q, want an error", arch, got)
		}
	}
}

// TestScanClaimsBuildTargetsLinux pins the rest of the helper's build
// environment: a static Linux binary, since the image carries no Go toolchain
// and no C library the helper could link against at run time.
func TestScanClaimsBuildTargetsLinux(t *testing.T) {
	t.Parallel()

	env := scanClaimsBuildEnv("arm64")
	for _, want := range []string{"GOOS=linux", "GOARCH=arm64", "CGO_ENABLED=0"} {
		if !slices.Contains(env, want) {
			t.Errorf("build environment lacks %s: %q", want, env)
		}
	}
	// os/exec keeps the last of duplicate keys, so a GOARCH inherited from
	// the caller's environment must come before the one set here.
	last := ""
	for _, kv := range env {
		if strings.HasPrefix(kv, "GOARCH=") {
			last = kv
		}
	}
	if last != "GOARCH=arm64" {
		t.Errorf("the last GOARCH entry is %q, so an inherited GOARCH would win", last)
	}
}

// TestDockerfileInstallsPdfcpuForTheImageArchitecture pins that the image
// installs the pdfcpu archive of its own architecture, each one checked
// against a hash versions.env pins.
//
// The Dockerfile named the x86_64 archive outright, so an arm64 image carried
// an amd64 pdfcpu that ran only under emulation, with nothing checking the
// archive it downloaded.
func TestDockerfileInstallsPdfcpuForTheImageArchitecture(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join(corpusSourceDir, "Dockerfile"))
	if err != nil {
		t.Fatalf("read Dockerfile: %v", err)
	}
	dockerfile := string(data)
	if !strings.Contains(dockerfile, "pdfcpu/releases/download") {
		t.Fatal("the Dockerfile downloads no pdfcpu release, so this test reads the wrong step")
	}
	if fixed := regexp.MustCompile(`pdfcpu_[^"/\s]*_Linux_(x86_64|arm64)`).FindString(dockerfile); fixed != "" {
		t.Errorf("the Dockerfile names the pdfcpu archive %q, a fixed architecture", fixed)
	}

	pins := make(map[string]string)
	for _, e := range readPinsFile(t) {
		pins[e.Key] = e.Value
	}
	hash := regexp.MustCompile(`^[0-9a-f]{64}$`)
	for _, arch := range imageArchitectures {
		key := "PDFCPU_SHA256_" + strings.ToUpper(arch)
		if !hash.MatchString(pins[key]) {
			t.Errorf("versions.env pins %s=%q, want the 64-digit SHA-256 of the %s archive", key, pins[key], arch)
		}
		if !strings.Contains(dockerfile, "${"+key+"}") {
			t.Errorf("the Dockerfile never expands %s, so the %s archive goes unchecked", key, arch)
		}
	}
}
