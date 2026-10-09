package memory

import (
	"context"

	"product-agent-workbench/internal/domain"
	storecontract "product-agent-workbench/internal/store"
)

// CreateRequirementConversation 用一把写锁提交新需求的三份初始事实。
//
// Memory Store 没有 SQL 事务，但“先完成全部校验，最后集中写 map”具有相同的原子语义。
func (s *Store) CreateRequirementConversation(
	ctx context.Context,
	input storecontract.CreateRequirementConversationInput,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := input.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	requirement := input.Requirement
	conversation := input.Conversation
	message := input.InitialMessage
	if _, exists := s.requirements[requirement.ID]; exists {
		return storecontract.ErrAlreadyExists
	}
	if _, exists := s.conversations[conversation.ID]; exists {
		return storecontract.ErrAlreadyExists
	}
	if _, exists := s.conversationByReq[requirement.ID]; exists {
		return storecontract.ErrAlreadyExists
	}
	if _, exists := s.conversationMessages[message.ID]; exists {
		return storecontract.ErrAlreadyExists
	}

	// 到这里才开始写入：前面的任意失败路径都不会留下孤立实体。
	s.requirements[requirement.ID] = requirement
	s.conversations[conversation.ID] = conversation
	s.conversationByReq[requirement.ID] = conversation.ID
	s.conversationMessages[message.ID] = cloneConversationMessage(message)
	s.messagesByConversation[conversation.ID] = []string{message.ID}
	return nil
}

// CreateConversation 保存 Requirement 唯一的逻辑对话头。
func (s *Store) CreateConversation(ctx context.Context, conversation domain.Conversation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := domain.ValidateConversation(conversation); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, exists := s.requirements[conversation.RequirementID]; !exists {
		return storecontract.ErrNotFound
	}
	if _, exists := s.conversations[conversation.ID]; exists {
		return storecontract.ErrAlreadyExists
	}
	if _, exists := s.conversationByReq[conversation.RequirementID]; exists {
		return storecontract.ErrAlreadyExists
	}
	s.conversations[conversation.ID] = conversation
	s.conversationByReq[conversation.RequirementID] = conversation.ID
	return nil
}

func (s *Store) GetConversationByRequirement(
	ctx context.Context,
	requirementID string,
) (domain.Conversation, error) {
	if err := ctx.Err(); err != nil {
		return domain.Conversation{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return domain.Conversation{}, err
	}
	id, exists := s.conversationByReq[requirementID]
	if !exists {
		return domain.Conversation{}, storecontract.ErrNotFound
	}
	return s.conversations[id], nil
}

func (s *Store) SaveConversation(
	ctx context.Context,
	conversation domain.Conversation,
	expectedRevision int64,
) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err := domain.ValidateConversation(conversation); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	current, exists := s.conversations[conversation.ID]
	if !exists {
		return 0, storecontract.ErrNotFound
	}
	if current.Revision != expectedRevision {
		return 0, storecontract.ErrConversationConflict
	}
	// ID、Requirement 归属和创建时间是身份字段，更新时不能偷偷改写。
	if current.RequirementID != conversation.RequirementID || !current.CreatedAt.Equal(conversation.CreatedAt) {
		return 0, domain.ErrInvalidInput
	}
	conversation.Revision = expectedRevision + 1
	s.conversations[conversation.ID] = conversation
	return conversation.Revision, nil
}

// BeginConversationTurn 原子保存 23A 状态机的返回值和两条页面消息。
func (s *Store) BeginConversationTurn(
	ctx context.Context,
	input storecontract.BeginConversationTurnInput,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := input.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	current, exists := s.conversations[input.Conversation.ID]
	if !exists {
		return storecontract.ErrNotFound
	}
	if current.Revision != input.ExpectedRevision {
		return storecontract.ErrConversationConflict
	}

	// 重新执行纯领域状态机，证明调用方提交的快照确实由当前数据库状态演进而来。
	expectedConversation, expectedTurn, err := current.BeginTurn(
		domain.BeginConversationTurnInput{
			ID:       input.Turn.ID,
			Sequence: input.Turn.Sequence,
		},
		input.Turn.StartedAt,
	)
	if err != nil {
		return err
	}
	if expectedConversation != input.Conversation || expectedTurn != input.Turn {
		return domain.ErrInvalidInput
	}

	turnIDs := s.turnsByConversation[current.ID]
	if input.Turn.Sequence != int64(len(turnIDs)+1) {
		return storecontract.ErrInvalidTurnSequence
	}
	for _, id := range turnIDs {
		if s.conversationTurns[id].Status == domain.ConversationTurnRunning {
			return storecontract.ErrAlreadyExists
		}
	}
	messageIDs := s.messagesByConversation[current.ID]
	nextMessageSequence := int64(len(messageIDs) + 1)
	if input.UserMessage.Sequence != nextMessageSequence {
		return storecontract.ErrInvalidMessageSequence
	}
	if _, exists := s.conversationTurns[input.Turn.ID]; exists {
		return storecontract.ErrAlreadyExists
	}
	if input.UserMessage.ID == input.AssistantMessage.ID {
		return storecontract.ErrAlreadyExists
	}
	if _, exists := s.conversationMessages[input.UserMessage.ID]; exists {
		return storecontract.ErrAlreadyExists
	}
	if _, exists := s.conversationMessages[input.AssistantMessage.ID]; exists {
		return storecontract.ErrAlreadyExists
	}

	// 校验全部完成后再一次性修改内存状态，对应 SQLite 的 Commit。
	s.conversations[current.ID] = input.Conversation
	s.conversationTurns[input.Turn.ID] = cloneConversationTurn(input.Turn)
	s.turnsByConversation[current.ID] = append(turnIDs, input.Turn.ID)
	s.conversationMessages[input.UserMessage.ID] = cloneConversationMessage(input.UserMessage)
	s.conversationMessages[input.AssistantMessage.ID] = cloneConversationMessage(input.AssistantMessage)
	s.messagesByConversation[current.ID] = append(
		messageIDs,
		input.UserMessage.ID,
		input.AssistantMessage.ID,
	)
	return nil
}

// RetryConversationTurn 原子创建新的运行 Turn 和 assistant 占位消息，不复制用户消息。
func (s *Store) RetryConversationTurn(
	ctx context.Context,
	input storecontract.RetryConversationTurnInput,
) error {
	// 第 1 步：锁外校验输入结构；取得写锁后再读取数据库真值。
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := input.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}

	// 第 2 步：用当前 Conversation 重跑状态机，不能相信应用层提交的 running 快照。
	current, exists := s.conversations[input.Conversation.ID]
	if !exists {
		return storecontract.ErrNotFound
	}
	if current.Revision != input.ExpectedRevision {
		return storecontract.ErrConversationConflict
	}
	expectedConversation, expectedTurn, err := current.BeginTurn(domain.BeginConversationTurnInput{
		ID: input.Turn.ID, Sequence: input.Turn.Sequence,
	}, input.Turn.StartedAt)
	if err != nil {
		return err
	}
	if expectedConversation != input.Conversation || expectedTurn != input.Turn {
		return domain.ErrInvalidInput
	}

	// 第 3 步：sequence 必须紧接历史尾部，同一对话不能已有另一条 running Turn。
	turnIDs := s.turnsByConversation[current.ID]
	if input.Turn.Sequence != int64(len(turnIDs)+1) {
		return storecontract.ErrInvalidTurnSequence
	}
	for _, id := range turnIDs {
		if s.conversationTurns[id].Status == domain.ConversationTurnRunning {
			return storecontract.ErrAlreadyExists
		}
	}
	messageIDs := s.messagesByConversation[current.ID]
	if input.AssistantMessage.Sequence != int64(len(messageIDs)+1) {
		return storecontract.ErrInvalidMessageSequence
	}
	if _, exists := s.conversationTurns[input.Turn.ID]; exists {
		return storecontract.ErrAlreadyExists
	}
	if _, exists := s.conversationMessages[input.AssistantMessage.ID]; exists {
		return storecontract.ErrAlreadyExists
	}

	// 第 4 步：所有检查通过后一起修改四个索引，模拟数据库事务的 commit。
	s.conversations[current.ID] = input.Conversation
	s.conversationTurns[input.Turn.ID] = cloneConversationTurn(input.Turn)
	s.turnsByConversation[current.ID] = append(turnIDs, input.Turn.ID)
	s.conversationMessages[input.AssistantMessage.ID] = cloneConversationMessage(input.AssistantMessage)
	s.messagesByConversation[current.ID] = append(messageIDs, input.AssistantMessage.ID)
	return nil
}

// AttachConversationThread 用同一把锁保存对话级和 Turn 级 Thread ID。
func (s *Store) AttachConversationThread(
	ctx context.Context,
	input storecontract.AttachConversationThreadInput,
) error {
	// 第 1 步：进入写锁后读取同一时刻的 Conversation 与 Turn。
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	current, conversationExists := s.conversations[input.Conversation.ID]
	currentTurn, turnExists := s.conversationTurns[input.Turn.ID]
	if !conversationExists || !turnExists {
		return storecontract.ErrNotFound
	}

	// 第 2 步：用当前值重放 AttachThread，确认调用方没有伪造 revision、时间或归属。
	if err := input.ValidateTransition(current, currentTurn); err != nil {
		return err
	}

	// 第 3 步：校验完成后一次性替换两份值，其他 goroutine 看不到只更新一半的状态。
	s.conversations[current.ID] = input.Conversation
	s.conversationTurns[currentTurn.ID] = cloneConversationTurn(input.Turn)
	return nil
}

// FinishConversationTurn 用一把写锁把对话头、Turn 和 assistant 消息一起收口。
//
// Memory Store 没有数据库事务，所以必须先完成全部读取和校验，最后才能修改三个 map。
func (s *Store) FinishConversationTurn(
	ctx context.Context,
	input storecontract.FinishConversationTurnInput,
) error {
	// 第 1 步：锁外完成只依赖输入的校验，减少持有写锁的时间。
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := input.Validate(); err != nil {
		return err
	}

	// 第 2 步：进入单一写锁临界区；从这里开始看到的是同一时刻的内存快照。
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}

	// 第 3 步：读取三个当前值。任意对象不存在都不能写另外两个对象。
	current, conversationExists := s.conversations[input.Conversation.ID]
	currentTurn, turnExists := s.conversationTurns[input.Turn.ID]
	currentMessage, messageExists := s.conversationMessages[input.AssistantMessage.ID]
	if !conversationExists || !turnExists || !messageExists {
		return storecontract.ErrNotFound
	}

	// 第 4 步：重放领域状态机，并检查消息仍是本 Turn 的 streaming assistant 占位项。
	if err := input.ValidateTransition(current, currentTurn); err != nil {
		return err
	}
	if err := input.ValidateCurrentAssistant(currentMessage); err != nil {
		return err
	}
	message := input.AssistantMessage

	// 第 5 步：所有失败路径都已经结束，现在一次性替换三份值。
	// 前端类比：这就是 reducer 计算完成后的单次 commit，中间状态不会被其他 goroutine 看到。
	s.conversations[current.ID] = input.Conversation
	s.conversationTurns[currentTurn.ID] = cloneConversationTurn(input.Turn)
	s.conversationMessages[currentMessage.ID] = cloneConversationMessage(message)
	return nil
}

func (s *Store) CreateConversationTurn(ctx context.Context, turn domain.ConversationTurn) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := domain.ValidateConversationTurn(turn); err != nil {
		return err
	}
	if turn.Status != domain.ConversationTurnRunning {
		return domain.ErrInvalidTransition
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, exists := s.conversations[turn.ConversationID]; !exists {
		return storecontract.ErrNotFound
	}
	if _, exists := s.conversationTurns[turn.ID]; exists {
		return storecontract.ErrAlreadyExists
	}
	ids := s.turnsByConversation[turn.ConversationID]
	if turn.Sequence != int64(len(ids)+1) {
		return storecontract.ErrInvalidTurnSequence
	}
	for _, id := range ids {
		if s.conversationTurns[id].Status == domain.ConversationTurnRunning {
			return storecontract.ErrAlreadyExists
		}
	}
	s.conversationTurns[turn.ID] = cloneConversationTurn(turn)
	s.turnsByConversation[turn.ConversationID] = append(ids, turn.ID)
	return nil
}

func (s *Store) GetConversationTurn(ctx context.Context, id string) (domain.ConversationTurn, error) {
	if err := ctx.Err(); err != nil {
		return domain.ConversationTurn{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return domain.ConversationTurn{}, err
	}
	turn, exists := s.conversationTurns[id]
	if !exists {
		return domain.ConversationTurn{}, storecontract.ErrNotFound
	}
	return cloneConversationTurn(turn), nil
}

func (s *Store) ListConversationTurns(
	ctx context.Context,
	conversationID string,
) ([]domain.ConversationTurn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, exists := s.conversations[conversationID]; !exists {
		return nil, storecontract.ErrNotFound
	}
	ids := s.turnsByConversation[conversationID]
	result := make([]domain.ConversationTurn, 0, len(ids))
	for _, id := range ids {
		result = append(result, cloneConversationTurn(s.conversationTurns[id]))
	}
	return result, nil
}

func (s *Store) SaveConversationTurn(
	ctx context.Context,
	turn domain.ConversationTurn,
	expectedStatus domain.ConversationTurnStatus,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := domain.ValidateConversationTurn(turn); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	current, exists := s.conversationTurns[turn.ID]
	if !exists {
		return storecontract.ErrNotFound
	}
	if current.Status != expectedStatus {
		return storecontract.ErrConversationTurnConflict
	}
	if current.ConversationID != turn.ConversationID || current.Sequence != turn.Sequence ||
		current.Phase != turn.Phase || !current.StartedAt.Equal(turn.StartedAt) {
		return domain.ErrInvalidInput
	}
	s.conversationTurns[turn.ID] = cloneConversationTurn(turn)
	return nil
}

func (s *Store) AppendConversationMessages(
	ctx context.Context,
	messages []domain.ConversationMessage,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(messages) == 0 {
		return domain.ErrInvalidInput
	}
	for _, message := range messages {
		if err := domain.ValidateConversationMessage(message); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	conversationID := messages[0].ConversationID
	if _, exists := s.conversations[conversationID]; !exists {
		return storecontract.ErrNotFound
	}
	ids := s.messagesByConversation[conversationID]
	nextSequence := int64(len(ids) + 1)
	seen := make(map[string]struct{}, len(messages))
	for index, message := range messages {
		if message.ConversationID != conversationID || message.Sequence != nextSequence+int64(index) {
			return storecontract.ErrInvalidMessageSequence
		}
		if _, exists := s.conversationMessages[message.ID]; exists {
			return storecontract.ErrAlreadyExists
		}
		if _, exists := seen[message.ID]; exists {
			return storecontract.ErrAlreadyExists
		}
		seen[message.ID] = struct{}{}
		if message.TurnID != "" {
			turn, exists := s.conversationTurns[message.TurnID]
			if !exists || turn.ConversationID != conversationID {
				return storecontract.ErrNotFound
			}
		}
	}
	for _, message := range messages {
		s.conversationMessages[message.ID] = cloneConversationMessage(message)
		s.messagesByConversation[conversationID] = append(
			s.messagesByConversation[conversationID],
			message.ID,
		)
	}
	return nil
}

func (s *Store) SaveConversationMessage(
	ctx context.Context,
	message domain.ConversationMessage,
	expectedStatus domain.ConversationMessageStatus,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := domain.ValidateConversationMessage(message); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	current, exists := s.conversationMessages[message.ID]
	if !exists {
		return storecontract.ErrNotFound
	}
	if current.Status != expectedStatus {
		return storecontract.ErrConversationMessageConflict
	}
	if current.ConversationID != message.ConversationID || current.TurnID != message.TurnID ||
		current.Sequence != message.Sequence || current.Role != message.Role ||
		current.Phase != message.Phase || !current.RecordedAt.Equal(message.RecordedAt) {
		return domain.ErrInvalidInput
	}
	s.conversationMessages[message.ID] = cloneConversationMessage(message)
	return nil
}

func (s *Store) ListConversationMessages(
	ctx context.Context,
	conversationID string,
	afterSequence int64,
) ([]domain.ConversationMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if afterSequence < 0 {
		return nil, domain.ErrInvalidInput
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, exists := s.conversations[conversationID]; !exists {
		return nil, storecontract.ErrNotFound
	}
	ids := s.messagesByConversation[conversationID]
	result := make([]domain.ConversationMessage, 0, len(ids))
	for _, id := range ids {
		message := s.conversationMessages[id]
		if message.Sequence > afterSequence {
			result = append(result, cloneConversationMessage(message))
		}
	}
	return result, nil
}

func cloneConversationTurn(value domain.ConversationTurn) domain.ConversationTurn {
	if value.FinishedAt != nil {
		finishedAt := *value.FinishedAt
		value.FinishedAt = &finishedAt
	}
	return value
}

func cloneConversationMessage(value domain.ConversationMessage) domain.ConversationMessage {
	value.Segments = append([]domain.ConversationSegment{}, value.Segments...)
	return value
}
