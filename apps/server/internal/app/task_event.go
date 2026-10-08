package app

import (
	"context"

	"product-agent-workbench/internal/domain"
)

const maxTaskEventReplayLimit = 1000

// ReplayTaskEventsInput 描述浏览器从哪个全局事件 ID 之后继续读取。
//
// 前端类比：AfterID 是 cursor，Limit 是 pageSize。这里的 ID 属于所有 TaskRun 共用的
// 时间线，不能传某个 TaskRun 内部的 Sequence。
type ReplayTaskEventsInput struct {
	AfterID int64
	Limit   int
}

// TaskEventReplay 是一次可恢复事件分页的结果。
type TaskEventReplay struct {
	Events        []domain.TaskEvent
	OldestID      int64
	LatestID      int64
	ResetRequired bool
}

// ReplayTaskEvents 读取 SSE 游标之后的一页持久化事件。
//
// ResetRequired 表示浏览器保存的游标已经不属于当前数据库：可能早于保留窗口，也可能
// 来自一个已被重建的数据库。HTTP 层会发 replay-reset 事件，让前端重新拉取页面快照，
// 而不是悄悄漏掉中间状态。
func (a *Application) ReplayTaskEvents(
	ctx context.Context,
	input ReplayTaskEventsInput,
) (TaskEventReplay, error) {
	if err := ctx.Err(); err != nil {
		return TaskEventReplay{}, err
	}
	if input.AfterID < 0 || input.Limit < 1 || input.Limit > maxTaskEventReplayLimit {
		return TaskEventReplay{}, domain.ErrInvalidInput
	}

	oldestID, latestID, err := a.store.TaskEventIDBounds(ctx)
	if err != nil {
		return TaskEventReplay{}, err
	}
	replay := TaskEventReplay{
		Events:   make([]domain.TaskEvent, 0),
		OldestID: oldestID,
		LatestID: latestID,
	}

	// AfterID=0 表示首次订阅，应该从当前最早事件开始；非零游标才需要检查断档。
	if input.AfterID > 0 && (input.AfterID > latestID || (oldestID > 0 && input.AfterID < oldestID-1)) {
		replay.ResetRequired = true
		return replay, nil
	}

	events, err := a.store.ListTaskEventsAfterID(ctx, input.AfterID, input.Limit)
	if err != nil {
		return TaskEventReplay{}, err
	}
	replay.Events = events
	// bounds 与列表查询之间可能刚好写入新事件。把实际读到的最后 ID 纳入结果，避免
	// HTTP 层返回比本页数据更旧的 latestEventId。
	if len(events) > 0 && events[len(events)-1].ID > replay.LatestID {
		replay.LatestID = events[len(events)-1].ID
	}
	return replay, nil
}
