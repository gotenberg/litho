package html

import (
	"strings"
	"testing"
)

// TestDraw_CalcSize locks CSS Sizing 4, 7.1: a calc-size() size is its
// calculation of the used size of its basis, an auto width stretching in
// normal flow and shrinking to fit elsewhere, an auto height being the
// content's, an automatic minimum size the content's where it has one and
// 0 elsewhere; and the box's intrinsic contributions are the calculation
// of its basis's.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 (gotenberg/gotenberg:9.4.1) draws, in a
//     400px parent, width calc-size(auto, size / 2) 200 wide, a float of
//     calc-size(auto, size + 30px) around 50px of content 80 wide, height
//     calc-size(auto, size * 2) over 15px of content 30 tall, an empty
//     block of min-height calc-size(auto, size + 40px) 40 tall, and
//     calc-size(stretch, size / 2) in a 100px parent 50 tall.
func TestDraw_CalcSize(t *testing.T) {
	t.Parallel()
	const body = `<body style="margin:0">`
	tests := []struct {
		name, src, want string
	}{
		{
			"auto width stretches", `<div style="width:calc-size(auto, size / 2);height:2px;background:red"></div>`,
			`RRRRR_____
			 RRRRR_____
			 __________`,
		},
		{
			"auto width of a float shrinks to fit", `<div style="float:left;width:calc-size(auto, size + 2px);height:2px;background:red">` +
				`<span style="display:inline-block;width:3px"></span></div>`,
			`RRRRR_____
			 RRRRR_____
			 __________`,
		},
		{
			"auto height is the content's", `<div style="width:2px;height:calc-size(auto, size * 2);background:red"><div style="height:1px"></div></div>`,
			`RR________
			 RR________
			 __________`,
		},
		{
			"automatic minimum of a block is 0", `<div style="width:2px;min-height:calc-size(auto, size + 2px);background:red"></div>`,
			`RR________
			 RR________
			 __________`,
		},
		{
			"automatic minimum of a ratio box is its content", `<div style="width:4px;aspect-ratio:2/1;min-height:calc-size(auto, size - 3px);background:red">` +
				`<div style="height:6px"></div></div>`,
			`RRRR______
			 RRRR______
			 RRRR______
			 __________`,
		},
		{
			"intrinsic contribution", `<div style="width:0"><div style="width:fit-content;height:1px;background:red">` +
				`<div style="width:calc-size(auto, size + 4px)"><span style="display:inline-block;width:2px"></span> ` +
				`<span style="display:inline-block;width:2px"></span></div></div></div>`,
			`RRRRRR____
			 __________`,
		},
		{
			"automatic minimum of a flex item", `<div style="display:flex;flex-direction:column;height:0">` +
				`<div style="width:2px;height:4px;min-height:calc-size(auto, size + 2px);background:red"><div style="height:1px"></div></div></div>`,
			`RR________
			 RR________
			 RR________
			 __________`,
		},
		{
			"column flex container flexes at the calculated height", `<div style="display:flex;flex-direction:column;flex-wrap:wrap;width:4px;height:calc-size(auto, min(size, 4px));background:red">` +
				`<div style="height:3px"></div><div style="height:3px"></div></div>`,
			`RRRR______
			 RRRR______
			 RRRR______
			 RRRR______
			 __________`,
		},
		{
			"column flex container calculates its height once", `<div style="display:flex;flex-direction:column;width:3px;height:calc-size(auto, size * 2);background:red">` +
				`<div style="height:1px"></div></div>`,
			`RRR_______
			 RRR_______
			 __________`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := drawPage(t, body+tt.src, 10, 8, Options{})
			var want []string
			for l := range strings.SplitSeq(tt.want, "\n") {
				want = append(want, strings.TrimSpace(l))
			}
			if g := strings.Join(got[:len(want)], "\n"); g != strings.Join(want, "\n") {
				t.Errorf("pixels:\n%s\nwant:\n%s", g, strings.Join(want, "\n"))
			}
		})
	}
}
