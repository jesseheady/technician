package main

import (
	"os"

	"github.com/m0nkey/technician/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
