package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/gotenberg/litho/pkg/pdf"
)

// cmykProfileUsage is the help of a --cmyk-profile flag that sets how a
// command reads DeviceCMYK.
const cmykProfileUsage = "ICC profile DeviceCMYK is read in when the document names none (default: built-in SWOP profile)"

// cmykProfileOptions returns the open options a --cmyk-profile value asks
// for: none when path is empty, else [pdf.WithDefaultCMYKProfile] over the
// file's bytes. [pdf.Open] refuses a file that is no CMYK profile.
func cmykProfileOptions(path string) ([]pdf.OpenOption, error) {
	if path == "" {
		return nil, nil
	}
	profile, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("reading the CMYK profile: %w", err)
	}
	return []pdf.OpenOption{pdf.WithDefaultCMYKProfile(profile)}, nil
}
