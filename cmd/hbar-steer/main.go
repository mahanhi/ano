package main

import (
	"fmt"
	"os"

	"github.com/hbar/hbar/internal/via"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: hbar-steer config.json")
		os.Exit(2)
	}
	if err := via.RunSteer(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
