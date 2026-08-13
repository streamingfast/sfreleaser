//go:build !unix

package main

import "os"

// attachTerminal is a no-op outside of Unix, where we do not run commands through a PTY
// in the first place.
func attachTerminal(childPTY *os.File) (detach func()) {
	return func() {}
}
