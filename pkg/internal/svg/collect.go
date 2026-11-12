package svg

import (
	"context"
	"sync"

	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// Collector gathers the warnings of the drawings of one conversion. A
// drawing records warnings when it is parsed, which draws it once at its
// default size, and again each time [Document.Draw] lays it out at a size
// it has not drawn: a layout draws a picture after the reader that parsed
// it is done. The converter adds each drawing it parses and takes the
// warnings once every page is laid out, so that none is lost; it may take
// them earlier too, to place what parsing found among its own warnings,
// and a later take returns only what the drawings added since. Each
// warning is reported once per code, effect and message, its message
// prefixed with the label of its drawing. The zero Collector is ready to
// use, and a nil one collects nothing. It is safe for concurrent use.
type Collector struct {
	mu       sync.Mutex
	drawings []collected
	seen     map[string]bool
}

// collected is one drawing a Collector reads: its label, and how many of
// its warnings were taken.
type collected struct {
	d     *Document
	label string
	taken int
}

// Add records the drawing d, whose warnings are reported with label
// before their message.
func (c *Collector) Add(d *Document, label string) {
	if c == nil || d == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.drawings = append(c.drawings, collected{d: d, label: label})
}

// Take returns the warnings the drawings added since the last Take, in
// the order the drawings were added, each drawing's in the order it met
// them.
func (c *Collector) Take() []typeset.Warning {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []typeset.Warning
	for i := range c.drawings {
		k := &c.drawings[i]
		ws := k.d.Warnings()
		for _, w := range ws[min(k.taken, len(ws)):] {
			w.Message = k.label + w.Message
			key := w.Code.String() + "\x00" + w.Effect.String() + "\x00" + w.Message
			if c.seen[key] {
				continue
			}
			if c.seen == nil {
				c.seen = map[string]bool{}
			}
			c.seen[key] = true
			out = append(out, w)
		}
		k.taken = len(ws)
	}
	return out
}

// PictureLabel returns the label of the warnings of an SVG picture of an
// Office document, the package part or path name holds it at, "" for a
// picture the document embeds without a name.
func PictureLabel(name string) string {
	if name == "" {
		return "in an embedded SVG picture: "
	}
	return "in the SVG picture " + name + ": "
}

// collectorKey addresses the Collector [WithCollector] installs on a
// context.
type collectorKey struct{}

// WithCollector returns a context that carries a new Collector, and the
// Collector: the readers of a conversion add the drawings they parse to it
// ([Collect]), and the converter takes their warnings once every page is
// laid out.
func WithCollector(ctx context.Context) (context.Context, *Collector) {
	c := &Collector{}
	return context.WithValue(ctx, collectorKey{}, c), c
}

// Collect adds the drawing d to the Collector ctx carries, with label
// before each of its warnings, and does nothing when ctx carries none.
func Collect(ctx context.Context, d *Document, label string) {
	c, _ := ctx.Value(collectorKey{}).(*Collector) // a type assertion, nil when absent
	c.Add(d, label)
}
