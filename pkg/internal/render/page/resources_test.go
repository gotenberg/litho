package page

import "testing"

// TestPageNamesFallBack pins that a form whose /Resources lacks a name
// resolves it in the page's, as poppler and PDFium do, that a name it defines
// itself wins, and that a tiling pattern's cell resolves no name its own
// /Resources lacks, as neither renderer does.
func TestPageNamesFallBack(t *testing.T) {
	t.Parallel()
	redFill := stream("/Type /XObject /Subtype /Form /BBox [0 0 100 100]", "1 0 0 rg 0 0 100 100 re f")
	blueFill := stream("/Type /XObject /Subtype /Form /BBox [0 0 100 100]", "0 0 1 rg 0 0 100 100 re f")
	for _, tc := range []struct {
		name string
		f    fixture
		want rgb
	}{
		{"a form with empty /Resources", fixture{
			page:    "/Resources << /XObject << /Fm 5 0 R /Inner 6 0 R >> >>",
			content: "/Fm Do",
			objs:    []string{stream("/Type /XObject /Subtype /Form /BBox [0 0 100 100] /Resources << >>", "/Inner Do"), redFill},
		}, red},
		{"a form whose /XObject lacks the name", fixture{
			page:    "/Resources << /XObject << /Fm 5 0 R /Inner 6 0 R >> >>",
			content: "/Fm Do",
			objs: []string{
				stream("/Type /XObject /Subtype /Form /BBox [0 0 100 100] /Resources << /XObject << /Other 7 0 R >> >>", "/Inner Do"),
				redFill, blueFill,
			},
		}, red},
		{"a form's own name wins", fixture{
			page:    "/Resources << /XObject << /Fm 5 0 R /Inner 6 0 R >> >>",
			content: "/Fm Do",
			objs: []string{
				stream("/Type /XObject /Subtype /Form /BBox [0 0 100 100] /Resources << /XObject << /Inner 7 0 R >> >>", "/Inner Do"),
				redFill, blueFill,
			},
		}, blue},
		{"a tiling pattern keeps its empty /Resources", fixture{
			page:    "/Resources << /Pattern << /P 5 0 R >> /XObject << /Inner 6 0 R >> >>",
			content: "/Pattern cs /P scn 0 0 100 100 re f",
			objs: []string{
				tilePattern("/PaintType 1 /BBox [0 0 10 10] /XStep 10 /YStep 10 /Resources << >>", "/Inner Do"),
				redFill,
			},
		}, paper},
	} {
		img, _ := draw(t, tc.f)
		if got := at(t, img, 50, 50); !close8(got, tc.want, 1) {
			t.Errorf("%s: (50,50) = %v, want %v", tc.name, got, tc.want)
		}
	}
}
