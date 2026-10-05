package sqlite

import (
	"database/sql"
	"fmt"
	"product-agent-workbench/internal/domain"
	"time"
)

// 本文件以后集中放 SQL 行到领域对象的还原逻辑。
//
// `Scan` 是 database/sql 把查询列值写入 Go 变量的方法；`scanTaskRun`
// 则是我们将要自定义的辅助函数，不是标准库 API。开始前先阅读同目录 README.md。
//
// 开始写查询前先确定并记录：
//
//   - 时间统一保存为 UTC Unix 纳秒 int64；
//   - ClaimedAt/StartedAt/FinishedAt 如何用 sql.NullInt64 接收 NULL；
//   - TaskResult 的三个字段如何保证“整体存在或整体不存在”；
//   - 从数据库读出的 status、时间组合如何再次验证领域不变量。
//
// 不要把同一套 Scan 参数复制到多个查询里；列顺序一旦变化会产生难发现的数据错位。
//
// 建议先完成三个小辅助函数，再写 GetTask/SaveTask：
//
//   - nullableUnixNano(*time.Time) any：nil 返回 nil，否则返回 UTC UnixNano；
//   - nullableResult(*domain.TaskResult) (any, any, any)：nil 时三列都返回 nil；
//   - scanTaskRun(scanner)：集中 Scan，再把 sql.NullInt64/sql.NullString 还原为指针。
//
// scanner 可以定义为只含 `Scan(dest ...any) error` 的小接口，这样 *sql.Row 和以后的
// *sql.Rows 都能复用同一个 scanTaskRun。

type rowScanner interface {
	Scan(dest ...any) error
}

func scanTaskRun(scanner rowScanner) (domain.TaskRun, error) {
	var run domain.TaskRun
	var status string
	var createdAt int64
	var claimedAt, startedAt, finishedAt sql.NullInt64
	var resultSummary, codexThreadID, worktreePath, failureReason sql.NullString

	err := scanner.Scan(
		&run.ID,
		&run.RequirementID,
		&run.TicketID,
		&run.Attempt,
		&status,
		&createdAt,
		&claimedAt,
		&startedAt,
		&finishedAt,
		&resultSummary,
		&codexThreadID,
		&worktreePath,
		&failureReason,
	)
	if err != nil {
		return domain.TaskRun{}, err
	}
	run.Status = domain.TaskStatus(status)
	run.CreatedAt = time.Unix(0, createdAt).UTC()

	run.ClaimedAt = timeFromNullInt64(claimedAt)
	run.StartedAt = timeFromNullInt64(startedAt)
	run.FinishedAt = timeFromNullInt64(finishedAt)

	switch {
	case !resultSummary.Valid && !codexThreadID.Valid && !worktreePath.Valid:
		run.Result = nil
	case resultSummary.Valid && codexThreadID.Valid && worktreePath.Valid:
		run.Result = &domain.TaskResult{
			Summary:       resultSummary.String,
			CodexThreadID: codexThreadID.String,
			WorktreePath:  worktreePath.String,
		}
	default:
		return domain.TaskRun{}, fmt.Errorf(
			"TaskRun %q 的结果字段不完整",
			run.ID,
		)
	}

	if failureReason.Valid {
		run.FailureReason = failureReason.String
	}

	return run, nil
}

func timeFromNullInt64(value sql.NullInt64) *time.Time {
	if !value.Valid {
		return nil
	}

	parsed := time.Unix(0, value.Int64).UTC()
	return &parsed
}

func nullableUnixNano(value *time.Time) any {
	if value == nil {
		return nil
	}

	return value.UTC().UnixNano()
}

func resultColumns(result *domain.TaskResult) (any, any, any) {
	if result == nil {
		return nil, nil, nil
	}

	return result.Summary, result.CodexThreadID, result.WorktreePath
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}

	return value
}
