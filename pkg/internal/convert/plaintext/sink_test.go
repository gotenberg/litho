package plaintext_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/plaintext"
)

// collectSink keeps the pages handed over.
type collectSink struct{ pages []*compose.Page }

func (c *collectSink) Expect(int) {}

func (c *collectSink) Page(_ context.Context, p *compose.Page) error {
	c.pages = append(c.pages, p)
	return nil
}

func TestConvert_HandsEveryPageToTheSink(t *testing.T) {
	fonts := testFonts(t)
	src := "\f" + strings.Repeat("a line of text\n", 200) + "é\n\f\f"
	opts := plaintext.Options{MarginTop: 72, MarginBottom: 72, MarginLeft: 72, MarginRight: 72}
	whole, err := plaintext.Convert(context.Background(), strings.NewReader(src), fonts, opts)
	if err != nil {
		t.Fatal(err)
	}
	sink := &collectSink{}
	opts.Sink = sink
	streamed, err := plaintext.Convert(context.Background(), strings.NewReader(src), fonts, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(streamed.Pages) != 0 {
		t.Errorf("the document holds %d pages, want none", len(streamed.Pages))
	}
	if !reflect.DeepEqual(sink.pages, whole.Pages) {
		t.Errorf("the sink got %d pages, not the %d of the document", len(sink.pages), len(whole.Pages))
	}
	if !reflect.DeepEqual(streamed.Warnings, whole.Warnings) || len(whole.Warnings) == 0 {
		t.Errorf("warnings %+v, want %+v", streamed.Warnings, whole.Warnings)
	}
}
