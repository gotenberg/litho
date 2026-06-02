package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/gotenberg/litho/pkg/models"
)

type model3dPageResult struct {
	Page   int              `json:"page"`
	Models []models.Model3D `json:"models"`
}

type model3dResult struct {
	File  string              `json:"file"`
	Pages []model3dPageResult `json:"pages"`
}

func runModel3d(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("model3d", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("f", "text", "Output format: text or json")
	password := fs.String("p", "", "PDF password")
	pagesFlag := fs.String("pages", "", "Page range (e.g., 1-3,5,last)")

	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return exitError
	}

	files := fs.Args()
	if len(files) == 0 {
		fmt.Fprintln(stderr, "Usage: litho model3d [-f json] [-p password] [--pages RANGE] <file> [file...]")
		return exitError
	}

	var results []model3dResult
	for _, path := range files {
		r, closer, pageNums, err := resolvePages(ctx, path, *password, *pagesFlag, stderr)
		if err != nil {
			return exitError
		}

		res := model3dResult{File: path}
		for _, p := range pageNums {
			models3d, err := r.Extract3DModels(ctx, p)
			if err != nil {
				fmt.Fprintf(stderr, "%s page %d: %v\n", path, p, err)
				continue
			}
			if len(models3d) == 0 {
				continue
			}
			res.Pages = append(res.Pages, model3dPageResult{Page: p, Models: models3d})
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
			for _, pg := range res.Pages {
				for _, m := range pg.Models {
					fmt.Fprintf(stdout, "%s (%s)\n", m.Name, m.Subtype)
				}
			}
		}
	}
	return exitOK
}
