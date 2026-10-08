// Package planning 把只读 Codex Turn 适配为应用层的结构化 SpecGenerator。
package planning

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"product-agent-workbench/internal/app"
	"product-agent-workbench/internal/domain"
)

const (
	specStart = "[WORKBENCH:SPEC]"
	specEnd   = "[/WORKBENCH:SPEC]"
)

var (
	ErrInvalidGenerator  = errors.New("Spec Generator 配置无效")
	ErrInvalidSpecOutput = errors.New("Codex Spec 输出无效")
)

type TextTurnRunner interface {
	RunTextTurn(ctx context.Context, workingDirectory string, prompt string) (string, error)
}

type Generator struct {
	turns          TextTurnRunner
	repositoryRoot string
}

func NewGenerator(turns TextTurnRunner, repositoryRoot string) (*Generator, error) {
	repositoryRoot = strings.TrimSpace(repositoryRoot)
	if turns == nil || repositoryRoot == "" {
		return nil, ErrInvalidGenerator
	}
	return &Generator{turns: turns, repositoryRoot: repositoryRoot}, nil
}

func (g *Generator) Generate(ctx context.Context, input app.SpecGenerationInput) (domain.SpecDocument, error) {
	text, err := g.turns.RunTextTurn(ctx, g.repositoryRoot, buildPrompt(input))
	if err != nil {
		return domain.SpecDocument{}, err
	}
	return parseSpec(text)
}

func buildPrompt(input app.SpecGenerationInput) string {
	revision := "这是第一版 Spec。"
	if input.Instruction != "" {
		revision = "本轮修订要求：" + input.Instruction
	}
	previous := "当前没有旧 Spec。"
	if input.PreviousSpec != nil {
		encoded, _ := json.Marshal(input.PreviousSpec)
		previous = "上一版 Spec（只能用于修订参考）：" + string(encoded)
	}
	return strings.Join([]string{
		"你正在 Product Agent Workbench 的 to-spec 阶段。",
		"只读检查目标仓库；不得修改文件、生成 Tickets 或执行实现。",
		"需求标题：" + input.Requirement.Title,
		fmt.Sprintf("已确认共同理解：summary=%q; desiredOutcome=%q; changeBoundary=%q; acceptanceSignal=%q",
			input.Understanding.Summary,
			input.Understanding.DesiredOutcome,
			input.Understanding.ChangeBoundary,
			input.Understanding.AcceptanceSignal,
		),
		revision,
		previous,
		`最终必须输出且只输出一个控制块：[WORKBENCH:SPEC]{"problem":"...","solution":"...","userStories":["..."],"implementationDecisions":["..."],"testingDecisions":["..."],"outOfScope":["..."],"furtherNotes":["..."]}[/WORKBENCH:SPEC]。每个字段都不能为空。`,
	}, "\n\n")
}

func parseSpec(text string) (domain.SpecDocument, error) {
	start := strings.Index(text, specStart)
	if start < 0 {
		return domain.SpecDocument{}, fmt.Errorf("%w: 缺少 %s", ErrInvalidSpecOutput, specStart)
	}
	start += len(specStart)
	end := strings.Index(text[start:], specEnd)
	if end < 0 {
		return domain.SpecDocument{}, fmt.Errorf("%w: 缺少 %s", ErrInvalidSpecOutput, specEnd)
	}
	var document struct {
		Problem                 string   `json:"problem"`
		Solution                string   `json:"solution"`
		UserStories             []string `json:"userStories"`
		ImplementationDecisions []string `json:"implementationDecisions"`
		TestingDecisions        []string `json:"testingDecisions"`
		OutOfScope              []string `json:"outOfScope"`
		FurtherNotes            []string `json:"furtherNotes"`
	}
	decoder := json.NewDecoder(strings.NewReader(text[start : start+end]))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return domain.SpecDocument{}, fmt.Errorf("%w: 解析 Spec 控制块: %v", ErrInvalidSpecOutput, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return domain.SpecDocument{}, fmt.Errorf("%w: Spec 控制块只能包含一个 JSON 值", ErrInvalidSpecOutput)
	}
	documentValue, err := domain.NormalizeSpecDocument(domain.SpecDocument{
		Problem:                 document.Problem,
		Solution:                document.Solution,
		UserStories:             document.UserStories,
		ImplementationDecisions: document.ImplementationDecisions,
		TestingDecisions:        document.TestingDecisions,
		OutOfScope:              document.OutOfScope,
		FurtherNotes:            document.FurtherNotes,
	})
	if err != nil {
		return domain.SpecDocument{}, fmt.Errorf("%w: 必填章节为空", ErrInvalidSpecOutput)
	}
	return documentValue, nil
}

var _ app.SpecGenerator = (*Generator)(nil)
