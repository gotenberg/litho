package main

import (
	"flag"

	"github.com/gotenberg/litho/pkg/pdf"
)

// padesFlags holds the flags that raise a signature to a PAdES baseline level
// (ETSI EN 319 142-1): the sign command and the pipe's sign stage share them.
type padesFlags struct {
	pades *bool
}

// registerPAdESFlags declares the PAdES flags on fs.
func registerPAdESFlags(fs *flag.FlagSet) padesFlags {
	return padesFlags{
		pades: fs.Bool("pades", false, "Write a PAdES baseline signature (ETSI.CAdES.detached) instead of adbe.pkcs7.detached"),
	}
}

// options returns the [pdf.SignOption] values the flags set.
func (f padesFlags) options() []pdf.SignOption {
	var opts []pdf.SignOption
	if *f.pades {
		opts = append(opts, pdf.WithPAdES())
	}
	return opts
}
