// Command ddassets builds and keeps the asset index: every Dungeondraft asset
// you can see on a map, with its picture, size, description and tags.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "usage: ddassets collect|describe [flags]")
	os.Exit(1)
}
