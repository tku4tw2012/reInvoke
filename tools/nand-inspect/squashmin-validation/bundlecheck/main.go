package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/tku4tw2012/reinvoke/tools/nand-inspect/internal/squashmin"
)

func main() {
	source := flag.String("source", "", "pinned original SquashFS")
	capture := flag.String("capture", "", "pinned data-only NAND capture")
	candidate := flag.String("candidate", "", "candidate bundle directory")
	gzip := flag.String("gzip", "/usr/bin/gzip", "existing host GNU gzip used for canonical patch replay")
	flag.Parse()
	if *source == "" || *capture == "" || *candidate == "" || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "required: -source FILE -capture FILE -candidate DIRECTORY")
		os.Exit(1)
	}
	if err := squashmin.VerifyBundle(*source, *capture, *candidate, *gzip, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "bundle verification failed:", err)
		os.Exit(1)
	}
}
