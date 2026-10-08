package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"product-agent-workbench/internal/domain"
	"product-agent-workbench/internal/store"
)

type ConfirmGrillingInput struct {
	RequirementID    string
	ExpectedRevision int64
	Summary          string
	DesiredOutcome   string
	ChangeBoundary   string
	AcceptanceSignal string
}

type GenerateSpecInput struct {
	RequirementID    string
	ExpectedRevision int64
	Instruction      string
}

type ConfirmSpecInput struct {
	RequirementID    string
	VersionID        string
	ExpectedRevision int64
}

type GenerateTicketsInput struct {
	RequirementID    string
	ExpectedRevision int64
	Instruction      string
}

type ConfirmTicketsInput struct {
	RequirementID    string
	VersionID        string
	ExpectedRevision int64
}

type PlanningSnapshot struct {
	Requirement    domain.Requirement
	Understanding  *domain.GrillingUnderstanding
	SpecVersions   []domain.SpecVersion
	TicketVersions []domain.TicketVersion
}

// ConfirmGrilling 冻结共同理解，并只允许 grilling → spec。
func (a *Application) ConfirmGrilling(
	ctx context.Context,
	input ConfirmGrillingInput,
) (PlanningSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return PlanningSnapshot{}, err
	}
	input.RequirementID = strings.TrimSpace(input.RequirementID)
	if input.RequirementID == "" || input.ExpectedRevision < 1 {
		return PlanningSnapshot{}, domain.ErrInvalidInput
	}
	requirement, err := a.store.GetRequirement(ctx, input.RequirementID)
	if err != nil {
		return PlanningSnapshot{}, err
	}
	if requirement.Revision != input.ExpectedRevision {
		return PlanningSnapshot{}, store.ErrRevisionConflict
	}
	if requirement.Stage != domain.RequirementStageGrilling {
		return PlanningSnapshot{}, domain.ErrInvalidTransition
	}
	understanding, err := domain.NormalizeGrillingUnderstanding(domain.GrillingUnderstanding{
		RequirementID:    requirement.ID,
		Summary:          input.Summary,
		DesiredOutcome:   input.DesiredOutcome,
		ChangeBoundary:   input.ChangeBoundary,
		AcceptanceSignal: input.AcceptanceSignal,
		ConfirmedAt:      a.now(),
	})
	if err != nil {
		return PlanningSnapshot{}, err
	}
	requirement.Stage = domain.RequirementStageSpec
	newRevision, err := a.store.ConfirmGrilling(ctx, requirement, input.ExpectedRevision, understanding)
	if err != nil {
		return PlanningSnapshot{}, err
	}
	requirement.Revision = newRevision
	return PlanningSnapshot{
		Requirement:    requirement,
		Understanding:  &understanding,
		SpecVersions:   []domain.SpecVersion{},
		TicketVersions: []domain.TicketVersion{},
	}, nil
}

// GenerateTickets 把已确认 Spec 拆成 Ticket DAG，但此时还不会开放 Scheduler。
func (a *Application) GenerateTickets(
	ctx context.Context,
	input GenerateTicketsInput,
) (PlanningSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return PlanningSnapshot{}, err
	}
	input.RequirementID = strings.TrimSpace(input.RequirementID)
	input.Instruction = strings.TrimSpace(input.Instruction)
	invalidInput := input.RequirementID == "" || input.ExpectedRevision < 1 || len(input.Instruction) > 4000
	if invalidInput {
		return PlanningSnapshot{}, domain.ErrInvalidInput
	}
	if a.ticketGenerator == nil {
		return PlanningSnapshot{}, ErrTicketGeneratorUnavailable
	}
	requirement, err := a.store.GetRequirement(ctx, input.RequirementID)
	if err != nil {
		return PlanningSnapshot{}, err
	}
	if requirement.Revision != input.ExpectedRevision {
		return PlanningSnapshot{}, store.ErrRevisionConflict
	}
	if requirement.Stage != domain.RequirementStageTickets {
		return PlanningSnapshot{}, domain.ErrInvalidTransition
	}
	specVersions, err := a.store.ListSpecVersions(ctx, requirement.ID)
	if err != nil {
		return PlanningSnapshot{}, err
	}
	confirmedSpec, found := confirmedSpecVersion(specVersions)
	if !found {
		return PlanningSnapshot{}, domain.ErrInvalidTransition
	}
	ticketVersions, err := a.store.ListTicketVersions(ctx, requirement.ID)
	if err != nil {
		return PlanningSnapshot{}, err
	}
	var previousVersion *domain.TicketVersion
	if len(ticketVersions) > 0 {
		value := ticketVersions[len(ticketVersions)-1]
		previousVersion = &value
	}
	plan, err := a.ticketGenerator.GenerateTickets(ctx, TicketGenerationInput{
		Requirement:     requirement,
		Spec:            confirmedSpec,
		PreviousVersion: previousVersion,
		Instruction:     input.Instruction,
	})
	if err != nil {
		return PlanningSnapshot{}, fmt.Errorf("生成 Tickets: %w", err)
	}
	plan.RepositoryRoot = filepath.Clean(strings.TrimSpace(plan.RepositoryRoot))
	plan.BaseRevision = strings.TrimSpace(plan.BaseRevision)
	plan.RequirementBranch = strings.TrimSpace(plan.RequirementBranch)
	validPlan := filepath.IsAbs(plan.RepositoryRoot) && plan.BaseRevision != "" && plan.RequirementBranch != ""
	if !validPlan {
		return PlanningSnapshot{}, ErrInvalidTicketOutput
	}
	tickets, err := domain.NormalizeTickets(plan.Tickets)
	if err != nil {
		return PlanningSnapshot{}, fmt.Errorf("%w: Ticket DAG 不完整", ErrInvalidTicketOutput)
	}
	version := domain.TicketVersion{
		ID:                "tickets-" + strings.TrimSpace(a.newID()),
		RequirementID:     requirement.ID,
		SpecVersionID:     confirmedSpec.ID,
		Version:           nextTicketVersion(ticketVersions),
		Status:            domain.TicketVersionDraft,
		RepositoryRoot:    plan.RepositoryRoot,
		BaseRevision:      plan.BaseRevision,
		RequirementBranch: plan.RequirementBranch,
		Tickets:           tickets,
		CreatedAt:         a.now().UTC(),
	}
	if version.ID == "tickets-" {
		return PlanningSnapshot{}, domain.ErrInvalidInput
	}
	newRevision, err := a.store.CreateTicketVersion(ctx, requirement, input.ExpectedRevision, version)
	if err != nil {
		return PlanningSnapshot{}, err
	}
	requirement.Revision = newRevision
	ticketVersions = supersedeTicketDrafts(ticketVersions)
	ticketVersions = append(ticketVersions, version)
	understanding, err := a.store.GetGrillingUnderstanding(ctx, requirement.ID)
	if err != nil {
		return PlanningSnapshot{}, err
	}
	return PlanningSnapshot{
		Requirement:    requirement,
		Understanding:  &understanding,
		SpecVersions:   specVersions,
		TicketVersions: ticketVersions,
	}, nil
}

// ConfirmTickets 先用 SQLite 冻结版本和阶段，再发布不可变执行 manifest。
// 如果文件发布失败，重复调用同一路径会从已确认数据库事实修复文件，不会再次增加 revision。
func (a *Application) ConfirmTickets(
	ctx context.Context,
	input ConfirmTicketsInput,
) (PlanningSnapshot, error) {
	input.RequirementID = strings.TrimSpace(input.RequirementID)
	input.VersionID = strings.TrimSpace(input.VersionID)
	if input.RequirementID == "" || input.VersionID == "" || input.ExpectedRevision < 1 {
		return PlanningSnapshot{}, domain.ErrInvalidInput
	}
	if a.ticketPublisher == nil {
		return PlanningSnapshot{}, ErrInvalidDependencies
	}
	requirement, err := a.store.GetRequirement(ctx, input.RequirementID)
	if err != nil {
		return PlanningSnapshot{}, err
	}
	versions, err := a.store.ListTicketVersions(ctx, requirement.ID)
	if err != nil {
		return PlanningSnapshot{}, err
	}
	if confirmed, found := findConfirmedTicketVersion(versions, input.VersionID); found &&
		requirement.Stage == domain.RequirementStageImplementation {
		if input.ExpectedRevision != requirement.Revision && input.ExpectedRevision+1 != requirement.Revision {
			return PlanningSnapshot{}, store.ErrRevisionConflict
		}
		if err := a.ticketPublisher.PublishConfirmedTicketVersion(context.WithoutCancel(ctx), confirmed); err != nil {
			return PlanningSnapshot{}, fmt.Errorf("修复 TicketVersion artifact: %w", err)
		}
		return a.GetPlanning(ctx, requirement.ID)
	}
	if requirement.Revision != input.ExpectedRevision {
		return PlanningSnapshot{}, store.ErrRevisionConflict
	}
	if requirement.Stage != domain.RequirementStageTickets {
		return PlanningSnapshot{}, domain.ErrInvalidTransition
	}
	active := -1
	for index := range versions {
		if versions[index].ID == input.VersionID && versions[index].Status == domain.TicketVersionDraft {
			active = index
		}
	}
	if active < 0 {
		return PlanningSnapshot{}, store.ErrNotFound
	}
	confirmedAt := a.now().UTC()
	requirement.Stage = domain.RequirementStageImplementation
	newRevision, err := a.store.ConfirmTicketVersion(
		ctx,
		requirement,
		input.ExpectedRevision,
		input.VersionID,
		confirmedAt,
	)
	if err != nil {
		return PlanningSnapshot{}, err
	}
	requirement.Revision = newRevision
	versions[active].Status = domain.TicketVersionConfirmed
	versions[active].ConfirmedAt = &confirmedAt
	// SQLite 已经提交后，即使浏览器中断请求，也要继续完成本地 manifest 发布。
	// 发布失败仍会返回错误；相同确认请求可以从数据库中的 confirmed 事实幂等修复。
	publishContext := context.WithoutCancel(ctx)
	if err := a.ticketPublisher.PublishConfirmedTicketVersion(publishContext, versions[active]); err != nil {
		return PlanningSnapshot{}, fmt.Errorf("发布 TicketVersion artifact: %w", err)
	}
	return a.GetPlanning(ctx, requirement.ID)
}

// GenerateSpec 执行一次只读 Codex Turn，并把完整结构化结果保存成新版本。
func (a *Application) GenerateSpec(
	ctx context.Context,
	input GenerateSpecInput,
) (PlanningSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return PlanningSnapshot{}, err
	}
	input.RequirementID = strings.TrimSpace(input.RequirementID)
	input.Instruction = strings.TrimSpace(input.Instruction)
	if input.RequirementID == "" || input.ExpectedRevision < 1 || len(input.Instruction) > 4000 {
		return PlanningSnapshot{}, domain.ErrInvalidInput
	}
	if a.specGenerator == nil {
		return PlanningSnapshot{}, ErrSpecGeneratorUnavailable
	}
	requirement, err := a.store.GetRequirement(ctx, input.RequirementID)
	if err != nil {
		return PlanningSnapshot{}, err
	}
	if requirement.Revision != input.ExpectedRevision {
		return PlanningSnapshot{}, store.ErrRevisionConflict
	}
	if requirement.Stage != domain.RequirementStageSpec {
		return PlanningSnapshot{}, domain.ErrInvalidTransition
	}
	understanding, err := a.store.GetGrillingUnderstanding(ctx, requirement.ID)
	if err != nil {
		return PlanningSnapshot{}, fmt.Errorf("读取已确认共同理解: %w", err)
	}
	versions, err := a.store.ListSpecVersions(ctx, requirement.ID)
	if err != nil {
		return PlanningSnapshot{}, err
	}
	var previousSpec *domain.SpecDocument
	if len(versions) > 0 {
		value := versions[len(versions)-1].Document
		previousSpec = &value
	}
	document, err := a.specGenerator.Generate(ctx, SpecGenerationInput{
		Requirement:   requirement,
		Understanding: understanding,
		PreviousSpec:  previousSpec,
		Instruction:   input.Instruction,
	})
	if err != nil {
		return PlanningSnapshot{}, fmt.Errorf("生成 Spec: %w", err)
	}
	document, err = domain.NormalizeSpecDocument(document)
	if err != nil {
		return PlanningSnapshot{}, fmt.Errorf("%w: Spec 生成结果不完整", ErrInvalidSpecOutput)
	}
	versionNumber := nextSpecVersion(versions)
	versionID := "spec-" + strings.TrimSpace(a.newID())
	if versionID == "spec-" {
		return PlanningSnapshot{}, domain.ErrInvalidInput
	}
	version := domain.SpecVersion{
		ID:            versionID,
		RequirementID: requirement.ID,
		Version:       versionNumber,
		Status:        domain.SpecVersionDraft,
		Document:      document,
		CreatedAt:     a.now().UTC(),
	}
	newRevision, err := a.store.CreateSpecVersion(ctx, requirement, input.ExpectedRevision, version)
	if err != nil {
		return PlanningSnapshot{}, err
	}
	requirement.Revision = newRevision
	versions = supersedeDrafts(versions)
	versions = append(versions, version)
	return PlanningSnapshot{Requirement: requirement, Understanding: &understanding, SpecVersions: versions}, nil
}

// ConfirmSpec 只冻结当前活动草稿，并允许 spec → tickets。
func (a *Application) ConfirmSpec(ctx context.Context, input ConfirmSpecInput) (PlanningSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return PlanningSnapshot{}, err
	}
	input.RequirementID = strings.TrimSpace(input.RequirementID)
	input.VersionID = strings.TrimSpace(input.VersionID)
	if input.RequirementID == "" || input.VersionID == "" || input.ExpectedRevision < 1 {
		return PlanningSnapshot{}, domain.ErrInvalidInput
	}
	requirement, err := a.store.GetRequirement(ctx, input.RequirementID)
	if err != nil {
		return PlanningSnapshot{}, err
	}
	if requirement.Revision != input.ExpectedRevision {
		return PlanningSnapshot{}, store.ErrRevisionConflict
	}
	if requirement.Stage != domain.RequirementStageSpec {
		return PlanningSnapshot{}, domain.ErrInvalidTransition
	}
	versions, err := a.store.ListSpecVersions(ctx, requirement.ID)
	if err != nil {
		return PlanningSnapshot{}, err
	}
	active := -1
	for index := range versions {
		if versions[index].ID == input.VersionID && versions[index].Status == domain.SpecVersionDraft {
			active = index
		}
	}
	if active < 0 {
		return PlanningSnapshot{}, store.ErrNotFound
	}
	confirmedAt := a.now().UTC()
	requirement.Stage = domain.RequirementStageTickets
	newRevision, err := a.store.ConfirmSpecVersion(
		ctx,
		requirement,
		input.ExpectedRevision,
		input.VersionID,
		confirmedAt,
	)
	if err != nil {
		return PlanningSnapshot{}, err
	}
	requirement.Revision = newRevision
	versions[active].Status = domain.SpecVersionConfirmed
	versions[active].ConfirmedAt = &confirmedAt
	understanding, err := a.store.GetGrillingUnderstanding(ctx, requirement.ID)
	if err != nil {
		return PlanningSnapshot{}, err
	}
	return PlanningSnapshot{Requirement: requirement, Understanding: &understanding, SpecVersions: versions}, nil
}

// GetPlanning 返回页面刷新后重建 21A/21B 所需的权威快照。
func (a *Application) GetPlanning(ctx context.Context, requirementID string) (PlanningSnapshot, error) {
	requirement, err := a.GetRequirement(ctx, requirementID)
	if err != nil {
		return PlanningSnapshot{}, err
	}
	snapshot := PlanningSnapshot{Requirement: requirement, SpecVersions: []domain.SpecVersion{}}
	snapshot.TicketVersions = []domain.TicketVersion{}
	understanding, err := a.store.GetGrillingUnderstanding(ctx, requirement.ID)
	if err == nil {
		snapshot.Understanding = &understanding
	} else if !errors.Is(err, store.ErrNotFound) {
		return PlanningSnapshot{}, err
	}
	versions, err := a.store.ListSpecVersions(ctx, requirement.ID)
	if err != nil {
		return PlanningSnapshot{}, err
	}
	snapshot.SpecVersions = versions
	ticketVersions, err := a.store.ListTicketVersions(ctx, requirement.ID)
	if err != nil {
		return PlanningSnapshot{}, err
	}
	snapshot.TicketVersions = ticketVersions
	return snapshot, nil
}

func nextSpecVersion(versions []domain.SpecVersion) int {
	next := 1
	for _, version := range versions {
		if version.Version >= next {
			next = version.Version + 1
		}
	}
	return next
}

func supersedeDrafts(versions []domain.SpecVersion) []domain.SpecVersion {
	result := make([]domain.SpecVersion, len(versions))
	copy(result, versions)
	for index := range result {
		if result[index].Status == domain.SpecVersionDraft {
			result[index].Status = domain.SpecVersionSuperseded
		}
	}
	return result
}

func confirmedSpecVersion(versions []domain.SpecVersion) (domain.SpecVersion, bool) {
	for _, version := range versions {
		if version.Status == domain.SpecVersionConfirmed {
			return version, true
		}
	}
	return domain.SpecVersion{}, false
}

func nextTicketVersion(versions []domain.TicketVersion) int {
	next := 1
	for _, version := range versions {
		if version.Version >= next {
			next = version.Version + 1
		}
	}
	return next
}

func supersedeTicketDrafts(versions []domain.TicketVersion) []domain.TicketVersion {
	result := make([]domain.TicketVersion, len(versions))
	copy(result, versions)
	for index := range result {
		if result[index].Status == domain.TicketVersionDraft {
			result[index].Status = domain.TicketVersionSuperseded
		}
	}
	return result
}

func findConfirmedTicketVersion(
	versions []domain.TicketVersion,
	versionID string,
) (domain.TicketVersion, bool) {
	for _, version := range versions {
		if version.ID == versionID && version.Status == domain.TicketVersionConfirmed {
			return version, true
		}
	}
	return domain.TicketVersion{}, false
}
