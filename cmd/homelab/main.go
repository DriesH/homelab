package main

import (
	"fmt"
	"os"
)

var version = "dev"

func main() {
	command := "serve"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}

	var err error
	switch command {
	case "serve":
		err = serve()
	case "admin":
		err = setupAdmin()
	case "version":
		fmt.Println(version)
	default:
		fmt.Fprintln(os.Stderr, "usage: homelab [serve|admin|version]")
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
