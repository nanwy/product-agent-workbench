package httpapi

import (
	"errors"
	"net/http"
)

const maxJSONBodyBytes int64 = 1 << 20 // 1 MiB，防止客户端用超大 body 占用本地进程内存。

var (
	// ErrInvalidRequest 表示 HTTP body 不是当前 endpoint 接受的单个 JSON 对象。
	// 它属于传输层错误，与领域层 domain.ErrInvalidInput 的来源不同，但都会映射为 400。
	ErrInvalidRequest = errors.New("HTTP 请求无效")

	// errNotImplemented 只用于尚未完成的 HTTP 学习占位。
	// 四组 Requirement HTTP 测试全部通过后应删除。
	errNotImplemented = errors.New("Requirement HTTP 练习尚未实现")
)

// decodeJSON 严格读取一个 JSON 请求对象。
//
// 实现要点：
//  1. 用 http.MaxBytesReader 限制 r.Body，最大读取 maxJSONBodyBytes。
//  2. 创建 json.Decoder，并调用 DisallowUnknownFields。
//  3. 第一次 Decode：空 body、语法错误、类型错误、未知字段和超限都包装为 ErrInvalidRequest。
//  4. 再 Decode 一次，必须得到 io.EOF；否则说明 body 中有多个 JSON 值，也返回 ErrInvalidRequest。
//  5. 错误可以用 fmt.Errorf("%w: %v", ErrInvalidRequest, err) 保留调试原因；对外响应不能
//     直接暴露该原因。
func decodeJSON(
	w http.ResponseWriter,
	r *http.Request,
	destination any,
) error {
	return errNotImplemented
}

// writeJSON 写入一个完整 JSON 响应。
//
// 实现要点：
//  1. 先 json.Marshal(value)，编码失败时还没有提交响应头。
//  2. 在编码结果末尾追加换行，便于终端阅读。
//  3. 设置 Content-Type，再 WriteHeader(status)，最后写入 body。
//  4. 返回编码或写入错误；调用方只记录错误，不能在已经提交响应后再次 http.Error。
func writeJSON(
	w http.ResponseWriter,
	status int,
	value any,
) error {
	return errNotImplemented
}

// writeError 把内部错误转换为稳定、不会泄漏实现细节的 JSON 错误响应。
func writeError(w http.ResponseWriter, err error) {
	status, response := mapError(err)
	logWriteError(writeJSON(w, status, response))
}

// mapError 集中维护错误语义到 HTTP 协议的映射。
//
// 实现要点：使用 errors.Is，而不是字符串比较。
//
//   - ErrInvalidRequest、domain.ErrInvalidInput → 400 / invalid_request
//   - store.ErrNotFound → 404 / not_found
//   - store.ErrAlreadyExists、store.ErrRevisionConflict、domain.ErrInvalidTransition
//     → 409 / conflict
//   - 其它错误 → 500 / internal_error
//
// Message 使用固定中文文案，不要把 err.Error()、SQL、文件路径或秘密返回给浏览器。
func mapError(err error) (int, ErrorResponse) {
	return http.StatusNotImplemented, ErrorResponse{
		Code:    "not_implemented",
		Message: errNotImplemented.Error(),
	}
}
