// Package artifact 读取已经确认、不可变的 TicketVersion 执行制品。
//
// 这里故意只实现读取：Spec/Ticket 的生成与确认仍由工作流用例负责。Runner 不能修改制品，
// 浏览器也不能把仓库路径、Prompt 或修改范围直接塞进执行请求。
package artifact

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"product-agent-workbench/internal/domain"
	"product-agent-workbench/internal/runner"
	"product-agent-workbench/internal/scheduler"
)

const maxDocumentBytes = 2 << 20

var (
	// ErrInvalidConfig 表示 artifact 根目录不是可安全读取的绝对目录。
	ErrInvalidConfig = errors.New("Artifact Store 配置无效")
	// ErrInvalidDocument 表示已确认文件缺字段、身份错配或包含未知字段。
	ErrInvalidDocument = errors.New("TicketVersion artifact 无效")
	artifactIDPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
)

// TicketVersionDocument 是磁盘 JSON 的稳定格式。
//
// 前端类比：它类似构建产物中的 manifest。Scheduler 只投影 ID 与 blockedBy；Runner 再按
// TicketID 读取执行所需字段，两个消费者共享同一份不可变事实。
type TicketVersionDocument struct {
	ID                string           `json:"id"`
	RequirementID     string           `json:"requirementId"`
	RepositoryRoot    string           `json:"repositoryRoot"`
	BaseRevision      string           `json:"baseRevision"`
	RequirementBranch string           `json:"requirementBranch"`
	Tickets           []TicketDocument `json:"tickets"`
}

// TicketDocument 保存单张执行票的依赖关系和受控执行输入。
type TicketDocument struct {
	ID                 string   `json:"id"`
	BlockedBy          []string `json:"blockedBy"`
	Prompt             string   `json:"prompt"`
	ModificationScopes []string `json:"modificationScopes"`
}

// Store 同时满足 Scheduler 与 Runner 的只读接口。
type Store struct {
	root string
}

// NewStore 创建 artifact reader。Root 会被创建并解析成绝对真实路径。
func NewStore(root string) (*Store, error) {
	root = strings.TrimSpace(root)
	if root == "" || !filepath.IsAbs(root) {
		return nil, ErrInvalidConfig
	}
	root = filepath.Clean(root)
	if root == string(filepath.Separator) {
		return nil, ErrInvalidConfig
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("创建 artifact 根目录: %w", err)
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("解析 artifact 根目录: %w", err)
	}
	return &Store{root: canonicalRoot}, nil
}

// ReadConfirmedTicketVersion 返回 Scheduler 需要的依赖图投影。
func (s *Store) ReadConfirmedTicketVersion(
	ctx context.Context,
	requirementID string,
	ticketVersionID string,
) (scheduler.TicketVersion, error) {
	document, err := s.readDocument(ctx, requirementID, ticketVersionID)
	if err != nil {
		return scheduler.TicketVersion{}, err
	}

	version := scheduler.TicketVersion{
		ID:            document.ID,
		RequirementID: document.RequirementID,
		Tickets:       make([]scheduler.Ticket, 0, len(document.Tickets)),
	}
	for _, ticket := range document.Tickets {
		version.Tickets = append(version.Tickets, scheduler.Ticket{
			ID:        ticket.ID,
			BlockedBy: append([]string{}, ticket.BlockedBy...),
		})
	}
	return version, nil
}

// ReadExecutionFacts 返回 Worker 需要的可信执行输入。
func (s *Store) ReadExecutionFacts(
	ctx context.Context,
	run domain.TaskRun,
) (runner.ExecutionFacts, error) {
	document, err := s.readDocument(ctx, run.RequirementID, run.TicketVersionID)
	if err != nil {
		return runner.ExecutionFacts{}, err
	}

	for _, ticket := range document.Tickets {
		if ticket.ID != run.TicketID {
			continue
		}
		return runner.ExecutionFacts{
			RequirementID:   document.RequirementID,
			TicketVersionID: document.ID,
			TicketID:        ticket.ID,
			RepositoryRoot:  document.RepositoryRoot,
			Prompt:          ticket.Prompt,
			// 当前 attempt 如果已经打开过 Thread（例如同进程内重建请求），直接恢复它。
			// 更早 attempt 的 Thread 由 composition root 组合的历史 reader 补充。
			CodexThreadID: run.CodexThreadID,
			Workspace: runner.WorkspacePlan{
				BaseRevision:       document.BaseRevision,
				TicketBranch:       ticketBranch(run),
				RequirementBranch:  document.RequirementBranch,
				ModificationScopes: append([]string{}, ticket.ModificationScopes...),
			},
		}, nil
	}
	return runner.ExecutionFacts{}, fmt.Errorf("%w: 找不到 Ticket %q", ErrInvalidDocument, run.TicketID)
}

// readDocument 的路径只由经过白名单校验的两个 ID 组成，不能通过 ../ 逃离 artifact 根目录。
func (s *Store) readDocument(
	ctx context.Context,
	requirementID string,
	ticketVersionID string,
) (TicketVersionDocument, error) {
	if ctx == nil {
		return TicketVersionDocument{}, fmt.Errorf("%w: context 不能为空", ErrInvalidDocument)
	}
	if err := ctx.Err(); err != nil {
		return TicketVersionDocument{}, err
	}
	if !validArtifactID(requirementID) || !validArtifactID(ticketVersionID) {
		return TicketVersionDocument{}, ErrInvalidDocument
	}

	path := filepath.Join(s.root, requirementID, ticketVersionID+".json")
	file, err := os.Open(path)
	if err != nil {
		return TicketVersionDocument{}, fmt.Errorf("打开 TicketVersion artifact: %w", err)
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxDocumentBytes+1))
	if err != nil {
		return TicketVersionDocument{}, fmt.Errorf("读取 TicketVersion artifact: %w", err)
	}
	if len(data) > maxDocumentBytes {
		return TicketVersionDocument{}, fmt.Errorf("%w: 文件超过 %d 字节", ErrInvalidDocument, maxDocumentBytes)
	}

	var document TicketVersionDocument
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return TicketVersionDocument{}, fmt.Errorf("%w: %v", ErrInvalidDocument, err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return TicketVersionDocument{}, err
	}
	if err := validateDocument(document, requirementID, ticketVersionID); err != nil {
		return TicketVersionDocument{}, err
	}
	if err := ctx.Err(); err != nil {
		return TicketVersionDocument{}, err
	}
	return document, nil
}

func validateDocument(document TicketVersionDocument, requirementID string, ticketVersionID string) error {
	validIdentity := document.RequirementID == requirementID && document.ID == ticketVersionID
	validExecutionRoot := filepath.IsAbs(document.RepositoryRoot) &&
		strings.TrimSpace(document.BaseRevision) != "" &&
		strings.TrimSpace(document.RequirementBranch) != ""
	if !validIdentity || !validExecutionRoot || len(document.Tickets) == 0 {
		return ErrInvalidDocument
	}

	seen := make(map[string]struct{}, len(document.Tickets))
	for _, ticket := range document.Tickets {
		if !validArtifactID(ticket.ID) || strings.TrimSpace(ticket.Prompt) == "" || len(ticket.ModificationScopes) == 0 {
			return fmt.Errorf("%w: Ticket %q 缺少执行字段", ErrInvalidDocument, ticket.ID)
		}
		if _, exists := seen[ticket.ID]; exists {
			return fmt.Errorf("%w: Ticket %q 重复", ErrInvalidDocument, ticket.ID)
		}
		seen[ticket.ID] = struct{}{}
	}
	return nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return fmt.Errorf("%w: 文件只能包含一个 JSON 值", ErrInvalidDocument)
	}
	return fmt.Errorf("%w: %v", ErrInvalidDocument, err)
}

func validArtifactID(value string) bool {
	return artifactIDPattern.MatchString(value)
}

func ticketBranch(run domain.TaskRun) string {
	return fmt.Sprintf(
		"workbench/%s/%s/attempt-%d",
		run.RequirementID,
		run.TicketID,
		run.Attempt,
	)
}

var _ scheduler.ArtifactReader = (*Store)(nil)
var _ runner.ExecutionFactsReader = (*Store)(nil)
