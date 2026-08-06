//go:build !wails

package main

import "fmt"

func main() {
	// The default build is intentionally headless and dependency-free. Native
	// Wails builds opt into the wails tag so Go tests do not link a platform
	// WebView by accident.
	fmt.Println("OneAgent desktop shell: build with -tags wails")
}
