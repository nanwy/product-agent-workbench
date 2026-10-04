package domain

import "time"

// TaskEvent 是可以持久化并通过 SSE 通知浏览器的有序事件。
//
// Payload 只保存已经脱敏、可公开给本地浏览器的数据；数据库主键负责提供全局 SSE id，
// Sequence 负责表达同一个 TaskRun 内的严格顺序。
type TaskEvent struct {
	ID        int64
	TaskRunID string
	Sequence  int64
	Type      string
	Payload   []byte
	CreatedAt time.Time
}
