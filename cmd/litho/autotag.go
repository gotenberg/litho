package main

import (
	"context"
	"fmt"
	"io"

	"github.com/gotenberg/litho/pkg/pdf"
)

// autotagSkipNotice leads the line reporting why AutoTag added no structure
// tree.
const autotagSkipNotice = "warning: autotag added no structure tree"

func runAutotag(ctx context.Context, args []string, _ io.Writer, stderr io.Writer) int {
	var report *pdf.AutoTagReport
	return runSimpleMutation(ctx, args, stderr, "autotag",
		"Usage: litho autotag <file> -o out.pdf",
		func(w *pdf.Writer) { report = w.AutoTag() },
		withAfterWrite(func() { warnAutoTagSkipped(stderr, report) }))
}

// warnAutoTagSkipped reports on stderr why AutoTag left the document without a
// new structure tree, when it did.
//
// A skip fails nothing: [pdf.Writer.AutoTag] leaves an already-tagged document,
// one whose content is already marked or cannot be read, and one with nothing
// to tag as they were, and the output is written. Without this line the
// command exited 0 in silence, and a caller asking for a tagged document got an
// untagged one back with no word of it. The reason is the report's own, page
// number included, so the command and the pipe stage say the same thing.
//
// Call it only after the write returns: the Writer fills the report when it
// applies the edit, and until then Skipped is nil whatever AutoTag will decide.
func warnAutoTagSkipped(stderr io.Writer, report *pdf.AutoTagReport) {
	if report == nil || report.Skipped == nil {
		return
	}
	fmt.Fprintf(stderr, "%s: %v\n", autotagSkipNotice, report.Skipped)
}
