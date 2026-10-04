package store

import "errors"

var (
	// ErrNotFound 表示指定实体不存在，或当前没有可领取的任务。
	ErrNotFound = errors.New("资源不存在")

	// ErrAlreadyExists 表示同一 ID 的实体已经存在。
	ErrAlreadyExists = errors.New("资源已存在")

	// ErrRevisionConflict 表示调用方基于旧版本提交了更新。
	//
	// 应用层可以通过 errors.Is 判断它，并在 HTTP 层映射成 409 Conflict。
	ErrRevisionConflict = errors.New("版本冲突")

	// ErrInvalidEventSequence 表示事件序号不是从上一条事件连续递增。
	ErrInvalidEventSequence = errors.New("事件序号不连续")
)
