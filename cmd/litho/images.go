package main

import (
	"context"
	"flag"
	"fmt"
	"io"
)

type imageInfo struct {
	Filename         string `json:"filename"`
	Width            int    `json:"width"`
	Height           int    `json:"height"`
	ColorSpace       string `json:"colorSpace"`
	BitsPerComponent int    `json:"bitsPerComponent"`
}

type imagePageResult struct {
	Page   int         `json:"page"`
	Images []imageInfo `json:"images"`
}

type imagesResult struct {
	File  string            `json:"file"`
	Pages []imagePageResult `json:"pages"`
}

func runImages(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("images", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("f", "text", "Output format: text or json")
	password := fs.String("p", "", "PDF password")
	pagesFlag := fs.String("pages", "", "Page range (e.g., 1-3,5,last)")

	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return exitError
	}

	files := fs.Args()
	if len(files) == 0 {
		fmt.Fprintln(stderr, "Usage: litho images [-f json] [-p password] [--pages RANGE] <file> [file...]")
		return exitError
	}

	var results []imagesResult
	for _, path := range files {
		r, closer, err := openPDF(ctx, path, *password)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", path, err)
			return exitError
		}

		pageCount, err := r.PageCount(ctx)
		if err != nil {
			closer()
			fmt.Fprintf(stderr, "%s: %v\n", path, err)
			return exitError
		}

		var pageNums []int
		if *pagesFlag != "" {
			pageNums, err = parsePages(*pagesFlag, pageCount)
			if err != nil {
				closer()
				fmt.Fprintf(stderr, "%s: %v\n", path, err)
				return exitError
			}
		} else {
			for i := 1; i <= pageCount; i++ {
				pageNums = append(pageNums, i)
			}
		}

		res := imagesResult{File: path}
		for _, p := range pageNums {
			metas, err := r.ExtractImages(ctx, p)
			if err != nil {
				fmt.Fprintf(stderr, "%s page %d: %v\n", path, p, err)
				metas = nil
			}
			var imgs []imageInfo
			for _, m := range metas {
				imgs = append(imgs, imageInfo{
					Filename:         m.Filename,
					Width:            m.Width,
					Height:           m.Height,
					ColorSpace:       m.ColorSpace,
					BitsPerComponent: m.BitsPerComponent,
				})
			}
			res.Pages = append(res.Pages, imagePageResult{Page: p, Images: imgs})
		}
		results = append(results, res)
		closer()
	}

	switch *format {
	case "json":
		if len(results) == 1 {
			return jsonOut(stdout, stderr, results[0])
		}
		return jsonOut(stdout, stderr, results)
	default:
		for _, res := range results {
			if len(results) > 1 {
				fmt.Fprintf(stdout, "--- %s ---\n", res.File)
			}
			for _, p := range res.Pages {
				if len(p.Images) == 0 {
					continue
				}
				fmt.Fprintf(stdout, "Page %d:\n", p.Page)
				for _, img := range p.Images {
					fmt.Fprintf(stdout, "  %s  %dx%d  %s  %d bpc\n", img.Filename, img.Width, img.Height, img.ColorSpace, img.BitsPerComponent)
				}
			}
		}
	}
	return exitOK
}
