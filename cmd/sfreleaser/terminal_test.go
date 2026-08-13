//go:build unix

package main

import (
	"os"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/term"
)

func TestAttachTerminalTo_forwardsSizeAndInput(t *testing.T) {
	parentPTY, parentTTY := openPTYPair(t)
	childPTY, childTTY := openPTYPair(t)

	require.NoError(t, pty.Setsize(parentTTY, &pty.Winsize{Rows: 42, Cols: 132}))

	detach := attachTerminalTo(parentTTY, parentTTY.Name(), childPTY)
	defer detach()

	size, err := pty.GetsizeFull(childTTY)
	require.NoError(t, err)
	assert.Equal(t, uint16(42), size.Rows, "child PTY should have inherited our rows")
	assert.Equal(t, uint16(132), size.Cols, "child PTY should have inherited our columns")

	_, err = parentPTY.Write([]byte("hello\n"))
	require.NoError(t, err)

	assert.Equal(t, "hello\n", readWithin(t, childTTY, len("hello\n"), 5*time.Second))
}

func TestAttachTerminalTo_restoresTerminalOnDetach(t *testing.T) {
	_, parentTTY := openPTYPair(t)
	childPTY, _ := openPTYPair(t)

	previousState, err := term.GetState(int(parentTTY.Fd()))
	require.NoError(t, err)

	detach := attachTerminalTo(parentTTY, parentTTY.Name(), childPTY)
	detach()

	restoredState, err := term.GetState(int(parentTTY.Fd()))
	require.NoError(t, err)
	assert.Equal(t, previousState, restoredState, "terminal state should be back to what it was")
}

func TestAttachTerminalTo_notATerminal(t *testing.T) {
	notATerminal, err := os.Open(os.DevNull)
	require.NoError(t, err)
	t.Cleanup(func() { notATerminal.Close() })

	childPTY, _ := openPTYPair(t)

	// Nothing to assert beyond the fact that detaching a never-attached terminal is safe
	attachTerminalTo(notATerminal, os.DevNull, childPTY)()
}

func openPTYPair(t *testing.T) (ptyFile *os.File, ttyFile *os.File) {
	t.Helper()

	ptyFile, ttyFile, err := pty.Open()
	require.NoError(t, err)

	t.Cleanup(func() {
		ttyFile.Close()
		ptyFile.Close()
	})

	return ptyFile, ttyFile
}

// readWithin reads exactly `count` bytes from `file`, failing the test if they did not
// all arrive within `timeout`.
func readWithin(t *testing.T, file *os.File, count int, timeout time.Duration) string {
	t.Helper()

	type readResult struct {
		read []byte
		err  error
	}

	results := make(chan readResult, 1)
	go func() {
		read := make([]byte, count)
		_, err := readFull(file, read)

		results <- readResult{read, err}
	}()

	select {
	case result := <-results:
		require.NoError(t, result.err)
		return string(result.read)

	case <-time.After(timeout):
		require.Fail(t, "timed out waiting for the child PTY to receive the forwarded input")
		return ""
	}
}

func readFull(file *os.File, into []byte) (int, error) {
	read := 0
	for read < len(into) {
		n, err := file.Read(into[read:])
		read += n

		if err != nil {
			return read, err
		}
	}

	return read, nil
}
