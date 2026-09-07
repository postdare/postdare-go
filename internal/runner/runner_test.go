package runner

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestLocalCommandRunnerCancelsProcessGroup(t *testing.T) {
	tmp := t.TempDir()
	pidFile := filepath.Join(tmp, "child.pid")
	r := &LocalCommandRunner{
		LogDir:  filepath.Join(tmp, "logs"),
		Timeout: time.Minute,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- r.Run(ctx, 1, "test", "sh -c 'sleep 30 & echo $! > \""+pidFile+"\"; wait'")
	}()

	var childPID int
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(pidFile)
		if err == nil {
			pid, parseErr := strconv.Atoi(strings.TrimSpace(string(raw)))
			if parseErr == nil && pid > 0 {
				childPID = pid
				break
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	if childPID == 0 {
		cancel()
		t.Fatal("child process pid was not written")
	}

	cancel()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "command canceled") {
			t.Fatalf("expected command canceled error, got %v", err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("runner did not return after cancellation")
	}

	deadline = time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if !processExists(childPID) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("child process %d still exists after cancel", childPID)
}

func TestLocalCommandRunnerCaptureEnforcesLimitAndKeepsStdoutOutOfLog(t *testing.T) {
	tmp := t.TempDir()
	r := &LocalCommandRunner{LogDir: tmp, Timeout: time.Minute}
	output, err := r.RunCapture(context.Background(), 7, "review", "printf '{\"ok\":true}'; printf 'diagnostic' >&2", nil, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if string(output) != `{"ok":true}` {
		t.Fatalf("unexpected output %q", output)
	}
	logBytes, err := os.ReadFile(filepath.Join(tmp, "7.log"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(logBytes), `{"ok":true}`) || !strings.Contains(string(logBytes), "diagnostic") {
		t.Fatalf("unexpected capture log %q", logBytes)
	}
	if _, err := r.RunCapture(context.Background(), 8, "review", "yes x | head -c 2049", nil, 2048); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected output limit error, got %v", err)
	}
}

func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil
}

// Run, RunWithEnv and RunCapture share one execution path; this pins the part
// that differs between them -- where stdout goes -- plus the env both inject.
func TestLocalCommandRunnerSharedPathRoutesStdoutAndEnv(t *testing.T) {
	tmp := t.TempDir()
	r := &LocalCommandRunner{LogDir: tmp, Timeout: time.Minute}
	env := map[string]string{"POSTDARE_COMMIT_ID": "abc123"}

	if err := r.RunWithEnv(context.Background(), 11, "build", `printf 'commit=%s\n' "$POSTDARE_COMMIT_ID"`, env); err != nil {
		t.Fatal(err)
	}
	streamed, err := os.ReadFile(filepath.Join(tmp, "11.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(streamed), "[build] commit=abc123") {
		t.Fatalf("stdout should reach the deploy log: %q", streamed)
	}
	if !strings.Contains(string(streamed), "stage command completed") {
		t.Fatalf("streamed stage should log completion: %q", streamed)
	}

	output, err := r.RunCapture(context.Background(), 12, "review", `printf 'commit=%s' "$POSTDARE_COMMIT_ID"`, env, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if string(output) != "commit=abc123" {
		t.Fatalf("capture should receive stdout and env, got %q", output)
	}
	captured, err := os.ReadFile(filepath.Join(tmp, "12.log"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(captured), "commit=abc123") {
		t.Fatalf("captured stdout must stay out of the deploy log: %q", captured)
	}

	if err := r.Run(context.Background(), 13, "fail", "exit 3"); err == nil || !strings.Contains(err.Error(), "code 3") {
		t.Fatalf("expected exit code 3, got %v", err)
	}
}
