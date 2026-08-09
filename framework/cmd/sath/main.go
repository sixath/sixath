package main

import (
	"os"

	"github.com/sixath/framework/cli"
)

func main() {
	if err := cli.NewRootCommand().Execute(); err != nil {
		os.Exit(1)
	}
}
