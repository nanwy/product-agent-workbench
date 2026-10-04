package app_test

import (
	"context"
	"errors"
	"testing"

	"product-agent-workbench/internal/app"
	"product-agent-workbench/internal/domain"
	"product-agent-workbench/internal/store"
	"product-agent-workbench/internal/store/memory"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name         string
		dependencies app.Dependencies
		wantError    error
	}{
		{
			name: "依赖完整",
			dependencies: app.Dependencies{
				Store: memory.New(),
				NewID: func() string { return "req-1" },
			},
		},
		{
			name: "缺少 Store",
			dependencies: app.Dependencies{
				NewID: func() string { return "req-1" },
			},
			wantError: app.ErrInvalidDependencies,
		},
		{
			name: "缺少 ID 生成器",
			dependencies: app.Dependencies{
				Store: memory.New(),
			},
			wantError: app.ErrInvalidDependencies,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			generatedIDs := 0
			dependencies := tt.dependencies
			if dependencies.NewID != nil {
				originalNewID := dependencies.NewID
				dependencies.NewID = func() string {
					generatedIDs++
					return originalNewID()
				}
			}

			application, err := app.New(dependencies)
			if !errors.Is(err, tt.wantError) {
				t.Fatalf("New() error = %v, want %v", err, tt.wantError)
			}
			if tt.wantError != nil && application != nil {
				t.Fatalf("New() application = %#v, want nil", application)
			}
			if tt.wantError == nil && application == nil {
				t.Fatal("New() application = nil, want non-nil")
			}
			if generatedIDs != 0 {
				t.Fatalf("New() 调用了 ID 生成器 %d 次，want 0", generatedIDs)
			}
		})
	}
}

func TestApplication_CreateRequirement(t *testing.T) {
	t.Run("创建并返回初始 Requirement", func(t *testing.T) {
		storeAdapter := memory.New()
		application := newApplication(t, storeAdapter, "req-1")

		got, err := application.CreateRequirement(
			context.Background(),
			app.CreateRequirementInput{
				ProjectID: "  project-1  ",
				Title:     "  学习应用用例层  ",
			},
		)
		if err != nil {
			t.Fatalf("CreateRequirement() error = %v", err)
		}

		want := domain.Requirement{
			ID:        "req-1",
			ProjectID: "project-1",
			Title:     "学习应用用例层",
			Stage:     domain.RequirementStageGrilling,
			Revision:  1,
		}
		if got != want {
			t.Fatalf("CreateRequirement() = %#v, want %#v", got, want)
		}

		persisted, err := storeAdapter.GetRequirement(context.Background(), want.ID)
		if err != nil {
			t.Fatalf("GetRequirement() persisted error = %v", err)
		}
		if persisted != want {
			t.Fatalf("persisted Requirement = %#v, want %#v", persisted, want)
		}
	})

	t.Run("无效输入不会生成 ID 或写入", func(t *testing.T) {
		tests := []struct {
			name  string
			input app.CreateRequirementInput
		}{
			{
				name:  "缺少 ProjectID",
				input: app.CreateRequirementInput{Title: "标题"},
			},
			{
				name:  "Title 只有空白",
				input: app.CreateRequirementInput{ProjectID: "project-1", Title: " \t "},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				generatedIDs := 0
				application, err := app.New(app.Dependencies{
					Store: memory.New(),
					NewID: func() string {
						generatedIDs++
						return "req-1"
					},
				})
				if err != nil {
					t.Fatalf("New() error = %v", err)
				}

				if _, err := application.CreateRequirement(context.Background(), tt.input); !errors.Is(err, domain.ErrInvalidInput) {
					t.Fatalf("CreateRequirement() error = %v, want ErrInvalidInput", err)
				}
				if generatedIDs != 0 {
					t.Fatalf("无效输入生成了 %d 个 ID，want 0", generatedIDs)
				}
			})
		}
	})

	t.Run("空白生成 ID 被拒绝", func(t *testing.T) {
		application := newApplication(t, memory.New(), " \t ")
		_, err := application.CreateRequirement(
			context.Background(),
			app.CreateRequirementInput{ProjectID: "project-1", Title: "标题"},
		)
		if !errors.Is(err, domain.ErrInvalidInput) {
			t.Fatalf("CreateRequirement() error = %v, want ErrInvalidInput", err)
		}
	})

	t.Run("重复 ID 透传 Store 冲突", func(t *testing.T) {
		storeAdapter := memory.New()
		application := newApplication(t, storeAdapter, "req-1")
		input := app.CreateRequirementInput{ProjectID: "project-1", Title: "标题"}

		if _, err := application.CreateRequirement(context.Background(), input); err != nil {
			t.Fatalf("第一次 CreateRequirement() error = %v", err)
		}
		if _, err := application.CreateRequirement(context.Background(), input); !errors.Is(err, store.ErrAlreadyExists) {
			t.Fatalf("重复 CreateRequirement() error = %v, want ErrAlreadyExists", err)
		}
	})
}

func TestApplication_GetRequirement(t *testing.T) {
	storeAdapter := memory.New()
	application := newApplication(t, storeAdapter, "req-1")
	want := domain.Requirement{
		ID:        "req-1",
		ProjectID: "project-1",
		Title:     "读取 Requirement",
		Stage:     domain.RequirementStageGrilling,
		Revision:  1,
	}
	if err := storeAdapter.CreateRequirement(context.Background(), want); err != nil {
		t.Fatalf("CreateRequirement() fixture error = %v", err)
	}

	t.Run("清理 ID 并返回实体", func(t *testing.T) {
		got, err := application.GetRequirement(context.Background(), "  req-1  ")
		if err != nil {
			t.Fatalf("GetRequirement() error = %v", err)
		}
		if got != want {
			t.Fatalf("GetRequirement() = %#v, want %#v", got, want)
		}
	})

	t.Run("空白 ID 返回输入错误", func(t *testing.T) {
		_, err := application.GetRequirement(context.Background(), " \t ")
		if !errors.Is(err, domain.ErrInvalidInput) {
			t.Fatalf("GetRequirement() error = %v, want ErrInvalidInput", err)
		}
	})

	t.Run("不存在时透传 Store 错误", func(t *testing.T) {
		_, err := application.GetRequirement(context.Background(), "req-missing")
		if !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("GetRequirement() error = %v, want ErrNotFound", err)
		}
	})

	t.Run("取消的 context 原样传播", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		if _, err := application.GetRequirement(ctx, want.ID); !errors.Is(err, context.Canceled) {
			t.Fatalf("GetRequirement(cancelled) error = %v, want context.Canceled", err)
		}
		if _, err := application.CreateRequirement(
			ctx,
			app.CreateRequirementInput{ProjectID: "project-1", Title: "标题"},
		); !errors.Is(err, context.Canceled) {
			t.Fatalf("CreateRequirement(cancelled) error = %v, want context.Canceled", err)
		}
	})
}

func newApplication(
	t *testing.T,
	storeAdapter store.Store,
	id string,
) *app.Application {
	t.Helper()

	application, err := app.New(app.Dependencies{
		Store: storeAdapter,
		NewID: func() string { return id },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return application
}
