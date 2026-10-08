package planning

import (
	"context"
	"testing"

	"product-agent-workbench/internal/app"
	"product-agent-workbench/internal/domain"
)

type textTurnStub struct{ text string }

func (stub textTurnStub) RunTextTurn(context.Context, string, string) (string, error) {
	return stub.text, nil
}

func TestGeneratorParsesStructuredSpec(t *testing.T) {
	generator, err := NewGenerator(textTurnStub{text: `[WORKBENCH:SPEC]{
		"problem":"问题","solution":"方案","userStories":["故事"],
		"implementationDecisions":["实现"],"testingDecisions":["测试"],
		"outOfScope":["范围外"],"furtherNotes":["备注"]}[/WORKBENCH:SPEC]`}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	document, err := generator.Generate(context.Background(), app.SpecGenerationInput{
		Requirement: domain.Requirement{Title: "需求"},
		Understanding: domain.GrillingUnderstanding{
			Summary: "摘要", DesiredOutcome: "结果", ChangeBoundary: "apps/web", AcceptanceSignal: "可见",
		},
	})
	if err != nil || document.Solution != "方案" || len(document.TestingDecisions) != 1 {
		t.Fatalf("Generate() document=%#v error=%v", document, err)
	}
}
