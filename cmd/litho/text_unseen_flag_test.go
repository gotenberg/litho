package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestText_UnseenFlagListsTextThePageHides pins the CLI surface of
// [pdf.Reader.ExtractUnseenText]: -unseen lists, with its reason, the text a
// page draws that shows nowhere, and leaves the text it shows out. The
// fixture's clipped word paints nothing, its clip removing it.
func TestText_UnseenFlagListsTextThePageHides(t *testing.T) {
	path := writeTempPDF(t, clipPathTextPDF())

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{"text", "-unseen", path}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("exit %d, stderr: %s", rc, stderr.String())
	}
	got := stdout.String()
	if !strings.Contains(got, "page 1: paints-nothing: CLIPAWAY") {
		t.Errorf("-unseen = %q, want the clipped word as painting nothing", got)
	}
	if strings.Contains(got, "PAINTED") {
		t.Errorf("-unseen listed the painted word: %q", got)
	}

	stdout.Reset()
	if rc := run(context.Background(), []string{"text", "-unseen", "-f", "json", path}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("exit %d, stderr: %s", rc, stderr.String())
	}
	var res textResult
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		t.Fatalf("json: %v: %s", err, stdout.String())
	}
	if len(res.Pages) != 1 || len(res.Pages[0].Unseen) != 1 || res.Pages[0].Unseen[0].Text != "CLIPAWAY" {
		t.Errorf("json pages = %+v, want one unseen run CLIPAWAY", res.Pages)
	}
}
