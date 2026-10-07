package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

func TestAppServerClientResumeRejectsDifferentThreadIdentity(t *testing.T) {
	connection, stop := newScriptedConnection(t, func(message jsonRPCMessage) (any, bool) {
		switch message.Method {
		case "initialize":
			return map[string]any{}, true
		case "thread/resume":
			return map[string]any{"thread": map[string]string{"id": "thread-other"}}, true
		default:
			return nil, false
		}
	})
	defer stop()

	client, err := NewAppServerClient(connection, ClientInfo{Name: "test", Title: "Test", Version: "1"})
	if err != nil {
		t.Fatalf("NewAppServerClient() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	_, err = client.OpenThread(ctx, TurnRequest{
		WorkingDirectory: t.TempDir(),
		ThreadID:         "thread-requested",
		SandboxMode:      SandboxWorkspaceWrite,
	})
	if err == nil || !strings.Contains(err.Error(), "身份不一致") {
		t.Fatalf("OpenThread() error = %v, want identity mismatch", err)
	}
}

func TestAppServerClientContractWithFakeServer(t *testing.T) {
	connection, stop := newScriptedConnection(t, func(message jsonRPCMessage) (any, bool) {
		switch message.Method {
		case "initialize":
			return map[string]any{}, true
		case "thread/start":
			return map[string]any{"thread": map[string]string{"id": "thread-1"}}, true
		case "turn/start":
			return map[string]any{"turn": map[string]string{"id": "turn-1"}}, true
		case "turn/interrupt":
			return map[string]any{}, true
		default:
			return nil, false
		}
	})
	defer stop()

	client, err := NewAppServerClient(connection, ClientInfo{Name: "test", Title: "Test", Version: "1"})
	if err != nil {
		t.Fatalf("NewAppServerClient() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	request := TurnRequest{WorkingDirectory: t.TempDir(), Prompt: "实现 Ticket", SandboxMode: SandboxWorkspaceWrite}
	threadID, err := client.OpenThread(ctx, request)
	if err != nil || threadID != "thread-1" {
		t.Fatalf("OpenThread() = %q, %v", threadID, err)
	}
	turnID, err := client.StartTurn(ctx, threadID, request)
	if err != nil || turnID != "turn-1" {
		t.Fatalf("StartTurn() = %q, %v", turnID, err)
	}
	if err := client.InterruptTurn(ctx, threadID, turnID); err != nil {
		t.Fatalf("InterruptTurn() error = %v", err)
	}
}

func TestJSONRPCRequestCancellationKeepsConnectionUsable(t *testing.T) {
	connection, stop := newScriptedConnection(t, func(message jsonRPCMessage) (any, bool) {
		if message.Method == "ping" {
			return map[string]bool{"ok": true}, true
		}
		return nil, false
	})
	defer stop()

	cancelledContext, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := connection.Request(cancelledContext, "never/respond", struct{}{}, nil); err == nil {
		t.Fatal("Request(never/respond) error = nil, want context deadline")
	}

	ctx, cancelPing := context.WithTimeout(context.Background(), time.Second)
	defer cancelPing()
	var result struct {
		OK bool `json:"ok"`
	}
	if err := connection.Request(ctx, "ping", struct{}{}, &result); err != nil {
		t.Fatalf("Request(ping) after cancellation error = %v", err)
	}
	if !result.OK {
		t.Fatal("Request(ping) result OK = false")
	}
}

type scriptedResponse func(message jsonRPCMessage) (result any, respond bool)

// newScriptedConnection 用内存管道模拟 app-server，验证真实 JSONL 编解码与请求 ID 路由。
func newScriptedConnection(t *testing.T, script scriptedResponse) (*JSONRPCConnection, func()) {
	t.Helper()
	clientInput, serverOutput := io.Pipe()
	serverInput, clientOutput := io.Pipe()
	connection, err := NewJSONRPCConnection(JSONRPCDependencies{
		Input:  clientInput,
		Output: clientOutput,
	})
	if err != nil {
		t.Fatalf("NewJSONRPCConnection() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := connection.Start(ctx); err != nil {
		t.Fatalf("connection.Start() error = %v", err)
	}

	serverDone := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(serverInput)
		for scanner.Scan() {
			var message jsonRPCMessage
			if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
				serverDone <- err
				return
			}
			result, respond := script(message)
			if !respond || len(message.ID) == 0 {
				continue
			}
			body, err := json.Marshal(struct {
				ID     json.RawMessage `json:"id"`
				Result any             `json:"result"`
			}{ID: message.ID, Result: result})
			if err != nil {
				serverDone <- err
				return
			}
			if _, err := fmt.Fprintf(serverOutput, "%s\n", body); err != nil {
				serverDone <- err
				return
			}
		}
		serverDone <- scanner.Err()
	}()

	stop := func() {
		cancel()
		_ = connection.Close()
		_ = serverOutput.Close()
		_ = serverInput.Close()
		select {
		case err := <-serverDone:
			if err != nil && !strings.Contains(err.Error(), "closed pipe") {
				t.Errorf("fake app-server error = %v", err)
			}
		case <-time.After(time.Second):
			t.Error("fake app-server did not stop")
		}
	}
	return connection, stop
}
