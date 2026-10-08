package app

import (
	"context"
	"errors"
	"fmt"
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

type PlanningSnapshot struct {
	Requirement   domain.Requirement
	Understanding *domain.GrillingUnderstanding
	SpecVersions  []domain.SpecVersion
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
		Requirement:   requirement,
		Understanding: &understanding,
		SpecVersions:  []domain.SpecVersion{},
	}, nil
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
