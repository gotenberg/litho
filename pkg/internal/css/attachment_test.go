package css

import "testing"

func TestBackgroundAttachment_Parse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		decls string
		want  Attachment
	}{
		{"", AttachScroll},
		{"background-attachment: local", AttachLocal},
		{"background-attachment: fixed, scroll", AttachFixed},
		{"background-attachment: nope", AttachScroll},
		{"background: url(a.png) local", AttachLocal},
		{"background-attachment: fixed; background: red", AttachScroll},
	}
	for _, tt := range tests {
		t.Run(tt.decls, func(t *testing.T) {
			t.Parallel()
			if got := styleOf(t, tt.decls).BackgroundAttachment; got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
