package main

import (
	"os"

	"gopaw/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:]))
}

