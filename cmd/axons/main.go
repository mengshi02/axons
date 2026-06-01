// Package main is the entry point for the axons CLI.
package main

import (
	"os"

	"github.com/mengshi02/axons/cmd/axons/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}