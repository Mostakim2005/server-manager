package main

import (
	"os"

	"github.com/Mostakim2005/server-manager/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
