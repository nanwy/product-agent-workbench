// Package artifact 读取已经确认、不可变的 TicketVersion 执行制品。
//
// 生成逻辑属于规划 Adapter；本包只负责原子发布已确认版本，以及为 Scheduler/Runner 提供
// 只读投影。浏览器不能把仓库路径、Prompt 或修改范围直接塞进执行请求。
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

// PublishConfirmedTicketVersion 把数据库中已冻结的 TicketVersion 发布成不可变 JSON manifest。
// 同一内容可重复发布，用于修复“数据库已提交、进程在文件落盘前退出”的安全失败状态。
func (s *Store) PublishConfirmedTicketVersion(
	ctx context.Context,
	version domain.TicketVersion,
) error {
	if ctx == nil {
		return fmt.Errorf("%w: context 不能为空", ErrInvalidDocument)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if version.Status != domain.TicketVersionConfirmed || version.ConfirmedAt == nil {
		return fmt.Errorf("%w: 只能发布已确认 TicketVersion", ErrInvalidDocument)
	}
	document := TicketVersionDocument{
		ID:                version.ID,
		RequirementID:     version.RequirementID,
		RepositoryRoot:    version.RepositoryRoot,
		BaseRevision:      version.BaseRevision,
		RequirementBranch: version.RequirementBranch,
		Tickets:           make([]TicketDocument, 0, len(version.Tickets)),
	}
	for _, ticket := range version.Tickets {
		document.Tickets = append(document.Tickets, TicketDocument{
			ID:                 ticket.ID,
			BlockedBy:          append([]string{}, ticket.BlockedBy...),
			Prompt:             renderTicketPrompt(ticket),
			ModificationScopes: append([]string{}, ticket.ModificationScopes...),
		})
	}
	if err := validateDocument(document, version.RequirementID, version.ID); err != nil {
		return err
	}
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return fmt.Errorf("编码 TicketVersion artifact: %w", err)
	}
	data = append(data, '\n')
	directory := filepath.Join(s.root, version.RequirementID)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("创建 TicketVersion artifact 目录: %w", err)
	}
	path := filepath.Join(directory, version.ID+".json")
	if same, err := artifactMatches(path, data); err != nil {
		return err
	} else if same {
		return nil
	}

	temporary, err := os.CreateTemp(directory, ".ticket-version-*.tmp")
	if err != nil {
		return fmt.Errorf("创建 TicketVersion 临时文件: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("设置 TicketVersion 临时文件权限: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return fmt.Errorf("写入 TicketVersion 临时文件: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("同步 TicketVersion 临时文件: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("关闭 TicketVersion 临时文件: %w", err)
	}
	// Link 不覆盖既有文件；并发发布只能有一个获胜，其余调用再比较内容后幂等成功。
	if err := os.Link(temporaryPath, path); err != nil {
		if same, matchErr := artifactMatches(path, data); matchErr != nil {
			return matchErr
		} else if same {
			return nil
		}
		return fmt.Errorf("发布 TicketVersion artifact: %w", err)
	}
	return nil
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
		if errors.Is(err, os.ErrNotExist) {
			// 前端类比：这是按 ID 查询资源得到空结果，应当一路保留“404”语义；不能把
			// 底层 ENOENT 直接冒泡，否则 HTTP 层只能把它当作未知故障返回 500。
			return TicketVersionDocument{}, fmt.Errorf(
				"%w: requirement=%q ticketVersion=%q",
				scheduler.ErrTicketVersionNotFound,
				requirementID,
				ticketVersionID,
			)
		}
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
		for _, blocker := range ticket.BlockedBy {
			if _, exists := seen[blocker]; !exists {
				return fmt.Errorf("%w: Ticket %q 依赖不存在或顺序错误", ErrInvalidDocument, ticket.ID)
			}
		}
		seen[ticket.ID] = struct{}{}
	}
	return nil
}

func artifactMatches(path string, expected []byte) (bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("读取既有 TicketVersion artifact: %w", err)
	}
	if !bytes.Equal(data, expected) {
		return false, fmt.Errorf("%w: 已存在同 ID 的不同 artifact", ErrInvalidDocument)
	}
	return true, nil
}

func renderTicketPrompt(ticket domain.Ticket) string {
	criteria := make([]string, 0, len(ticket.AcceptanceCriteria))
	for _, criterion := range ticket.AcceptanceCriteria {
		criteria = append(criteria, "- "+criterion)
	}
	return strings.Join([]string{
		"实现执行票 " + ticket.ID + "：" + ticket.Title,
		"交付内容：" + ticket.Deliverable,
		"验收条件：\n" + strings.Join(criteria, "\n"),
		"只修改 manifest 中允许的 modificationScopes；完成后说明修改和验证结果。",
	}, "\n\n")
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
var _ interface {
	PublishConfirmedTicketVersion(context.Context, domain.TicketVersion) error
} = (*Store)(nil)
