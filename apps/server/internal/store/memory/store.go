package memory

import (
	"context"
	"strings"
	"sync"
	"time"

	"product-agent-workbench/internal/domain"
	storecontract "product-agent-workbench/internal/store"
)

// Store 是并发安全的内存 Adapter。
//
// 一把 RWMutex 共同保护下面所有字段。这里故意不拆多把锁：SaveRequirement、
// ClaimNextTask 和 AppendTaskEvents 都需要跨多个读写步骤维持原子性，先保证正确，
// 再根据真实 profiling 结果考虑更细的锁粒度。
type Store struct {
	mu sync.RWMutex

	requirements map[string]domain.Requirement
	tasks        map[string]domain.TaskRun
	taskOrder    []string
	eventsByTask map[string][]domain.TaskEvent
	// events 按全局 ID 保存所有 TaskRun 的事件。前端类比：eventsByTask 是详情页索引，
	// events 是消息中心的总时间线；SSE 的 Last-Event-ID 只能查询后者。
	events      []domain.TaskEvent
	nextEventID int64
}

// 编译期检查：当 Store interface 变化时，这一行会立即指出 Adapter 缺少的方法。
var _ storecontract.Store = (*Store)(nil)

// New 创建一个内部集合均已初始化的空 Store。
//
// map 必须在构造阶段初始化；nil map 可以读取，却会在第一次写入时 panic。
func New() *Store {
	return &Store{
		requirements: make(map[string]domain.Requirement),
		tasks:        make(map[string]domain.TaskRun),
		taskOrder:    []string{},
		eventsByTask: make(map[string][]domain.TaskEvent),
		events:       []domain.TaskEvent{},
	}
}

// CreateRequirement 首次保存 Requirement。
func (s *Store) CreateRequirement(ctx context.Context, requirement domain.Requirement) error {
	// 实现要点：
	// 1. 先检查 ctx.Err()，已取消的调用不能产生写入。
	// 2. 获取写锁并 defer 解锁。
	// 3. 检查 requirement.ID 是否已存在；存在则返回 storecontract.ErrAlreadyExists。
	// 4. 保存值。Requirement 当前没有引用字段，直接赋值就是副本。
	if ctx.Err() != nil {
		return ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		return ctx.Err()
	}

	if _, exists := s.requirements[requirement.ID]; exists {
		return storecontract.ErrAlreadyExists
	}

	s.requirements[requirement.ID] = requirement
	return nil
}

// GetRequirement 返回 Requirement 的值副本。
func (s *Store) GetRequirement(ctx context.Context, id string) (domain.Requirement, error) {
	// 实现要点：
	// 1. 先检查 ctx.Err()。
	// 2. 使用读锁，不修改任何共享状态。
	// 3. 利用 map 的 value, ok 形式区分“不存在”和 Requirement 零值。
	if ctx.Err() != nil {
		return domain.Requirement{}, ctx.Err()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	if ctx.Err() != nil {
		return domain.Requirement{}, ctx.Err()
	}

	requirement, exists := s.requirements[id]
	if !exists {
		return domain.Requirement{}, storecontract.ErrNotFound
	}
	return requirement, nil
}

// SaveRequirement 使用 expectedRevision 执行乐观锁更新。
func (s *Store) SaveRequirement(
	ctx context.Context,
	requirement domain.Requirement,
	expectedRevision int64,
) (int64, error) {
	// 实现要点：整个“读取旧值 → 比较版本 → 计算新版本 → 写回”只能使用一段写锁。
	// 如果先用 RLock 检查、释放后再用 Lock 写入，两个并发请求可能同时通过检查，
	// 造成 lost update。
	//
	// 建议步骤：
	// 1. 检查 ctx.Err()。
	// 2. 获取写锁。
	// 3. 按 requirement.ID 查旧值，不存在返回 ErrNotFound。
	// 4. 旧值 Revision 不等于 expectedRevision 时返回 ErrRevisionConflict。
	// 5. 把传入值的 Revision 设置为 expectedRevision+1，再写回并返回新版本。
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}

	oldRequirement, exists := s.requirements[requirement.ID]

	if !exists {
		return 0, storecontract.ErrNotFound
	}

	if oldRequirement.Revision != expectedRevision {
		return 0, storecontract.ErrRevisionConflict
	}

	requirement.Revision = expectedRevision + 1
	s.requirements[requirement.ID] = requirement

	return requirement.Revision, nil
}

// EnqueueTask 首次保存 queued TaskRun，并记录稳定的入队顺序。
func (s *Store) EnqueueTask(ctx context.Context, run domain.TaskRun) error {
	// 实现要点：
	// 1. 检查 context，并获取写锁。
	// 2. 重复 ID、同一 Ticket 的重复 attempt 或第二条活动记录返回 ErrAlreadyExists。
	// 3. 不同 Ticket 即使属于同一 Requirement，也允许同时 queued。
	// 4. 只接受 queued 状态；非法状态返回 domain.ErrInvalidTransition。
	// 5. 使用 cloneTaskRun 保存深拷贝。
	// 6. 仅在成功写入 map 后，把 ID append 到 taskOrder；失败路径不能污染顺序。
	if ctx.Err() != nil {
		return ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	// 获取锁之后再次检查，避免调用在等待锁期间被取消却仍然写入。
	if ctx.Err() != nil {
		return ctx.Err()
	}

	if _, exists := s.tasks[run.ID]; exists {
		return storecontract.ErrAlreadyExists
	}
	if run.Status != domain.TaskStatusQueued {
		return domain.ErrInvalidTransition
	}
	if isBlank(run.TicketVersionID) {
		return domain.ErrInvalidInput
	}
	for _, existing := range s.tasks {
		if existing.RequirementID != run.RequirementID || existing.TicketID != run.TicketID {
			continue
		}
		if existing.TicketVersionID == run.TicketVersionID && existing.Attempt == run.Attempt {
			return storecontract.ErrAlreadyExists
		}
		// 同一 Requirement 的同名 Ticket 即使来自不同版本，也不能同时活动。
		// 确认新版本前应先处理旧版本仍在执行的工作。
		if isActiveTaskStatus(existing.Status) {
			return storecontract.ErrAlreadyExists
		}
	}

	// map 和顺序切片由同一把锁保护，并且只在全部校验成功后一起更新。
	s.tasks[run.ID] = cloneTaskRun(run)
	s.taskOrder = append(s.taskOrder, run.ID)
	return nil
}

// GetTask 返回 TaskRun 的深拷贝。
func (s *Store) GetTask(ctx context.Context, id string) (domain.TaskRun, error) {
	// 实现要点：检查 context，使用读锁查找，然后通过 cloneTaskRun 返回深拷贝。
	if ctx.Err() != nil {
		return domain.TaskRun{}, ctx.Err()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	// 等待读锁期间 context 仍可能取消，因此取得锁后再检查一次。
	if ctx.Err() != nil {
		return domain.TaskRun{}, ctx.Err()
	}

	run, exists := s.tasks[id]
	if !exists {
		return domain.TaskRun{}, storecontract.ErrNotFound
	}
	return cloneTaskRun(run), nil
}

// ListTasksByTicketVersion 返回指定执行票版本的完整 TaskRun 历史。
func (s *Store) ListTasksByTicketVersion(
	ctx context.Context,
	requirementID string,
	ticketVersionID string,
) ([]domain.TaskRun, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if _, exists := s.requirements[requirementID]; !exists {
		return nil, storecontract.ErrNotFound
	}

	runs := make([]domain.TaskRun, 0)
	for _, id := range s.taskOrder {
		run, exists := s.tasks[id]
		if !exists || run.RequirementID != requirementID || run.TicketVersionID != ticketVersionID {
			continue
		}
		runs = append(runs, cloneTaskRun(run))
	}
	return runs, nil
}

// ListInFlightTasks 返回 claimed/running TaskRun，供启动恢复使用。
func (s *Store) ListInFlightTasks(ctx context.Context) ([]domain.TaskRun, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	runs := make([]domain.TaskRun, 0)
	for _, id := range s.taskOrder {
		run, exists := s.tasks[id]
		if !exists || (run.Status != domain.TaskStatusClaimed && run.Status != domain.TaskStatusRunning) {
			continue
		}
		runs = append(runs, cloneTaskRun(run))
	}
	return runs, nil
}

// ClaimNextTask 原子领取最早入队、仍处于 queued 状态的任务。
func (s *Store) ClaimNextTask(ctx context.Context, now time.Time) (domain.TaskRun, error) {
	// 实现要点：
	// 1. 检查 context，并直接获取写锁。
	// 2. 按 taskOrder 顺序遍历 ID，跳过已经不是 queued 的任务。
	// 3. 在锁内调用 run.Claim(now)；领域错误原样返回。
	// 4. 把迁移后的值写回 map，再返回深拷贝。
	//
	// 不要先调用 GetTask 再调用 SaveTask：那会把一次原子操作拆成两段，两个 goroutine
	// 可能领取到同一个任务。
	if ctx.Err() != nil {
		return domain.TaskRun{}, ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		return domain.TaskRun{}, ctx.Err()
	}

	for _, id := range s.taskOrder {
		run, exists := s.tasks[id]
		if !exists || run.Status != domain.TaskStatusQueued {
			continue
		}
		claimed, err := run.Claim(now)
		if err != nil {
			return domain.TaskRun{}, err
		}

		// Claim 是值接收者，必须保存它返回的新值；原 run 仍然是 queued。
		s.tasks[id] = cloneTaskRun(claimed)
		return cloneTaskRun(claimed), nil
	}

	return domain.TaskRun{}, storecontract.ErrNotFound
}

// SaveTask 使用状态 CAS 更新已存在的 TaskRun。
func (s *Store) SaveTask(ctx context.Context, run domain.TaskRun, expectedStatus domain.TaskStatus) error {
	// 实现要点：检查 context，获取写锁，在同一临界区确认 ID 和旧状态，再保存深拷贝。
	// 这个方法不执行状态迁移；迁移规则属于 domain.TaskRun。expectedStatus 只防止旧快照
	// 覆盖已经发生的并发迁移。
	if ctx.Err() != nil {
		return ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if ctx.Err() != nil {
		return ctx.Err()
	}

	current, exists := s.tasks[run.ID]
	if !exists {
		return storecontract.ErrNotFound
	}
	if current.Status != expectedStatus {
		return storecontract.ErrTaskConflict
	}

	s.tasks[run.ID] = cloneTaskRun(run)
	return nil
}

// AppendTaskEvents 原子追加一批连续事件。
func (s *Store) AppendTaskEvents(ctx context.Context, events []domain.TaskEvent) (int64, error) {
	// 实现要点：先验证整个批次，再修改任何字段，保证 all-or-nothing。
	//
	// 建议在写锁内按顺序完成：
	// 1. 空批次直接返回当前 nextEventID，不能制造新事件。
	// 2. 所有事件必须属于同一个已存在的 TaskRun。
	// 3. 根据已有最后一条事件算出 expectedSequence；没有历史时从 1 开始。
	// 4. 验证批次的 TaskRunID、Sequence 和必要字段。任何失败都立即返回，且此时
	//    eventsByTask 与 nextEventID 都还没有改变。
	// 5. 验证全部通过后，再逐条增加 nextEventID、填写 ID、深拷贝 Payload 并追加。

	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if ctx.Err() != nil {
		return 0, ctx.Err()
	}

	if len(events) == 0 {
		return s.nextEventID, nil
	}

	taskRunID := events[0].TaskRunID
	if _, exists := s.tasks[taskRunID]; !exists {
		return 0, storecontract.ErrNotFound
	}

	lastEvents := s.eventsByTask[taskRunID]
	expectedSequence := int64(1)
	if len(lastEvents) > 0 {
		expectedSequence = lastEvents[len(lastEvents)-1].Sequence + 1
	}
	for i, event := range events {
		if event.TaskRunID != taskRunID || event.Sequence != expectedSequence+int64(i) {
			return 0, storecontract.ErrInvalidEventSequence
		}
		if isBlank(event.Type) || event.CreatedAt.IsZero() {
			return 0, domain.ErrInvalidInput
		}
	}

	for _, event := range events {
		s.nextEventID++

		// 不修改调用方传入的 event；Store 保存拥有独立 Payload 的副本。
		storedEvent := cloneTaskEvent(event)
		storedEvent.ID = s.nextEventID
		s.eventsByTask[taskRunID] = append(s.eventsByTask[taskRunID], storedEvent)
		s.events = append(s.events, cloneTaskEvent(storedEvent))
	}

	return s.nextEventID, nil
}

// ListTaskEvents 返回指定序号之后的事件深拷贝。
func (s *Store) ListTaskEvents(
	ctx context.Context,
	taskRunID string,
	afterSequence int64,
) ([]domain.TaskEvent, error) {
	// 实现要点：
	// 1. 检查 context，使用读锁。
	// 2. TaskRun 不存在时返回 ErrNotFound。
	// 3. 创建非 nil 空切片；筛选 Sequence > afterSequence 的事件。
	// 4. 每条事件都通过 cloneTaskEvent 拷贝，不能把内部 Payload 切片交给调用方。
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	// 等待读锁期间 context 仍可能取消，因此取得锁后再检查一次。
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if _, exists := s.tasks[taskRunID]; !exists {
		return nil, storecontract.ErrNotFound
	}

	storedEvents := s.eventsByTask[taskRunID]
	events := make([]domain.TaskEvent, 0, len(storedEvents))
	for _, event := range storedEvents {
		if event.Sequence <= afterSequence {
			continue
		}
		events = append(events, cloneTaskEvent(event))
	}

	return events, nil
}

// TaskEventIDBounds 返回全局事件时间线当前可读取的首尾 ID。
func (s *Store) TaskEventIDBounds(ctx context.Context) (int64, int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, 0, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return 0, 0, err
	}
	if len(s.events) == 0 {
		return 0, 0, nil
	}
	return s.events[0].ID, s.events[len(s.events)-1].ID, nil
}

// ListTaskEventsAfterID 返回全局游标之后的一页事件。
func (s *Store) ListTaskEventsAfterID(
	ctx context.Context,
	afterID int64,
	limit int,
) ([]domain.TaskEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if afterID < 0 || limit < 1 {
		return nil, domain.ErrInvalidInput
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	events := make([]domain.TaskEvent, 0, min(limit, len(s.events)))
	for _, event := range s.events {
		if event.ID <= afterID {
			continue
		}
		events = append(events, cloneTaskEvent(event))
		if len(events) == limit {
			break
		}
	}
	return events, nil
}

// cloneTaskRun 创建 TaskRun 的深拷贝。
func cloneTaskRun(run domain.TaskRun) domain.TaskRun {
	// 实现要点：结构体赋值只完成浅拷贝。分别复制 ClaimedAt、StartedAt、FinishedAt
	// 和 Result 指向的值，再把新地址放进副本。nil 指针保持 nil。

	copyRun := run // 先浅拷贝结构体

	if run.ClaimedAt != nil {
		claimedAt := *run.ClaimedAt
		copyRun.ClaimedAt = &claimedAt
	}
	if run.StartedAt != nil {
		startedAt := *run.StartedAt
		copyRun.StartedAt = &startedAt
	}
	if run.FinishedAt != nil {
		finishedAt := *run.FinishedAt
		copyRun.FinishedAt = &finishedAt
	}
	if run.Result != nil {
		result := *run.Result
		copyRun.Result = &result
	}
	return copyRun
}

// cloneTaskEvent 创建 TaskEvent 的深拷贝。
func cloneTaskEvent(event domain.TaskEvent) domain.TaskEvent {
	// 实现要点：使用 append([]byte(nil), event.Payload...) 或 slices.Clone，避免调用方
	// 修改返回的 Payload 时篡改 Store 内部事件。

	copyEvent := event // 先浅拷贝结构体
	copyEvent.Payload = append([]byte(nil), event.Payload...)

	return copyEvent
}

func isBlank(value string) bool {
	return strings.TrimSpace(value) == ""
}

func isActiveTaskStatus(status domain.TaskStatus) bool {
	return status == domain.TaskStatusQueued ||
		status == domain.TaskStatusClaimed ||
		status == domain.TaskStatusRunning
}
