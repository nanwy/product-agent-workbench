package codex

import "sync"

// tailBuffer 只保留最近 limit 个字节，避免失控的子进程 stderr 持续占用内存。
// Write 可能由 os/exec 的复制 goroutine 调用，因此读取诊断信息时也要加锁。
type tailBuffer struct {
	mu    sync.Mutex
	limit int
	data  []byte
}

func newTailBuffer(limit int) *tailBuffer {
	return &tailBuffer{limit: limit, data: make([]byte, 0, limit)}
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	written := len(p)
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(p) >= b.limit {
		b.data = append(b.data[:0], p[len(p)-b.limit:]...)
		return written, nil
	}
	overflow := len(b.data) + len(p) - b.limit
	if overflow > 0 {
		copy(b.data, b.data[overflow:])
		b.data = b.data[:len(b.data)-overflow]
	}
	b.data = append(b.data, p...)
	return written, nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(append([]byte(nil), b.data...))
}
