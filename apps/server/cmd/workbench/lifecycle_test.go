package main

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"
)

func TestServeRuntimeStopsWhenContextIsCancelled(t *testing.T) {
	components, err := buildRuntime(validRuntimeDependencies())
	if err != nil {
		t.Fatalf("buildRuntime() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = serveRuntimeOnListener(ctx, processConfig{
		Address:         "127.0.0.1:0",
		DrainInterval:   time.Millisecond,
		ShutdownTimeout: time.Second,
	}, components, newBlockingListener())
	if err != nil {
		t.Fatalf("serveRuntime() error = %v", err)
	}
}

// blockingListener 不占用操作系统端口；Shutdown 调用 Close 后，Accept 才返回。
type blockingListener struct {
	closed chan struct{}
	once   sync.Once
}

func newBlockingListener() *blockingListener {
	return &blockingListener{closed: make(chan struct{})}
}

func (listener *blockingListener) Accept() (net.Conn, error) {
	<-listener.closed
	return nil, net.ErrClosed
}

func (listener *blockingListener) Close() error {
	listener.once.Do(func() { close(listener.closed) })
	return nil
}

func (*blockingListener) Addr() net.Addr {
	return testAddress("127.0.0.1:0")
}

type testAddress string

func (address testAddress) Network() string { return "tcp" }
func (address testAddress) String() string  { return string(address) }

func TestValidateProcessConfigRejectsNonLoopbackAddress(t *testing.T) {
	config := processConfig{
		Address:         "0.0.0.0:8080",
		CodexExecutable: "codex",
		GitExecutable:   "git",
		Concurrency:     1,
		DrainInterval:   time.Millisecond,
		ShutdownTimeout: time.Second,
	}
	if err := validateProcessConfig(config); err == nil {
		t.Fatal("validateProcessConfig() error = nil, want non-loopback rejection")
	}
}
