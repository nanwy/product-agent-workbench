package memory

import (
	"context"
	"time"

	"product-agent-workbench/internal/domain"
	storecontract "product-agent-workbench/internal/store"
)

func (s *Store) CreateTicketVersion(
	ctx context.Context,
	requirement domain.Requirement,
	expectedRevision int64,
	version domain.TicketVersion,
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
	versions := s.ticketVersions[requirement.ID]
	for index := range versions {
		if versions[index].ID == version.ID || versions[index].Version == version.Version {
			return 0, storecontract.ErrAlreadyExists
		}
		if versions[index].Status == domain.TicketVersionDraft {
			versions[index].Status = domain.TicketVersionSuperseded
		}
	}
	requirement.Revision = expectedRevision + 1
	s.requirements[requirement.ID] = requirement
	s.ticketVersions[requirement.ID] = append(versions, cloneTicketVersion(version))
	return requirement.Revision, nil
}

func (s *Store) ListTicketVersions(
	ctx context.Context,
	requirementID string,
) ([]domain.TicketVersion, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, exists := s.requirements[requirementID]; !exists {
		return nil, storecontract.ErrNotFound
	}
	versions := s.ticketVersions[requirementID]
	result := make([]domain.TicketVersion, 0, len(versions))
	for _, version := range versions {
		result = append(result, cloneTicketVersion(version))
	}
	return result, nil
}

func (s *Store) ConfirmTicketVersion(
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
	versions := s.ticketVersions[requirement.ID]
	found := false
	for index := range versions {
		if versions[index].ID != versionID || versions[index].Status != domain.TicketVersionDraft {
			continue
		}
		value := confirmedAt.UTC()
		versions[index].Status = domain.TicketVersionConfirmed
		versions[index].ConfirmedAt = &value
		found = true
	}
	if !found {
		return 0, storecontract.ErrNotFound
	}
	requirement.Revision = expectedRevision + 1
	s.requirements[requirement.ID] = requirement
	s.ticketVersions[requirement.ID] = versions
	return requirement.Revision, nil
}

func cloneTicketVersion(value domain.TicketVersion) domain.TicketVersion {
	value.Tickets = make([]domain.Ticket, 0, len(value.Tickets))
	for _, ticket := range value.Tickets {
		ticket.BlockedBy = append([]string{}, ticket.BlockedBy...)
		ticket.AcceptanceCriteria = append([]string{}, ticket.AcceptanceCriteria...)
		ticket.ModificationScopes = append([]string{}, ticket.ModificationScopes...)
		value.Tickets = append(value.Tickets, ticket)
	}
	if value.ConfirmedAt != nil {
		confirmedAt := *value.ConfirmedAt
		value.ConfirmedAt = &confirmedAt
	}
	return value
}
