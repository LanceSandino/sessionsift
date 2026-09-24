// Package main is the SessionSift command-line entrypoint.
// @module SessionSiftMain is the process entrypoint that hands arguments to the CLI.
package main

import (
	"context"
	"io"
	"os"

	"github.com/LanceSandino/sessionsift/src/internal/cli"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, in io.Reader, out, errOut io.Writer) int {
	return cli.Run(context.Background(), args, in, out, errOut)
}
