package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestManifestProfilesAreMapped pins that every profile a corpus manifest
// lists reaches each place a profile name is mapped: the veraPDF flavour of
// the remediation feature, the flavour table of the provisioning script that
// writes the golden named after it, a litho validator, and the remediation
// engine. A profile one of them misses scores silently: compliance calls every
// cell compliant, and remediation skips the profile with "no verapdf flavour".
// PDF/A-4e, 4f, 1a, 3a and 3u joined the litho manifest under this guard.
func TestManifestProfilesAreMapped(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..", "corpus")
	script, err := os.ReadFile(filepath.Join(root, "lib", "run-verapdf.sh"))
	if err != nil {
		t.Fatal(err)
	}
	manifests, err := filepath.Glob(filepath.Join(root, "*", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(manifests) == 0 {
		t.Fatal("found no corpus manifest")
	}

	checked := 0
	for _, path := range manifests {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var m Manifest
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		for _, profile := range m.Profiles {
			checked++
			flavour, ok := profileDirToVeraPDFFlavour(profile)
			if !ok {
				t.Errorf("%s: %s has no veraPDF flavour", m.Name, profile)
				continue
			}
			if want := `["` + profile + `"]="` + flavour + `"`; !strings.Contains(string(script), want) {
				t.Errorf("%s: run-verapdf.sh has no %s", m.Name, want)
			}
			_, isA := dirToPDFAProfile[profile]
			_, isUA := dirToPDFUAProfile[profile]
			_, isWT := dirToWTPDFProfile[profile]
			if !isA && !isUA && !isWT {
				t.Errorf("%s: %s maps to no litho validator", m.Name, profile)
			}
			if _, _, ok := newRemediationFeature(false).remediators(profile); !ok {
				t.Errorf("%s: %s has no remediation", m.Name, profile)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no manifest lists a profile")
	}
}
