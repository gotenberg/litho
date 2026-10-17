package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/gotenberg/litho/pkg/models"
)

type permsInfo struct {
	Print             bool `json:"print"`
	ModifyContent     bool `json:"modifyContent"`
	ExtractText       bool `json:"extractText"`
	ModifyAnnotations bool `json:"modifyAnnotations"`
	FillForms         bool `json:"fillForms"`
	Assemble          bool `json:"assemble"`
	PrintHighQuality  bool `json:"printHighQuality"`
}

type infoResult struct {
	File               string                    `json:"file"`
	Version            string                    `json:"version"`
	Pages              int                       `json:"pages"`
	Encrypted          bool                      `json:"encrypted"`
	Linearized         bool                      `json:"linearized"`
	HasForms           bool                      `json:"has_forms"`
	IsPortfolio        bool                      `json:"is_portfolio"`
	UndecodableStreams []models.StreamDiagnostic `json:"undecodableStreams,omitempty"`
	SalvagedStreams    []models.StreamDiagnostic `json:"salvagedStreams,omitempty"`
	Permissions        *permsInfo                `json:"permissions,omitempty"`
}

func runInfo(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("info", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("f", "text", "Output format: text or json")
	password := fs.String("p", "", "PDF password")
	checkStreams := fs.Bool("streams", false, "Report streams litho cannot decode (unknown filters), and those whose damaged data it salvages")
	checkPerms := fs.Bool("permissions", false, "Show the document permission flags")

	if code, ok := parseCommandFlags(fs, args); !ok {
		return code
	}

	files := fs.Args()
	if len(files) == 0 {
		fmt.Fprintln(stderr, "Usage: litho info [-f json] [-p password] <file> [file...]")
		return exitError
	}

	var results []infoResult
	for _, path := range files {
		r, closer, err := openPDF(ctx, path, *password)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", path, err)
			return exitError
		}

		pageCount, _ := r.PageCount(ctx)
		hasForms, _ := r.HasFormFields(ctx)
		isPortfolio, _ := r.IsPortfolio(ctx)

		res := infoResult{
			File:        path,
			Version:     r.Version(),
			Pages:       pageCount,
			Encrypted:   r.IsEncrypted(),
			Linearized:  r.IsLinearized(),
			HasForms:    hasForms,
			IsPortfolio: isPortfolio,
		}
		if *checkStreams {
			res.UndecodableStreams, _ = r.UndecodableStreams(ctx)
			res.SalvagedStreams, _ = r.SalvagedStreams(ctx)
		}
		if *checkPerms {
			p := r.Permissions()
			res.Permissions = &permsInfo{
				Print:             p.Print,
				ModifyContent:     p.ModifyContent,
				ExtractText:       p.ExtractText,
				ModifyAnnotations: p.ModifyAnnotations,
				FillForms:         p.FillForms,
				Assemble:          p.Assemble,
				PrintHighQuality:  p.PrintHighQuality,
			}
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
			fmt.Fprintf(stdout, "Version:     %s\n", res.Version)
			fmt.Fprintf(stdout, "Pages:       %d\n", res.Pages)
			fmt.Fprintf(stdout, "Encrypted:   %t\n", res.Encrypted)
			fmt.Fprintf(stdout, "Linearized:  %t\n", res.Linearized)
			fmt.Fprintf(stdout, "Has forms:   %t\n", res.HasForms)
			fmt.Fprintf(stdout, "Portfolio:   %t\n", res.IsPortfolio)
			if *checkStreams {
				if len(res.UndecodableStreams) == 0 {
					fmt.Fprintln(stdout, "Undecodable: none")
				} else {
					fmt.Fprintf(stdout, "Undecodable: %d stream(s)\n", len(res.UndecodableStreams))
					for _, s := range res.UndecodableStreams {
						fmt.Fprintf(stdout, "  obj %d: %s\n", s.ObjectNum, strings.Join(s.Filters, ", "))
					}
				}
				if len(res.SalvagedStreams) == 0 {
					fmt.Fprintln(stdout, "Salvaged:    none")
				} else {
					fmt.Fprintf(stdout, "Salvaged:    %d stream(s)\n", len(res.SalvagedStreams))
					for _, s := range res.SalvagedStreams {
						fmt.Fprintf(stdout, "  obj %d: %s\n", s.ObjectNum, strings.Join(s.Filters, ", "))
					}
				}
			}
			if p := res.Permissions; p != nil {
				fmt.Fprintln(stdout, "Permissions:")
				fmt.Fprintf(stdout, "  Print:              %t\n", p.Print)
				fmt.Fprintf(stdout, "  Modify content:     %t\n", p.ModifyContent)
				fmt.Fprintf(stdout, "  Extract text:       %t\n", p.ExtractText)
				fmt.Fprintf(stdout, "  Modify annotations: %t\n", p.ModifyAnnotations)
				fmt.Fprintf(stdout, "  Fill forms:         %t\n", p.FillForms)
				fmt.Fprintf(stdout, "  Assemble:           %t\n", p.Assemble)
				fmt.Fprintf(stdout, "  Print high quality: %t\n", p.PrintHighQuality)
			}
		}
	}
	return exitOK
}

func jsonOut(stdout, stderr io.Writer, v any) int {
	if err := outputJSON(stdout, v); err != nil {
		fmt.Fprintf(stderr, "encoding JSON: %v\n", err)
		return exitError
	}
	return exitOK
}
