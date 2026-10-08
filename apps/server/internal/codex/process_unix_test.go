//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package codex

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAppServerProcessShutdownHonorsDeadlineWhenNotificationHandlerBlocks(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	process, err := LaunchAppServer(context.Background(), AppServerLaunchConfig{
		Executable:       "/bin/sh",
		PrefixArgs:       []string{"-c", `printf '{"method":"test/block","params":{}}\n'; while :; do sleep 1; done`},
		WorkingDirectory: t.TempDir(),
		Environment:      []string{"PATH=/usr/bin:/bin"},
		NotificationSink: func(context.Context, Notification) error {
			close(entered)
			<-release
			return nil
		},
	})
	if err != nil {
		t.Fatalf("LaunchAppServer() error = %v", err)
	}

	select {
	case <-entered:
	case <-time.After(time.Second):
		close(release)
		_ = process.Shutdown(context.Background())
		t.Fatal("notification handler was not called")
	}

	shutdownContext, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	startedAt := time.Now()
	shutdownErr := process.Shutdown(shutdownContext)
	cancel()
	if !errors.Is(shutdownErr, context.DeadlineExceeded) {
		close(release)
		t.Fatalf("Shutdown() error = %v, want context deadline", shutdownErr)
	}
	if elapsed := time.Since(startedAt); elapsed > time.Second {
		close(release)
		t.Fatalf("Shutdown() elapsed = %v, deadline was not honored", elapsed)
	}

	select {
	case <-process.Done():
	case <-time.After(time.Second):
		close(release)
		t.Fatal("force-killed app-server did not exit")
	}
	waitContext, cancelWait := context.WithTimeout(context.Background(), 20*time.Millisecond)
	_, waitErr := process.Wait(waitContext)
	cancelWait()
	if !errors.Is(waitErr, context.DeadlineExceeded) {
		close(release)
		t.Fatalf("Wait() error = %v, want context deadline while handler is blocked", waitErr)
	}

	// 解除测试故意制造的阻塞，让 Connection.Close goroutine 完整退出，避免测试自身泄漏。
	close(release)
	_ = process.Shutdown(context.Background())
}
