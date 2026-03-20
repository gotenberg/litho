//go:build integration

package integration

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// registeredCorpora lists all corpora that must be provisioned before
// integration tests run. Add new entries here when introducing a new corpus.
var registeredCorpora = []corpus{
	{
		Name:       "pdfjs",
		ContextDir: "corpus/pdfjs",
		OutputDir:  "testdata/pdf.js",
		ReadyFunc: func() bool {
			entries, err := os.ReadDir(filepath.Join(corpusDir, "references"))
			return err == nil && len(entries) > 0
		},
		Timeout: 20 * time.Minute,
	},
}

func TestMain(m *testing.M) {
	for i := range registeredCorpora {
		if err := registeredCorpora[i].provision(); err != nil {
			fmt.Fprintf(os.Stderr, "corpus setup failed: %v\n", err)
			os.Exit(1)
		}
	}
	os.Exit(m.Run())
}
