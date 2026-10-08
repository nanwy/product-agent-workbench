package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"

	"product-agent-workbench/internal/domain"
	"product-agent-workbench/internal/scheduler"
	"product-agent-workbench/internal/store"
	"product-agent-workbench/internal/verification"
)

const maxJSONBodyBytes int64 = 1 << 20 // 1 MiB，防止客户端用超大 body 占用本地进程内存。

// ErrInvalidRequest 表示 HTTP body 不是当前 endpoint 接受的单个 JSON 对象。
// 它属于传输层错误，与领域层 domain.ErrInvalidInput 的来源不同，但都会映射为 400。
var ErrInvalidRequest = errors.New("HTTP 请求无效")

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
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	return nil
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
	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("编码 JSON 响应: %w", err)
	}
	payload = append(payload, '\n')

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if _, err := w.Write(payload); err != nil {
		return fmt.Errorf("写入 JSON 响应: %w", err)
	}
	return nil
}

// writeError 把内部错误转换为稳定、不会泄漏实现细节的 JSON 错误响应。
func writeError(w http.ResponseWriter, err error) {
	status, response := mapError(err)
	// 浏览器只能看到稳定错误码；本地服务日志保留包装后的调用链，才能诊断 Codex、SQLite
	// 或文件系统故障。错误对象本身仍不进入 JSON 响应。
	if status == http.StatusInternalServerError {
		log.Printf("HTTP 请求失败: %v", err)
	}
	logWriteError(writeJSON(w, status, response))
}

// mapError 集中维护错误语义到 HTTP 协议的映射。
//
// 实现要点：使用 errors.Is，而不是字符串比较。
//
//   - ErrInvalidRequest、domain.ErrInvalidInput → 400 / invalid_request
//   - store.ErrNotFound、scheduler.ErrTicketVersionNotFound → 404 / not_found
//   - store.ErrAlreadyExists、store.ErrRevisionConflict、domain.ErrInvalidTransition
//     → 409 / conflict
//   - 其它错误 → 500 / internal_error
//
// Message 使用固定中文文案，不要把 err.Error()、SQL、文件路径或秘密返回给浏览器。
func mapError(err error) (int, ErrorResponse) {
	if errors.Is(err, ErrInvalidRequest) || errors.Is(err, domain.ErrInvalidInput) {
		return http.StatusBadRequest, ErrorResponse{
			Code:    "invalid_request",
			Message: "请求无效",
		}
	}
	if errors.Is(err, store.ErrNotFound) ||
		errors.Is(err, scheduler.ErrTicketVersionNotFound) ||
		errors.Is(err, verification.ErrReportNotFound) {
		return http.StatusNotFound, ErrorResponse{
			Code:    "not_found",
			Message: "资源不存在",
		}
	}
	if errors.Is(err, store.ErrAlreadyExists) ||
		errors.Is(err, store.ErrRevisionConflict) ||
		errors.Is(err, store.ErrTaskConflict) ||
		errors.Is(err, domain.ErrInvalidTransition) {
		return http.StatusConflict, ErrorResponse{
			Code:    "conflict",
			Message: "资源冲突",
		}
	}
	return http.StatusInternalServerError, ErrorResponse{
		Code:    "internal_error",
		Message: "服务器内部错误",
	}
}
