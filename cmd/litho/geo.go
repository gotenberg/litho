package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/gotenberg/litho/pkg/models"
)

type geoPageResult struct {
	Page    int              `json:"page"`
	GeoData []models.GeoData `json:"geoData"`
}

type geoResult struct {
	File  string          `json:"file"`
	Pages []geoPageResult `json:"pages"`
}

func runGeo(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("geo", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("f", "text", "Output format: text or json")
	password := fs.String("p", "", "PDF password")
	pagesFlag := fs.String("pages", "", "Page range (e.g., 1-3,5,last)")
	geojson := fs.Bool("geojson", false, "Emit GeoJSON (text mode) via GeoData.ToGeoJSON()")

	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return exitError
	}

	files := fs.Args()
	if len(files) == 0 {
		fmt.Fprintln(stderr, "Usage: litho geo [-f json] [-p password] [--pages RANGE] [--geojson] <file> [file...]")
		return exitError
	}

	var results []geoResult
	for _, path := range files {
		r, closer, pageNums, err := resolvePages(ctx, path, *password, *pagesFlag, stderr)
		if err != nil {
			return exitError
		}

		res := geoResult{File: path}
		for _, p := range pageNums {
			geo, err := r.ExtractGeoData(ctx, p)
			if err != nil {
				fmt.Fprintf(stderr, "%s page %d: %v\n", path, p, err)
				continue
			}
			if len(geo) == 0 {
				continue
			}
			res.Pages = append(res.Pages, geoPageResult{Page: p, GeoData: geo})
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
			for _, page := range res.Pages {
				for _, g := range page.GeoData {
					if *geojson {
						fmt.Fprintln(stdout, g.ToGeoJSON())
						continue
					}
					fmt.Fprintf(stdout, "Page %d:\n", page.Page)
					fmt.Fprintf(stdout, "  Coordinate system: %s\n", g.CoordinateSystem)
					if g.EPSG != "" {
						fmt.Fprintf(stdout, "  EPSG:              %s\n", g.EPSG)
					}
					fmt.Fprintf(stdout, "  Tie points:        %d\n", len(g.TiePoints))
					fmt.Fprintf(stdout, "  Bounds:            %v\n", g.Bounds)
				}
			}
		}
	}
	return exitOK
}
