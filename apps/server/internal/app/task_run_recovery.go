package app

import (
	"context"
	"fmt"

	"product-agent-workbench/internal/domain"
)

const processRestartFailureReason = "服务进程在任务执行期间退出"

// RecoverInterruptedTaskRuns 把上次进程遗留的 claimed/running 记录收敛为 failed。
//
// 前端类比：页面刷新后，不能让一直停在 loading 的请求永远占着按钮。这里读取所有
// “已经开始但没有终态”的持久化记录，统一补上可解释的失败终态。它不会自动重跑，因为
// 外部执行器可能已经产生部分副作用；后续重试必须创建新的 attempt。
func (app *Application) RecoverInterruptedTaskRuns(ctx context.Context) ([]domain.TaskRun, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	inFlight, err := app.store.ListInFlightTasks(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取中断的 TaskRun: %w", err)
	}
	if len(inFlight) == 0 {
		return []domain.TaskRun{}, nil
	}

	// 同一次恢复使用同一个时间点，便于审计，也避免循环中系统时钟变化造成伪顺序。
	recoveredAt := app.now()
	recovered := make([]domain.TaskRun, 0, len(inFlight))
	for _, run := range inFlight {
		if err := ctx.Err(); err != nil {
			return recovered, err
		}
		failed, err := run.Fail(processRestartFailureReason, recoveredAt)
		if err != nil {
			return recovered, fmt.Errorf("恢复 TaskRun %q: %w", run.ID, err)
		}
		if err := app.store.SaveTask(ctx, failed, run.Status); err != nil {
			return recovered, fmt.Errorf("保存恢复后的 TaskRun %q: %w", run.ID, err)
		}
		recovered = append(recovered, failed)
	}
	return recovered, nil
}
