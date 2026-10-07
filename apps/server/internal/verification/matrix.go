package verification

import (
	"fmt"
	"strings"
)

// BuildMatrix 把证据采集 Adapter 返回的原始数组转换成可以展示和判断门禁的 Matrix。
//
// 前端可以先把目标形状理解成：
//
//	function buildMatrix(evidence: Evidence[]): Matrix {
//	  const records: Evidence[] = []
//	  const seenIDs = new Set<string>()
//	  for (const item of evidence) {
//	    validateEvidence(item, seenIDs)
//	    records.push({ ...item })
//	  }
//	  return { records }
//	}
//
// 不要逐字符翻译 TypeScript；按下面的 Go 步骤实现：
//
//  1. 用 make 创建长度为 0、容量为 len(evidence) 的 records 切片；
//  2. 用 map[string]struct{} 创建 seenIDs 集合；Go 没有内置 Set，空结构体只表示“存在”；
//  3. 用 for range 依次取得每条 item；
//  4. 调用 validateEvidence。出错时立即返回空 Matrix，并用 %w 包装错误；
//  5. 检查 seenIDs，重复 ID 返回 ErrInvalidEvidence，否则把 ID 写入集合；
//  6. append item 到 records。Evidence 当前只有值类型字段，append 得到安全的值拷贝；
//  7. 循环结束后返回 Matrix{Records: records}, nil。
//
// 这是纯函数：不能执行命令、访问 SQLite、修改输入切片或读取当前时间。
func BuildMatrix(evidence []Evidence) (Matrix, error) {
	records := make([]Evidence, 0, len(evidence))
	seenIDs := make(map[string]struct{}, len(evidence))
	for _, item := range evidence {
		// 类似 Zod 的 transform：先把字符串规范化，再校验和保存。否则 "check-1"
		// 与 " check-1 " 会绕过去重，并把不稳定 ID 带进后续 UI key 和持久化结果。
		item.ID = strings.TrimSpace(item.ID)
		item.Label = strings.TrimSpace(item.Label)
		item.Reason = strings.TrimSpace(item.Reason)
		if err := validateEvidence(item); err != nil {
			return Matrix{}, fmt.Errorf("验证证据 %q: %w", item.ID, err)
		}
		if _, exists := seenIDs[item.ID]; exists {
			return Matrix{}, fmt.Errorf("重复的证据 ID %q: %w", item.ID, ErrInvalidEvidence)
		}
		seenIDs[item.ID] = struct{}{}
		records = append(records, item)
	}
	return Matrix{Records: records}, nil
}

// BlocksDelivery 判断当前矩阵是否存在阻止交付的记录。
//
// 前端对应代码是：
//
//	matrix.records.some(
//	  item => item.required && item.status !== "verified"
//	)
//
// Go 的写法不是链式 some，而是 for range：遇到第一条阻塞记录就 return true；循环结束
// 仍未返回，说明没有阻塞项，此时 return false。注意 not-run 也不是 verified，所以必要项
// 为 not-run 时同样阻止交付。
func (m Matrix) BlocksDelivery() bool {
	for _, item := range m.Records {
		if item.Required && item.Status != StatusVerified {
			return true
		}
	}
	return false
}

// validateEvidence 只校验“一行证据”，不要在这里遍历整个 Matrix。
//
// 前端类比：它相当于 Zod schema 对单个对象的 refine，而重复 ID 属于数组级规则，应该
// 留在 BuildMatrix。建议按以下顺序使用 early return：
//
//  1. strings.TrimSpace 后的 ID 和 Label 都不能为空；
//  2. 用 switch item.Status，只接受三个 Status 常量；
//  3. StatusNotRun 时，trim 后的 Reason 不能为空；
//  4. 以上都通过才 return nil。
//
// 返回错误时使用 fmt.Errorf("%w: ...", ErrInvalidEvidence)，这样调用方既能读懂具体原因，
// 又能通过 errors.Is(err, ErrInvalidEvidence) 做稳定判断。
func validateEvidence(item Evidence) error {
	if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.Label) == "" {
		return fmt.Errorf("%w: ID 和 Label 不能为空", ErrInvalidEvidence)
	}
	switch item.Status {
	case StatusVerified, StatusNotRun, StatusFailed:
		// 合法状态，继续检查
	default:
		return fmt.Errorf("%w: 未知状态 %q", ErrInvalidEvidence, item.Status)
	}
	if item.Status == StatusNotRun && strings.TrimSpace(item.Reason) == "" {
		return fmt.Errorf("%w: not-run 状态必须填写 Reason", ErrInvalidEvidence)
	}
	return nil
}
