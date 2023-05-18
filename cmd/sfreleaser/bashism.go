package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/creack/pty"
	"github.com/kballard/go-shellquote"
	"github.com/streamingfast/cli"
	"go.uber.org/zap"
)

var envRegex = regexp.MustCompile(`^\s*([a-zA-Z_0-9]+)\s*=\s*([^\s]+)\s*$`)

type commandInfo struct {
	env     []string
	command string
	args    []string
}

func newCommandInfo(inputs ...string) *commandInfo {
	var env []string
	args := make([]string, 0, len(inputs))
	for _, input := range unquotedFlatten(inputs...) {
		// Accepts env as long as we did not start arguments parsing yet
		if len(args) == 0 {
			if matches := envRegex.FindStringSubmatch(input); len(matches) > 0 {
				env = append(env, matches[0])
				continue
			}
		}

		args = append(args, input)
	}

	if len(args) == 0 {
		return &commandInfo{
			env, "", nil,
		}
	}

	return &commandInfo{
		env, args[0], args[1:],
	}
}

func unquotedFlatten(inputs ...string) (out []string) {
	for _, input := range inputs {
		parts, err := shellquote.Split(input)
		cli.NoError(err, "Shell splitting failed")

		out = append(out, parts...)
	}

	return
}

func (i *commandInfo) RunCombined() (combined string, err error) {
	return combinedOutput(i.ToCommand())
}

func (i *commandInfo) RunSplit() (stdOut string, stdErr string, combined string, err error) {
	return splitOutput(i.ToCommand())
}

func (i *commandInfo) ToCommand() *exec.Cmd {
	cmd := exec.Command(i.command, i.args...)
	cmd.Env = i.env

	return cmd
}

func (i *commandInfo) String() string {
	return strings.Join(append(append(i.env, i.command), i.args...), " ")
}

func run(inputs ...string) (output string) {
	output, info, err := maybeRun(inputs...)
	cli.NoError(err, "Command %q failed", info)

	return output
}

// runSilent is like [run] but do not print the command output but do print
// the failure
func runSilent(inputs ...string) (output string, err error) {
	output, info, err := internalMaybeRun(inputs, true)
	if err != nil {
		zlog.Debug("run command failed", zap.Stringer("cmd", info), zap.Error(err), zap.String("output", output))
		cli.Exit(1)
	}

	return output, nil
}

func maybeRun(inputs ...string) (output string, info *commandInfo, err error) {
	return internalMaybeRun(inputs, false)
}

func internalMaybeRun(inputs []string, silent bool) (output string, info *commandInfo, err error) {
	startTime := time.Now()

	defer func() {
		var fields []zap.Field
		if info != nil {
			fields = append(fields, zap.Stringer("command", info))
		}
		fields = append(fields, zap.Duration("took", time.Since(startTime)), zap.Bool("success", err == nil))

		zlog.Debug("run of command terminated", fields...)
	}()

	info = newCommandInfo(inputs...)
	cli.Ensure(info.command != "", "Must have at least command to run")

	zlog.Debug("starting command through PTY", zap.Stringer("cmd", info))

	cmd := info.ToCommand()
	ptyFile, err := pty.Start(cmd)
	cli.NoError(err, "Unable to create PTY")
	defer ptyFile.Close()

	// FIXME: What to do with error where program would like to receive data written to terminal,
	// for example for input?

	captured := bytes.NewBuffer(nil)

	var outputWriter io.Writer = os.Stdout
	if silent {
		outputWriter = io.Discard
	}

	writer := io.MultiWriter(outputWriter, captured)

	go func() {
		zlog.Debug("starting copy of process pty output to stdout")
		_, err = io.Copy(writer, ptyFile)
		cli.NoError(err, "Unable to copy command PTY to stdout")
		zlog.Debug("completed pty output copier")
	}()

	err = cmd.Wait()
	return captured.String(), info, err
}

func resultOf(inputs ...string) string {
	output, info, err := maybeResultOf(inputs...)
	cli.NoError(err, "Command %q failed", info)

	return output
}

func maybeResultOf(inputs ...string) (output string, info *commandInfo, err error) {
	info = newCommandInfo(inputs...)
	cli.Ensure(info.command != "", "Must have at least command to run")

	zlog.Debug("executing command for maybe result", zap.Stringer("command", info))
	start := time.Now()

	defer func() {
		zlog.Debug("command execution completed", zap.Stringer("command", info), zap.String("output", output), zap.Duration("took", time.Since(start)))
	}()

	rawStdout, _, combined, err := info.RunSplit()
	if err != nil {
		return combined, info, err
	}

	return string(rawStdout), info, nil
}

func combinedOutput(c *exec.Cmd) (string, error) {
	if c.Stdout != nil {
		return "", errors.New("exec: Stdout already set")
	}
	if c.Stderr != nil {
		return "", errors.New("exec: Stderr already set")
	}

	var buffer bytes.Buffer
	c.Stdout = &buffer
	c.Stderr = &buffer

	err := c.Run()
	return buffer.String(), err
}

func splitOutput(c *exec.Cmd) (string, string, string, error) {
	if c.Stdout != nil {
		return "", "", "", errors.New("exec: Stdout already set")
	}
	if c.Stderr != nil {
		return "", "", "", errors.New("exec: Stderr already set")
	}

	var allOutput bytes.Buffer

	var stdoutBuffer bytes.Buffer
	c.Stdout = io.MultiWriter(&allOutput, &stdoutBuffer)

	var stderrBuffer bytes.Buffer
	c.Stderr = io.MultiWriter(&allOutput, &stderrBuffer)

	err := c.Run()
	return stdoutBuffer.String(), stderrBuffer.String(), allOutput.String(), err
}
