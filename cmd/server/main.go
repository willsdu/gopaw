package main

import (
	"os"

	"github.com/willisdu/gopaw/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:]))
}
