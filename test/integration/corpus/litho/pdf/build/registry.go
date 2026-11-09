// Package build writes the litho/pdf corpus fixtures. Each fixture has one
// builder that returns the bytes of ../pdfs/<name>.pdf: `corpus build
// --corpus=litho/pdf` writes them, and TestBuildersMatchCommitted holds the
// committed files to their builders byte for byte.
package build

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

var builders = map[string]func() ([]byte, error){}

// register adds a fixture builder under its fixture name, the PDF's base
// name without .pdf.
func register(name string, fn func() ([]byte, error)) {
	if _, dup := builders[name]; dup {
		panic("build: duplicate builder " + name)
	}
	builders[name] = fn
}

// Names returns the registered fixture names, sorted.
func Names() []string {
	names := make([]string, 0, len(builders))
	for name := range builders {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Build returns the bytes of the named fixture. A builder that panics, as
// pdfgen's constructors do on a malformed call, returns the panic as an
// error.
func Build(name string) (out []byte, err error) {
	fn, ok := builders[name]
	if !ok {
		return nil, fmt.Errorf("build: no builder named %q", name)
	}
	defer func() {
		if r := recover(); r != nil {
			out, err = nil, fmt.Errorf("build %s: panic: %v", name, r)
		}
	}()
	out, err = fn()
	if err != nil {
		return nil, fmt.Errorf("build %s: %w", name, err)
	}
	return out, nil
}

// repoFile reads a file of the repository by its slash path from the module
// root, the nearest directory up from the working directory that holds
// go.mod, so a builder reads it alike under go test and under corpus build.
func repoFile(rel string) ([]byte, error) {
	dir, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel))) //nolint:gosec // a repository file a builder names
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, errors.New("build: no go.mod above the working directory")
		}
		dir = parent
	}
}
