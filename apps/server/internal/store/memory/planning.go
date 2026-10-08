package memory

import (
	"context"
	"time"

	"product-agent-workbench/internal/domain"
	storecontract "product-agent-workbench/internal/store"
)

// ConfirmGrilling 把 Requirement 和共同理解放在同一把写锁内提交。
// 前端类比：这相当于一个 mutation 同时更新两个 normalized entity，不能先成功一个再失败一个。
func (s *Store) ConfirmGrilling(
	ctx context.Context,
	requirement domain.Requirement,
	expectedRevision int64,
	understanding domain.GrillingUnderstanding,
) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, exists := s.requirements[requirement.ID]
	if !exists {
		return 0, storecontract.ErrNotFound
	}
	if current.Revision != expectedRevision {
		return 0, storecontract.ErrRevisionConflict
	}
	if _, exists := s.understandings[requirement.ID]; exists {
		return 0, storecontract.ErrAlreadyExists
	}
	requirement.Revision = expectedRevision + 1
	s.requirements[requirement.ID] = requirement
	s.understandings[requirement.ID] = understanding
	return requirement.Revision, nil
}

func (s *Store) GetGrillingUnderstanding(
	ctx context.Context,
	requirementID string,
) (domain.GrillingUnderstanding, error) {
	if err := ctx.Err(); err != nil {
		return domain.GrillingUnderstanding{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	understanding, exists := s.understandings[requirementID]
	if !exists {
		return domain.GrillingUnderstanding{}, storecontract.ErrNotFound
	}
	return understanding, nil
}

func (s *Store) CreateSpecVersion(
	ctx context.Context,
	requirement domain.Requirement,
	expectedRevision int64,
	version domain.SpecVersion,
) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, exists := s.requirements[requirement.ID]
	if !exists {
		return 0, storecontract.ErrNotFound
	}
	if current.Revision != expectedRevision {
		return 0, storecontract.ErrRevisionConflict
	}
	versions := s.specVersions[requirement.ID]
	for index := range versions {
		if versions[index].ID == version.ID || versions[index].Version == version.Version {
			return 0, storecontract.ErrAlreadyExists
		}
		if versions[index].Status == domain.SpecVersionDraft {
			versions[index].Status = domain.SpecVersionSuperseded
		}
	}
	requirement.Revision = expectedRevision + 1
	s.requirements[requirement.ID] = requirement
	s.specVersions[requirement.ID] = append(versions, cloneSpecVersion(version))
	return requirement.Revision, nil
}

func (s *Store) ListSpecVersions(ctx context.Context, requirementID string) ([]domain.SpecVersion, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, exists := s.requirements[requirementID]; !exists {
		return nil, storecontract.ErrNotFound
	}
	versions := s.specVersions[requirementID]
	result := make([]domain.SpecVersion, 0, len(versions))
	for _, version := range versions {
		result = append(result, cloneSpecVersion(version))
	}
	return result, nil
}

func (s *Store) ConfirmSpecVersion(
	ctx context.Context,
	requirement domain.Requirement,
	expectedRevision int64,
	versionID string,
	confirmedAt time.Time,
) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, exists := s.requirements[requirement.ID]
	if !exists {
		return 0, storecontract.ErrNotFound
	}
	if current.Revision != expectedRevision {
		return 0, storecontract.ErrRevisionConflict
	}
	versions := s.specVersions[requirement.ID]
	found := false
	for index := range versions {
		if versions[index].ID != versionID || versions[index].Status != domain.SpecVersionDraft {
			continue
		}
		value := confirmedAt.UTC()
		versions[index].Status = domain.SpecVersionConfirmed
		versions[index].ConfirmedAt = &value
		found = true
	}
	if !found {
		return 0, storecontract.ErrNotFound
	}
	requirement.Revision = expectedRevision + 1
	s.requirements[requirement.ID] = requirement
	s.specVersions[requirement.ID] = versions
	return requirement.Revision, nil
}

func cloneSpecVersion(value domain.SpecVersion) domain.SpecVersion {
	value.Document.UserStories = append([]string(nil), value.Document.UserStories...)
	value.Document.ImplementationDecisions = append([]string(nil), value.Document.ImplementationDecisions...)
	value.Document.TestingDecisions = append([]string(nil), value.Document.TestingDecisions...)
	value.Document.OutOfScope = append([]string(nil), value.Document.OutOfScope...)
	value.Document.FurtherNotes = append([]string(nil), value.Document.FurtherNotes...)
	if value.ConfirmedAt != nil {
		confirmedAt := *value.ConfirmedAt
		value.ConfirmedAt = &confirmedAt
	}
	return value
}
