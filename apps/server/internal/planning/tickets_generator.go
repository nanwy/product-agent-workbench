package planning

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"

	"product-agent-workbench/internal/app"
	"product-agent-workbench/internal/domain"
)

const (
	ticketsStart = "[WORKBENCH:TICKETS]"
	ticketsEnd   = "[/WORKBENCH:TICKETS]"
)

var fullGitRevision = regexp.MustCompile(`^[0-9a-fA-F]{40}([0-9a-fA-F]{24})?$`)

// TicketsGenerator 使用只读 Codex 形成 DAG，并由宿主 Git 固定执行基线。
// Codex 只能决定票的内容和依赖，仓库路径、修改范围与 commit 都来自受信任配置。
type TicketsGenerator struct {
	turns     TextTurnRunner
	target    Target
	gitBinary string
}

func NewTicketsGenerator(
	turns TextTurnRunner,
	target Target,
	gitBinary string,
) (*TicketsGenerator, error) {
	gitBinary = strings.TrimSpace(gitBinary)
	if turns == nil || target.RepositoryRoot == "" || target.ModificationScope == "" || gitBinary == "" {
		return nil, ErrInvalidGenerator
	}
	return &TicketsGenerator{turns: turns, target: target, gitBinary: gitBinary}, nil
}

func (g *TicketsGenerator) GenerateTickets(
	ctx context.Context,
	input app.TicketGenerationInput,
) (app.GeneratedTicketPlan, error) {
	text, err := g.turns.RunTextTurn(ctx, g.target.RepositoryRoot, buildTicketsPrompt(input))
	if err != nil {
		return app.GeneratedTicketPlan{}, err
	}
	tickets, err := parseTickets(text, g.target.ModificationScope)
	if err != nil {
		return app.GeneratedTicketPlan{}, err
	}
	baseRevision, err := g.headRevision(ctx)
	if err != nil {
		return app.GeneratedTicketPlan{}, err
	}
	return app.GeneratedTicketPlan{
		RepositoryRoot:    g.target.RepositoryRoot,
		BaseRevision:      baseRevision,
		RequirementBranch: "workbench/requirements/" + input.Requirement.ID,
		Tickets:           tickets,
	}, nil
}

func (g *TicketsGenerator) headRevision(ctx context.Context) (string, error) {
	command := exec.CommandContext(
		ctx,
		g.gitBinary,
		"-C",
		g.target.RepositoryRoot,
		"rev-parse",
		"--verify",
		"HEAD",
	)
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("读取目标项目 HEAD: %w", err)
	}
	revision := strings.TrimSpace(string(output))
	if !fullGitRevision.MatchString(revision) {
		return "", fmt.Errorf("%w: Git HEAD 不是完整 commit ID", ErrInvalidSpecOutput)
	}
	return revision, nil
}

func buildTicketsPrompt(input app.TicketGenerationInput) string {
	previous := "当前没有旧 TicketVersion。"
	if input.PreviousVersion != nil {
		encoded, _ := json.Marshal(input.PreviousVersion.Tickets)
		previous = "上一版 Tickets（只能用于修订参考）：" + string(encoded)
	}
	instruction := "把已确认 Spec 拆成最小但完整、可独立验证的纵向 Tickets。"
	if input.Instruction != "" {
		instruction = "本轮修订要求：" + input.Instruction
	}
	specJSON, _ := json.Marshal(input.Spec.Document)
	return strings.Join([]string{
		"你正在 Product Agent Workbench 的 to-tickets 阶段。",
		"只读检查目标仓库；不得修改文件、创建 Issue、PR 或执行实现。",
		"已确认 Spec：" + string(specJSON),
		previous,
		instruction,
		"blockedBy 只能引用本次列表中排在当前票之前的 ID；没有真实依赖时必须是空数组。",
		`最终必须输出且只输出一个控制块：[WORKBENCH:TICKETS]{"tickets":[{"id":"TICKET-01","title":"...","blockedBy":[],"deliverable":"...","acceptanceCriteria":["..."]}]}[/WORKBENCH:TICKETS]。ID 必须按 TICKET-01 顺序递增，至少一票，所有字段非空。`,
	}, "\n\n")
}

func parseTickets(text string, modificationScope string) ([]domain.Ticket, error) {
	start := strings.Index(text, ticketsStart)
	if start < 0 {
		return nil, fmt.Errorf("%w: 缺少 %s", ErrInvalidSpecOutput, ticketsStart)
	}
	start += len(ticketsStart)
	end := strings.Index(text[start:], ticketsEnd)
	if end < 0 {
		return nil, fmt.Errorf("%w: 缺少 %s", ErrInvalidSpecOutput, ticketsEnd)
	}
	var payload struct {
		Tickets []struct {
			ID                 string   `json:"id"`
			Title              string   `json:"title"`
			BlockedBy          []string `json:"blockedBy"`
			Deliverable        string   `json:"deliverable"`
			AcceptanceCriteria []string `json:"acceptanceCriteria"`
		} `json:"tickets"`
	}
	decoder := json.NewDecoder(strings.NewReader(text[start : start+end]))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("%w: 解析 Tickets 控制块: %v", ErrInvalidSpecOutput, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: Tickets 控制块只能包含一个 JSON 值", ErrInvalidSpecOutput)
	}
	tickets := make([]domain.Ticket, 0, len(payload.Tickets))
	for index, value := range payload.Tickets {
		expectedID := fmt.Sprintf("TICKET-%02d", index+1)
		if strings.TrimSpace(value.ID) != expectedID {
			return nil, fmt.Errorf("%w: 第 %d 张票 ID 必须是 %s", ErrInvalidSpecOutput, index+1, expectedID)
		}
		tickets = append(tickets, domain.Ticket{
			ID:                 expectedID,
			Title:              value.Title,
			BlockedBy:          value.BlockedBy,
			Deliverable:        value.Deliverable,
			AcceptanceCriteria: value.AcceptanceCriteria,
			ModificationScopes: []string{modificationScope},
		})
	}
	tickets, err := domain.NormalizeTickets(tickets)
	if err != nil {
		return nil, fmt.Errorf("%w: 依赖图或必填字段无效", ErrInvalidSpecOutput)
	}
	return tickets, nil
}

var _ app.TicketGenerator = (*TicketsGenerator)(nil)
