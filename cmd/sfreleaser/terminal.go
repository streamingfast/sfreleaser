//go:build unix

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"slices"
	"sync"
	"syscall"

	"github.com/creack/pty"
	"github.com/streamingfast/cli"
	"go.uber.org/zap"
	"golang.org/x/term"
)

const restoreTerminalExitHandlerID = "restore-terminal"

// controllingTerminalPath is the terminal attached to our process group, opening it
// yields a brand new file description which is exactly what [openNonBlocking] needs.
const controllingTerminalPath = "/dev/tty"

// attachTerminal wires our terminal to the PTY a child command runs in, propagating the
// window size, switching to raw mode and forwarding keystrokes for as long as the child
// lives. The returned function undoes all of it and must always be called.
//
// Without this, a child that talks to the terminal (an editor, a passphrase prompt, or
// any TUI probing for capabilities) writes its escape sequences through us to the real
// terminal, whose answers then land on our unread standard input and end up as garbage
// in whatever reads it next.
func attachTerminal(childPTY *os.File) (detach func()) {
	return attachTerminalTo(os.Stdin, controllingTerminalPath, childPTY)
}

// attachTerminalTo is [attachTerminal] with the parent side injected, `parentTTYPath`
// being a path that opens the very same terminal as `parentTTY`.
func attachTerminalTo(parentTTY *os.File, parentTTYPath string, childPTY *os.File) (detach func()) {
	if !term.IsTerminal(int(parentTTY.Fd())) {
		zlog.Debug("not running against a terminal, leaving the child's PTY unattached")
		return func() {}
	}

	detachers := []func(){
		forwardTerminalSize(parentTTY, childPTY),
		enableRawMode(parentTTY),
		forwardTerminalInput(parentTTYPath, childPTY),
	}

	return func() {
		for _, detacher := range slices.Backward(detachers) {
			detacher()
		}
	}
}

// forwardTerminalSize gives the child's PTY our own dimensions and keeps them in sync,
// a PTY defaults to 0x0 otherwise and anything drawing a full screen renders garbled.
func forwardTerminalSize(parentTTY *os.File, childPTY *os.File) (stop func()) {
	resize := func() {
		if err := pty.InheritSize(parentTTY, childPTY); err != nil {
			zlog.Debug("unable to propagate terminal size to the child's PTY", zap.Error(err))
		}
	}
	resize()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGWINCH)

	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-signals:
				resize()
			case <-done:
				return
			}
		}
	}()

	return func() {
		signal.Stop(signals)
		close(done)
	}
}

// enableRawMode hands every keystroke to the child unprocessed, which is what makes an
// interactive child usable. The previous state is restored on detach and, because a raw
// terminal no longer echoes what you type, also on exit.
func enableRawMode(parentTTY *os.File) (restore func()) {
	fd := int(parentTTY.Fd())

	previousState, err := term.MakeRaw(fd)
	if err != nil {
		zlog.Debug("unable to put the terminal in raw mode", zap.Error(err))
		return func() {}
	}

	restoreOnce := sync.OnceFunc(func() {
		if err := term.Restore(fd, previousState); err != nil {
			zlog.Debug("unable to restore the terminal state", zap.Error(err))
		}
	})

	cli.ExitHandler(restoreTerminalExitHandlerID, func(_ int) { restoreOnce() })

	return func() {
		restoreOnce()
		cli.ExitHandler(restoreTerminalExitHandlerID, nil)
	}
}

// forwardTerminalInput copies what we read from the terminal into the child's PTY until
// the returned function is called.
func forwardTerminalInput(parentTTYPath string, childPTY *os.File) (stop func()) {
	input, err := openNonBlocking(parentTTYPath)
	if err != nil {
		zlog.Debug("unable to open the terminal for reading, the child will not receive any input",
			zap.String("path", parentTTYPath),
			zap.Error(err),
		)
		return func() {}
	}

	go func() {
		if _, err := io.Copy(childPTY, input); err != nil && !errors.Is(err, os.ErrClosed) {
			zlog.Debug("terminal input forwarding terminated", zap.Error(err))
		}
	}()

	return func() {
		if err := input.Close(); err != nil {
			zlog.Debug("unable to close the terminal reading end", zap.Error(err))
		}
	}
}

// openNonBlocking opens `path` in non-blocking mode so that the Go runtime poller owns
// the file descriptor, which is what makes a pending `Read` cancellable: closing the
// returned file unblocks it. A forwarding goroutine left stuck on a terminal read would
// otherwise outlive its command and steal the first keystroke of our next prompt.
//
// Opening the terminal afresh rather than duplicating our standard input is deliberate,
// `O_NONBLOCK` is carried by the open file description and duplicating would hand it to
// the shell that started us.
func openNonBlocking(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("open %q: %w", path, err)
	}
	syscall.CloseOnExec(fd)

	return os.NewFile(uintptr(fd), path), nil
}
