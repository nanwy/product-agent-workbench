package httpapi

import (
	"time"

	"product-agent-workbench/internal/app"
	"product-agent-workbench/internal/domain"
	"product-agent-workbench/internal/verification"
)

// 路径常量先固定浏览器与 Go 之间的语言，具体 handler 等领域用例稳定后再实现。
const (
	PathHealth       = "/api/v1/health"
	PathRequirements = "/api/v1/requirements"
	PathTaskRuns     = "/api/v1/task-runs"
	PathEvents       = "/api/v1/events"
)

// HealthResponse 是健康检查成功时返回的稳定 JSON 结构。
//
// 第一版只表达进程已经启动并能处理 HTTP。数据库连通性、Codex 可用性等属于 readiness
// 或环境预检，不要把昂贵检查塞进每一次轻量健康探测。
type HealthResponse struct {
	Status string `json:"status"`
}

// CreateRequirementRequest 是 POST /requirements 接受的 JSON。
//
// 不包含 ID、Stage 和 Revision：这些字段由 Application 创建，不能由浏览器伪造。
type CreateRequirementRequest struct {
	ProjectID string `json:"projectId"`
	Title     string `json:"title"`
}

// RequirementResponse 是 Requirement endpoint 返回的稳定 JSON 视图。
//
// 不直接把领域结构体当作 HTTP 合约，避免以后领域字段变化意外破坏前端协议。
type RequirementResponse struct {
	ID        string                  `json:"id"`
	ProjectID string                  `json:"projectId"`
	Title     string                  `json:"title"`
	Stage     domain.RequirementStage `json:"stage"`
	Revision  int64                   `json:"revision"`
}

type ConfirmGrillingRequest struct {
	ExpectedRevision int64  `json:"expectedRevision"`
	Summary          string `json:"summary"`
	DesiredOutcome   string `json:"desiredOutcome"`
	ChangeBoundary   string `json:"changeBoundary"`
	AcceptanceSignal string `json:"acceptanceSignal"`
}

type GenerateSpecRequest struct {
	ExpectedRevision int64  `json:"expectedRevision"`
	Instruction      string `json:"instruction"`
}

type ConfirmSpecRequest struct {
	ExpectedRevision int64 `json:"expectedRevision"`
}

type GenerateTicketsRequest struct {
	ExpectedRevision int64  `json:"expectedRevision"`
	Instruction      string `json:"instruction"`
}

type ConfirmTicketsRequest struct {
	ExpectedRevision int64 `json:"expectedRevision"`
}

type GrillingUnderstandingResponse struct {
	Summary          string    `json:"summary"`
	DesiredOutcome   string    `json:"desiredOutcome"`
	ChangeBoundary   string    `json:"changeBoundary"`
	AcceptanceSignal string    `json:"acceptanceSignal"`
	ConfirmedAt      time.Time `json:"confirmedAt"`
}

type SpecDocumentResponse struct {
	Problem                 string   `json:"problem"`
	Solution                string   `json:"solution"`
	UserStories             []string `json:"userStories"`
	ImplementationDecisions []string `json:"implementationDecisions"`
	TestingDecisions        []string `json:"testingDecisions"`
	OutOfScope              []string `json:"outOfScope"`
	FurtherNotes            []string `json:"furtherNotes"`
}

type SpecVersionResponse struct {
	ID          string                   `json:"id"`
	Version     int                      `json:"version"`
	Status      domain.SpecVersionStatus `json:"status"`
	Document    SpecDocumentResponse     `json:"document"`
	CreatedAt   time.Time                `json:"createdAt"`
	ConfirmedAt *time.Time               `json:"confirmedAt,omitempty"`
}

type TicketResponse struct {
	ID                 string   `json:"id"`
	Title              string   `json:"title"`
	BlockedBy          []string `json:"blockedBy"`
	Deliverable        string   `json:"deliverable"`
	AcceptanceCriteria []string `json:"acceptanceCriteria"`
	ModificationScopes []string `json:"modificationScopes"`
}

type TicketVersionResponse struct {
	ID                string                     `json:"id"`
	SpecVersionID     string                     `json:"specVersionId"`
	Version           int                        `json:"version"`
	Status            domain.TicketVersionStatus `json:"status"`
	BaseRevision      string                     `json:"baseRevision"`
	RequirementBranch string                     `json:"requirementBranch"`
	Tickets           []TicketResponse           `json:"tickets"`
	CreatedAt         time.Time                  `json:"createdAt"`
	ConfirmedAt       *time.Time                 `json:"confirmedAt,omitempty"`
}

type PlanningResponse struct {
	Requirement    RequirementResponse            `json:"requirement"`
	Understanding  *GrillingUnderstandingResponse `json:"understanding"`
	SpecVersions   []SpecVersionResponse          `json:"specVersions"`
	TicketVersions []TicketVersionResponse        `json:"ticketVersions"`
}

// ReconcileTaskRunsRequest 是浏览器请求调度一个已确认 TicketVersion 的稳定 JSON 合约。
//
// TicketID 和 Attempt 不接受浏览器输入；Scheduler 必须从确认 artifact 与执行历史计算。
type ReconcileTaskRunsRequest struct {
	RequirementID   string `json:"requirementId"`
	TicketVersionID string `json:"ticketVersionId"`
}

// ReconcileTaskRunsResponse 返回本次 Reconcile 真正新建的 queued TaskRun。
type ReconcileTaskRunsResponse struct {
	TicketVersionID string            `json:"ticketVersionId"`
	Enqueued        []TaskRunResponse `json:"enqueued"`
}

// TaskResultResponse 是成功 TaskRun 暴露给浏览器的结果视图。
type TaskResultResponse struct {
	Summary       string `json:"summary"`
	CodexThreadID string `json:"codexThreadId,omitempty"`
	WorktreePath  string `json:"worktreePath,omitempty"`
	Revision      string `json:"revision,omitempty"`
}

// TaskRunResponse 是 TaskRun endpoint 的稳定 JSON 视图。
type TaskRunResponse struct {
	ID              string              `json:"id"`
	RequirementID   string              `json:"requirementId"`
	TicketVersionID string              `json:"ticketVersionId"`
	TicketID        string              `json:"ticketId"`
	Attempt         int                 `json:"attempt"`
	Status          domain.TaskStatus   `json:"status"`
	CreatedAt       time.Time           `json:"createdAt"`
	ClaimedAt       *time.Time          `json:"claimedAt,omitempty"`
	StartedAt       *time.Time          `json:"startedAt,omitempty"`
	FinishedAt      *time.Time          `json:"finishedAt,omitempty"`
	Result          *TaskResultResponse `json:"result,omitempty"`
	FailureReason   string              `json:"failureReason,omitempty"`
}

// TaskEventResponse 是 SSE 中 task-event 事件的 JSON 数据。
//
// Payload 使用字符串而不是 json.RawMessage：Store 允许保存任意命令输出字节，脏数据也
// 只能影响这一字段，不能破坏整条 SSE JSON。
type TaskEventResponse struct {
	TaskRunID string    `json:"taskRunId"`
	Sequence  int64     `json:"sequence"`
	Type      string    `json:"type"`
	Payload   string    `json:"payload"`
	CreatedAt time.Time `json:"createdAt"`
}

// ReplayResetResponse 告诉前端：本地游标已经无法从当前事件表连续恢复。
//
// 收到它后，前端应重新 GET 页面快照，再用最新游标建立新连接。它类似前端缓存失效，
// 不是普通的 task-event。
type ReplayResetResponse struct {
	Reason        string `json:"reason"`
	OldestEventID int64  `json:"oldestEventId"`
	LatestEventID int64  `json:"latestEventId"`
}

type VerifyRequirementRequest struct {
	TicketVersionID string `json:"ticketVersionId"`
}

// VerificationEvidenceResponse 是前端验证矩阵的一行。
type VerificationEvidenceResponse struct {
	ID          string              `json:"id"`
	Label       string              `json:"label"`
	Status      verification.Status `json:"status"`
	Detail      string              `json:"detail"`
	Reason      string              `json:"reason,omitempty"`
	Required    bool                `json:"required"`
	Source      verification.Source `json:"source"`
	Command     string              `json:"command,omitempty"`
	ExitCode    *int                `json:"exitCode,omitempty"`
	StartedAt   time.Time           `json:"startedAt"`
	CompletedAt time.Time           `json:"completedAt"`
}

type VerificationReportResponse struct {
	ID                string                         `json:"id"`
	RequirementID     string                         `json:"requirementId"`
	TicketVersionID   string                         `json:"ticketVersionId"`
	CandidateRevision string                         `json:"candidateRevision,omitempty"`
	Status            verification.ReportStatus      `json:"status"`
	Records           []VerificationEvidenceResponse `json:"records"`
	StartedAt         time.Time                      `json:"startedAt"`
	CompletedAt       time.Time                      `json:"completedAt"`
}

type VerifyRequirementResponse struct {
	Requirement RequirementResponse        `json:"requirement"`
	Report      VerificationReportResponse `json:"report"`
	Deliverable bool                       `json:"deliverable"`
}

// ErrorResponse 是所有 HTTP 错误使用的稳定 JSON 结构。
//
// Code 给前端程序判断，Message 给本地用户阅读；不要把原始 SQL、文件路径或秘密直接放入
// Message。RequestID 留给后续日志关联，第一阶段可以为空。
type ErrorResponse struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"requestId,omitempty"`
}

// MutationMeta 是所有写请求共有的并发与幂等信息。
type MutationMeta struct {
	IdempotencyKey   string `json:"idempotencyKey"`
	ExpectedRevision int64  `json:"expectedRevision"`
}

// newRequirementResponse 把领域事实显式映射为传输对象。
//
// 实现要点：逐字段复制 ID、ProjectID、Title、Stage 和 Revision。不要返回领域对象本身，
// 也不要在映射函数中修改任何业务值。
func newRequirementResponse(requirement domain.Requirement) RequirementResponse {
	return RequirementResponse{
		ID:        requirement.ID,
		ProjectID: requirement.ProjectID,
		Title:     requirement.Title,
		Stage:     requirement.Stage,
		Revision:  requirement.Revision,
	}
}

func newPlanningResponse(snapshot app.PlanningSnapshot) PlanningResponse {
	response := PlanningResponse{
		Requirement:    newRequirementResponse(snapshot.Requirement),
		SpecVersions:   make([]SpecVersionResponse, 0, len(snapshot.SpecVersions)),
		TicketVersions: make([]TicketVersionResponse, 0, len(snapshot.TicketVersions)),
	}
	if snapshot.Understanding != nil {
		response.Understanding = &GrillingUnderstandingResponse{
			Summary:          snapshot.Understanding.Summary,
			DesiredOutcome:   snapshot.Understanding.DesiredOutcome,
			ChangeBoundary:   snapshot.Understanding.ChangeBoundary,
			AcceptanceSignal: snapshot.Understanding.AcceptanceSignal,
			ConfirmedAt:      snapshot.Understanding.ConfirmedAt,
		}
	}
	for _, version := range snapshot.SpecVersions {
		response.SpecVersions = append(response.SpecVersions, SpecVersionResponse{
			ID:      version.ID,
			Version: version.Version,
			Status:  version.Status,
			Document: SpecDocumentResponse{
				Problem:                 version.Document.Problem,
				Solution:                version.Document.Solution,
				UserStories:             append([]string(nil), version.Document.UserStories...),
				ImplementationDecisions: append([]string(nil), version.Document.ImplementationDecisions...),
				TestingDecisions:        append([]string(nil), version.Document.TestingDecisions...),
				OutOfScope:              append([]string(nil), version.Document.OutOfScope...),
				FurtherNotes:            append([]string(nil), version.Document.FurtherNotes...),
			},
			CreatedAt:   version.CreatedAt,
			ConfirmedAt: version.ConfirmedAt,
		})
	}
	for _, version := range snapshot.TicketVersions {
		tickets := make([]TicketResponse, 0, len(version.Tickets))
		for _, ticket := range version.Tickets {
			tickets = append(tickets, TicketResponse{
				ID:                 ticket.ID,
				Title:              ticket.Title,
				BlockedBy:          append([]string{}, ticket.BlockedBy...),
				Deliverable:        ticket.Deliverable,
				AcceptanceCriteria: append([]string{}, ticket.AcceptanceCriteria...),
				ModificationScopes: append([]string{}, ticket.ModificationScopes...),
			})
		}
		response.TicketVersions = append(response.TicketVersions, TicketVersionResponse{
			ID:                version.ID,
			SpecVersionID:     version.SpecVersionID,
			Version:           version.Version,
			Status:            version.Status,
			BaseRevision:      version.BaseRevision,
			RequirementBranch: version.RequirementBranch,
			Tickets:           tickets,
			CreatedAt:         version.CreatedAt,
			ConfirmedAt:       version.ConfirmedAt,
		})
	}
	return response
}

// newTaskRunResponse 显式把领域实体映射为 HTTP DTO。
//
// 实现时逐字段复制；Result 非 nil 时创建新的 TaskResultResponse，不能把领域结构体直接
// 当作 JSON 合约。时间指针只读使用，不在映射函数中改写。
func newTaskRunResponse(run domain.TaskRun) TaskRunResponse {
	response := TaskRunResponse{
		ID:              run.ID,
		RequirementID:   run.RequirementID,
		TicketVersionID: run.TicketVersionID,
		TicketID:        run.TicketID,
		Attempt:         run.Attempt,
		Status:          run.Status,
		CreatedAt:       run.CreatedAt,
		ClaimedAt:       run.ClaimedAt,
		StartedAt:       run.StartedAt,
		FinishedAt:      run.FinishedAt,
		FailureReason:   run.FailureReason,
	}
	if run.Result != nil {
		response.Result = &TaskResultResponse{
			Summary:       run.Result.Summary,
			CodexThreadID: run.Result.CodexThreadID,
			WorktreePath:  run.Result.WorktreePath,
			Revision:      run.Result.Revision,
		}
	}
	return response
}

func newTaskEventResponse(event domain.TaskEvent) TaskEventResponse {
	return TaskEventResponse{
		TaskRunID: event.TaskRunID,
		Sequence:  event.Sequence,
		Type:      event.Type,
		Payload:   string(event.Payload),
		CreatedAt: event.CreatedAt,
	}
}

func newVerificationReportResponse(report verification.Report) VerificationReportResponse {
	records := make([]VerificationEvidenceResponse, 0, len(report.Matrix.Records))
	for _, evidence := range report.Matrix.Records {
		var exitCode *int
		if evidence.ExitCode != nil {
			value := *evidence.ExitCode
			exitCode = &value
		}
		records = append(records, VerificationEvidenceResponse{
			ID:          evidence.ID,
			Label:       evidence.Label,
			Status:      evidence.Status,
			Detail:      evidence.Detail,
			Reason:      evidence.Reason,
			Required:    evidence.Required,
			Source:      evidence.Source,
			Command:     evidence.Command,
			ExitCode:    exitCode,
			StartedAt:   evidence.StartedAt,
			CompletedAt: evidence.CompletedAt,
		})
	}
	return VerificationReportResponse{
		ID:                report.ID,
		RequirementID:     report.RequirementID,
		TicketVersionID:   report.TicketVersionID,
		CandidateRevision: report.CandidateRevision,
		Status:            report.Status,
		Records:           records,
		StartedAt:         report.StartedAt,
		CompletedAt:       report.CompletedAt,
	}
}
