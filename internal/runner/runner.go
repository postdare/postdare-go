package runner

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/hellodeveye/postdare-go/internal/sse"
)

type CommandRunner interface {
	Run(ctx context.Context, taskID uint64, stage string, command string) error
}

type EnvironmentCommandRunner interface {
	RunWithEnv(ctx context.Context, taskID uint64, stage string, command string, env map[string]string) error
}

type CaptureCommandRunner interface {
	RunCapture(ctx context.Context, taskID uint64, stage string, command string, env map[string]string, maxBytes int64) ([]byte, error)
}

type LocalCommandRunner struct {
	LogDir  string
	Timeout time.Duration
	Hub     *sse.Hub
	Logger  *zap.Logger
}

func (r *LocalCommandRunner) Run(parent context.Context, taskID uint64, stage string, command string) error {
	return r.RunWithEnv(parent, taskID, stage, command, nil)
}

func (r *LocalCommandRunner) RunWithEnv(parent context.Context, taskID uint64, stage string, command string, env map[string]string) error {
	if command == "" {
		return nil
	}
	if r.Timeout == 0 {
		r.Timeout = 30 * time.Minute
	}
	ctx, cancel := context.WithTimeout(parent, r.Timeout)
	defer cancel()

	if err := os.MkdirAll(r.LogDir, 0o755); err != nil {
		return err
	}
	logPath := filepath.Join(r.LogDir, fmt.Sprintf("%d.log", taskID))
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()

	cmd := exec.CommandContext(ctx, "bash", "-lc", command)
	cmd.Env = append(os.Environ(), envPairs(env)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	waitDone := make(chan struct{})
	go terminateProcessGroupOnCancel(ctx, cmd, waitDone)

	var mu sync.Mutex
	writeLine := func(line string) {
		formatted := fmt.Sprintf("[%s] %s\n", stage, line)
		mu.Lock()
		_, _ = file.WriteString(formatted)
		mu.Unlock()
		if r.Hub != nil {
			r.Hub.Publish(sse.DeployTopic(taskID), formatted)
		}
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go scanPipe(stdout, &wg, writeLine)
	go scanPipe(stderr, &wg, writeLine)
	wg.Wait()

	err = cmd.Wait()
	close(waitDone)
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		writeLine("command timeout")
		return fmt.Errorf("command timeout after %s", r.Timeout)
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		writeLine("command canceled")
		return fmt.Errorf("command canceled")
	}
	if err != nil {
		exitCode := -1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				exitCode = status.ExitStatus()
			}
		}
		writeLine(fmt.Sprintf("command exited with code %d", exitCode))
		return fmt.Errorf("command exited with code %d: %w", exitCode, err)
	}
	writeLine("stage command completed")
	return nil
}

// RunCapture keeps stdout out of the deploy log and returns it to the caller.
// stderr remains visible in the log so operators can diagnose a failed script.
func (r *LocalCommandRunner) RunCapture(parent context.Context, taskID uint64, stage string, command string, env map[string]string, maxBytes int64) ([]byte, error) {
	if command == "" {
		return nil, nil
	}
	if r.Timeout == 0 {
		r.Timeout = 30 * time.Minute
	}
	ctx, cancel := context.WithTimeout(parent, r.Timeout)
	defer cancel()
	if err := os.MkdirAll(r.LogDir, 0o755); err != nil {
		return nil, err
	}
	logPath := filepath.Join(r.LogDir, strconv.FormatUint(taskID, 10)+".log")
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	cmd := exec.CommandContext(ctx, "bash", "-lc", command)
	cmd.Env = append(os.Environ(), envPairs(env)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	waitDone := make(chan struct{})
	go terminateProcessGroupOnCancel(ctx, cmd, waitDone)

	writeError := func(line string) {
		formatted := fmt.Sprintf("[%s] %s\n", stage, line)
		_, _ = file.WriteString(formatted)
		if r.Hub != nil {
			r.Hub.Publish(sse.DeployTopic(taskID), formatted)
		}
	}
	var output cappedBuffer
	output.max = maxBytes
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = output.ReadFrom(stdout)
	}()
	go scanPipe(stderr, &wg, writeError)
	wg.Wait()
	err = cmd.Wait()
	close(waitDone)
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		writeError("command timeout")
		return nil, fmt.Errorf("command timeout after %s", r.Timeout)
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		writeError("command canceled")
		return nil, fmt.Errorf("command canceled")
	}
	if output.exceeded {
		writeError("captured stdout exceeded limit")
		return nil, fmt.Errorf("captured stdout exceeds %d bytes", maxBytes)
	}
	if err != nil {
		exitCode := -1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
		writeError(fmt.Sprintf("command exited with code %d", exitCode))
		return nil, fmt.Errorf("command exited with code %d: %w", exitCode, err)
	}
	return output.Bytes(), nil
}

type cappedBuffer struct {
	bytes.Buffer
	max      int64
	exceeded bool
}

func (b *cappedBuffer) ReadFrom(src interface{ Read([]byte) (int, error) }) (int64, error) {
	buf := make([]byte, 32*1024)
	var total int64
	for {
		n, err := src.Read(buf)
		if n > 0 {
			total += int64(n)
			remaining := b.max + 1 - int64(b.Len())
			if remaining > 0 {
				keep := int64(n)
				if keep > remaining {
					keep = remaining
				}
				_, _ = b.Buffer.Write(buf[:keep])
			}
			if total > b.max {
				b.exceeded = true
			}
		}
		if err != nil {
			if errors.Is(err, os.ErrClosed) {
				return total, nil
			}
			if errors.Is(err, io.EOF) {
				return total, nil
			}
			return total, err
		}
	}
}

func envPairs(env map[string]string) []string {
	pairs := make([]string, 0, len(env))
	for key, value := range env {
		pairs = append(pairs, key+"="+value)
	}
	return pairs
}

func terminateProcessGroupOnCancel(ctx context.Context, cmd *exec.Cmd, waitDone <-chan struct{}) {
	<-ctx.Done()
	if cmd.Process == nil {
		return
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		return
	}
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	select {
	case <-waitDone:
	case <-time.After(3 * time.Second):
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	}
}

func scanPipe(pipe interface{ Read([]byte) (int, error) }, wg *sync.WaitGroup, writeLine func(string)) {
	defer wg.Done()
	scanner := bufio.NewScanner(pipe)
	buffer := make([]byte, 0, 64*1024)
	scanner.Buffer(buffer, 1024*1024)
	for scanner.Scan() {
		writeLine(scanner.Text())
	}
}

func AppendLog(logFile string, hub *sse.Hub, taskID uint64, stage string, line string) {
	_ = os.MkdirAll(filepath.Dir(logFile), 0o755)
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err == nil {
		_, _ = f.WriteString(fmt.Sprintf("[%s] %s\n", stage, line))
		_ = f.Close()
	}
	if hub != nil {
		hub.Publish(sse.DeployTopic(taskID), fmt.Sprintf("[%s] %s\n", stage, line))
	}
}
