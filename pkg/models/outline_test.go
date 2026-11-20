package models

import "testing"

func TestBookmark_JSONRoundTrip(t *testing.T) {
	assertRoundTrip(t, Bookmark{
		Title:      "Chapter 1",
		PageNumber: 10,
		Children: []Bookmark{
			{
				Title:      "Section 1.1",
				PageNumber: 11,
				Children: []Bookmark{
					{Title: "Subsection", PageNumber: 12},
				},
			},
			{Title: "Section 1.2", PageNumber: 15},
		},
	})
}
