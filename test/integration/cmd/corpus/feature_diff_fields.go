package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// scoreFields is the share of the widgets of the reference's interactive
// form, push buttons left out, that litho's output holds too: a widget of
// the same kind on the same page, its centre within [fieldPlaceSlack]
// points of the reference widget's rectangle, holding the same value (see
// [fieldRecall]). The diff cell records it only for a reference that holds
// a form, so the scores of every other fixture are what they were.
const scoreFields = "field_recall"

// fieldPlaceSlack is how far, in points, the centre of litho's widget may lie
// outside the reference widget's rectangle and still sit in its place: 3
// points. LibreOffice insets an OpenDocument control's widget about 2
// points inside the control's frame, which litho's widget fills, and sets a
// text control's widget on the text's line box, about a point taller than
// litho's, which spans the face's ascent and descent.
const fieldPlaceSlack = 3

// The field flags [diffWidget.kind] reads (ISO 32000-1, Tables 226 and
// 230).
const (
	fieldFlagRadio      = 1 << 15
	fieldFlagPushbutton = 1 << 16
	fieldFlagCombo      = 1 << 17
)

// diffWidget is one widget of an interactive form, as qpdf reads it: its
// field's kind and value, its page and its rectangle.
type diffWidget struct {
	Kind  string
	Value string
	Page  int
	Rect  [4]float64
}

// qpdfWidget is one entry of qpdf --json's "acroform" fields, which lists
// a field once per widget.
type qpdfWidget struct {
	FieldType  string          `json:"fieldtype"`
	FieldFlags int             `json:"fieldflags"`
	Value      json.RawMessage `json:"value"`
	Page       int             `json:"pageposfrom1"`
	Annotation struct {
		Object          string `json:"object"`
		AppearanceState string `json:"appearancestate"`
	} `json:"annotation"`
}

// maxDiffWidgets bounds the widgets one PDF's form is read for. Anti-DoS
// guard, not spec-mandated: a page holds tens of form controls, and a
// conversion's pages are capped at maxDiffPages.
const maxDiffWidgets = 100_000

// readWidgets returns the widgets of the interactive form of data, read
// with qpdf at qpdf, nil for a file without one. qpdf exits 3 when it read
// the file with warnings, and still writes the JSON.
func readWidgets(ctx context.Context, qpdf string, data []byte) ([]diffWidget, error) {
	if !bytes.Contains(data, []byte("/AcroForm")) {
		return nil, nil
	}
	tmp, err := os.CreateTemp("", "litho-diff-fields-*.pdf")
	if err != nil {
		return nil, fmt.Errorf("qpdf staging file: %w", err)
	}
	path := tmp.Name()
	defer os.Remove(path)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return nil, fmt.Errorf("writing the qpdf staging file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("closing the qpdf staging file: %w", err)
	}
	cmd := exec.CommandContext(ctx, qpdf, "--json=2", "--json-key=acroform", "--json-key=qpdf", "--json-stream-data=none", path) //nolint:gosec // qpdf from LookPath, path is our staging file
	stdout, runErr := cmd.Output()
	var exitErr *exec.ExitError
	if runErr != nil && (!errors.As(runErr, &exitErr) || exitErr.ExitCode() != 3) {
		return nil, fmt.Errorf("running qpdf --json: %w", runErr)
	}
	var doc struct {
		AcroForm struct {
			Fields []qpdfWidget `json:"fields"`
		} `json:"acroform"`
		QPDF []json.RawMessage `json:"qpdf"`
	}
	if err := json.Unmarshal(stdout, &doc); err != nil {
		return nil, fmt.Errorf("reading qpdf --json: %w", err)
	}
	if len(doc.QPDF) < 2 {
		return nil, errors.New("qpdf --json lists no objects")
	}
	var objects map[string]struct {
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(doc.QPDF[1], &objects); err != nil {
		return nil, fmt.Errorf("reading qpdf --json objects: %w", err)
	}
	var out []diffWidget
	for _, f := range doc.AcroForm.Fields {
		if len(out) >= maxDiffWidgets {
			break
		}
		w := diffWidget{Kind: f.kind(), Value: f.value(), Page: f.Page}
		if w.Kind == "" {
			continue
		}
		var annot struct {
			Rect []float64 `json:"/Rect"`
		}
		if json.Unmarshal(objects["obj:"+f.Annotation.Object].Value, &annot) == nil && len(annot.Rect) == 4 {
			rect := annot.Rect
			w.Rect = [4]float64{min(rect[0], rect[2]), min(rect[1], rect[3]), max(rect[0], rect[2]), max(rect[1], rect[3])}
		}
		out = append(out, w)
	}
	return out, nil
}

// kind returns the kind of the field: text, checkbox, radio, combo or
// list; "" for a push button, which holds no value, or a signature.
func (f qpdfWidget) kind() string {
	switch f.FieldType {
	case "/Tx":
		return "text"
	case "/Btn":
		switch {
		case f.FieldFlags&fieldFlagPushbutton != 0:
			return ""
		case f.FieldFlags&fieldFlagRadio != 0:
			return "radio"
		}
		return "checkbox"
	case "/Ch":
		if f.FieldFlags&fieldFlagCombo != 0 {
			return "combo"
		}
		return "list"
	}
	return ""
}

// value returns the field's value as text: a text string without qpdf's
// "u:" prefix, its line ends written "\n" and its edges trimmed; a name
// without its slash; the items of a list that selects several one a line.
// A check box reads "on" or "Off" alone, since engines name the on state
// as they like, and a radio button its widget's own state, on or off.
func (f qpdfWidget) value() string {
	var v any
	if json.Unmarshal(f.Value, &v) != nil {
		return ""
	}
	text := func(x any) string {
		s, _ := x.(string)
		s = strings.TrimPrefix(s, "u:")
		s = strings.TrimPrefix(s, "/")
		s = strings.ReplaceAll(s, "\r\n", "\n")
		return strings.TrimSpace(strings.ReplaceAll(s, "\r", "\n"))
	}
	var s string
	switch x := v.(type) {
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			parts[i] = text(e)
		}
		s = strings.Join(parts, "\n")
	default:
		s = text(x)
	}
	switch f.kind() {
	case "checkbox":
		if s == "" || s == "Off" {
			return "Off"
		}
		return "on"
	case "radio":
		if st := strings.TrimPrefix(f.Annotation.AppearanceState, "/"); st != "" && st != "Off" {
			return "on"
		}
		return "Off"
	}
	return s
}

// fieldRecall is the share of the reference's widgets litho's widgets
// match, each matched once: the same kind, page and value, the centre of
// litho's within [fieldPlaceSlack] of the reference's rectangle. 1 when
// the reference has none.
func fieldRecall(ref, got []diffWidget) float64 {
	if len(ref) == 0 {
		return 1
	}
	used := make([]bool, len(got))
	found := 0
	for _, r := range ref {
		for i, g := range got {
			if used[i] || g.Kind != r.Kind || g.Page != r.Page || g.Value != r.Value {
				continue
			}
			cx, cy := (g.Rect[0]+g.Rect[2])/2, (g.Rect[1]+g.Rect[3])/2
			if cx < r.Rect[0]-fieldPlaceSlack || cx > r.Rect[2]+fieldPlaceSlack || cy < r.Rect[1]-fieldPlaceSlack || cy > r.Rect[3]+fieldPlaceSlack {
				continue
			}
			used[i] = true
			found++
			break
		}
	}
	return float64(found) / float64(len(ref))
}
