package main

import (
	"fmt"
	"io"
	"os"

	"github.com/sharedcode/sop/tools/sopbackend"
)

func runAdminCLI(args []string, stdout, stderr io.Writer) error {
	return sopbackend.RunAdminCLI(args, stdout, stderr)
}

func main() {
	if err := runAdminCLI(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
