package html

import "testing"

func TestDraw_UnderlineAuto(t *testing.T) {
	t.Parallel()
	// An auto underline, Chromium 9.0.0's whatever the face: floor(size /
	// 10) pixels thick, at least one, its top ceil(size / 20) pixels below
	// the baseline, at least one. The red bar of one pixel sits on the
	// baseline.
	tests := []struct {
		size       string
		top, thick int
	}{
		{"13px", 1, 1},
		{"16px", 1, 1},
		{"20px", 1, 2},
		{"27px", 2, 2},
		{"40px", 2, 4},
	}
	for _, tt := range tests {
		t.Run(tt.size, func(t *testing.T) {
			t.Parallel()
			src := `<body style="margin:0"><div style="line-height:1;white-space:nowrap;font-size:` + tt.size + `">` +
				`<b style="display:inline-block;width:4px;height:1px;background:red"></b>` +
				`<u style="color:transparent;text-decoration-color:red;text-decoration-skip-ink:none">XXXX</u></div>`
			rows := drawPage(t, src, 300, 60, Options{})
			col := func(x int) []int {
				var ys []int
				for y, r := range rows {
					if x < len(r) && r[x] == 'R' {
						ys = append(ys, y)
					}
				}
				return ys
			}
			bar, line := col(1), col(10)
			if len(bar) != 1 || len(line) == 0 {
				t.Fatalf("bar rows %v, underline rows %v", bar, line)
			}
			if top, thick := line[0]-bar[0]-1, len(line); top != tt.top || thick != tt.thick {
				t.Errorf("underline %d rows thick, its top %d below the baseline row; want %d and %d", thick, top, tt.thick, tt.top)
			}
		})
	}
}
