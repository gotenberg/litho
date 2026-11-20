package models

import (
	"context"
	"io"
)

// Model3D represents a 3D model stream embedded in a PDF annotation.
// Content is accessed lazily: call Open to obtain an [io.ReadCloser] that
// decodes the stream through the PDF filter pipeline on read. The caller
// must close the returned reader.
// Concurrent reads of a value are safe; a write needs synchronization.
// Open may be called from several goroutines at once, and alongside
// other reads of the Reader that returned the value, while that Reader stays
// open.
type Model3D struct {
	// Name is the annotation's /NM, or model_N, N counting the page's models
	// from 0, when it has none.
	Name string `json:"name"`
	// Subtype is the model format, "U3D" or "PRC".
	Subtype string `json:"subtype"`
	// Open reads the model's decoded stream. Each call returns a new reader.
	Open func(ctx context.Context) (io.ReadCloser, error) `json:"-"`
}
