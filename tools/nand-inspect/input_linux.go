package main

import (
	"os"

	"github.com/tku4tw2012/reinvoke/tools/nand-inspect/internal/regularfile"
)

const linuxOPath = regularfile.PathOnly

func openRegularInput(path string) (*os.File, error) {
	return regularfile.Open(path)
}

func reopenRegularInput(pinned *os.File) (*os.File, error) {
	return regularfile.Reopen(pinned)
}
