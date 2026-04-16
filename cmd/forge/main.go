package main

import (
	"fmt"
	"os"

	"github.com/arturklasa/forge/internal/cli"
)

func main() {
	root := cli.NewRootCmd()
	cli.RegisterCommands(root)

	if err := root.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
