package heuristics

import (
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

func TestRequiresOCR_NoTextLargeImage(t *testing.T) {
	// Typical scanned page: 612x792 points (US Letter), no text,
	// one 2550x3300 pixel image (300 DPI scan of US Letter).
	images := []models.ImageMeta{
		{Width: 2550, Height: 3300, ColorSpace: "DeviceRGB"},
	}
	if !RequiresOCR(nil, images, 612, 792) {
		t.Error("expected RequiresOCR=true for scanned page")
	}
}

func TestRequiresOCR_HasText(t *testing.T) {
	elements := []models.TextElement{
		{Text: "Hello World", X: 72, Y: 720, FontSize: 12},
	}
	images := []models.ImageMeta{
		{Width: 2550, Height: 3300, ColorSpace: "DeviceRGB"},
	}
	if RequiresOCR(elements, images, 612, 792) {
		t.Error("expected RequiresOCR=false when text is present")
	}
}

func TestRequiresOCR_OnlyHiddenText(t *testing.T) {
	// Page has text but it's all hidden (e.g., invisible OCR layer already
	// present but the caller stripped it). No visible text → flag for OCR.
	elements := []models.TextElement{
		{Text: "ghost", Hidden: true},
	}
	images := []models.ImageMeta{
		{Width: 2550, Height: 3300, ColorSpace: "DeviceRGB"},
	}
	if !RequiresOCR(elements, images, 612, 792) {
		t.Error("expected RequiresOCR=true when all text is hidden")
	}
}

func TestRequiresOCR_SmallImage(t *testing.T) {
	// Page has no text, but the image is a tiny logo — not a scan.
	images := []models.ImageMeta{
		{Width: 100, Height: 50, ColorSpace: "DeviceRGB"},
	}
	if RequiresOCR(nil, images, 612, 792) {
		t.Error("expected RequiresOCR=false for small image")
	}
}

func TestRequiresOCR_NoImages(t *testing.T) {
	if RequiresOCR(nil, nil, 612, 792) {
		t.Error("expected RequiresOCR=false with no images")
	}
}

func TestRequiresOCR_ZeroPageDimensions(t *testing.T) {
	images := []models.ImageMeta{
		{Width: 2550, Height: 3300},
	}
	if RequiresOCR(nil, images, 0, 0) {
		t.Error("expected RequiresOCR=false for zero page dimensions")
	}
}

func TestRequiresOCR_MultipleSmallImagesLargeCoverage(t *testing.T) {
	// Multiple images that individually are small but collectively cover
	// the page (via the totalImageArea > pageArea*10 fallback).
	images := []models.ImageMeta{
		{Width: 1275, Height: 3300},
		{Width: 1275, Height: 3300},
	}
	if !RequiresOCR(nil, images, 612, 792) {
		t.Error("expected RequiresOCR=true for multiple large images")
	}
}

func TestRequiresOCR_A4Scan(t *testing.T) {
	// A4 page: 595x842 points, scanned at 200 DPI → ~1654x2339 pixels.
	images := []models.ImageMeta{
		{Width: 1654, Height: 2339, ColorSpace: "DeviceGray"},
	}
	if !RequiresOCR(nil, images, 595, 842) {
		t.Error("expected RequiresOCR=true for A4 scan at 200 DPI")
	}
}

func TestRequiresOCR_InvisibleText(t *testing.T) {
	// Invisible text should not count as real text.
	elements := []models.TextElement{
		{Text: "invisible OCR layer", Invisible: true},
	}
	images := []models.ImageMeta{
		{Width: 2550, Height: 3300},
	}
	if !RequiresOCR(elements, images, 612, 792) {
		t.Error("expected RequiresOCR=true when only invisible text exists")
	}
}
