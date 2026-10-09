package memory

import (
	"context"

	"product-agent-workbench/internal/domain"
	storecontract "product-agent-workbench/internal/store"
)

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
