package main

import (
	"os"

	"github.com/monkeyWzr/technician/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
