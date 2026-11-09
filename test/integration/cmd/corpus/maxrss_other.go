//go:build !unix

package main

import "os"

// maxRSS returns 0: the OS reports no peak resident set size.
func maxRSS(*os.ProcessState) int64 { return 0 }
