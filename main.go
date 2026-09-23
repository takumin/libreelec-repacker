package main

import (
	"os"

	"github.com/takumin/libreelec-repacker/internal/command"
)

var osExit = os.Exit

func main() {
	osExit(command.Main(
		os.Stdout,
		os.Stderr,
		os.Stdin,
		os.Args,
	))
}
