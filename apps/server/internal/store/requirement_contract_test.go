package store_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"product-agent-workbench/internal/domain"
	"product-agent-workbench/internal/store"
)

// storeFactory 为每个契约用例创建全新 Store。
//
// Memory 会返回新 map；SQLite 会返回使用独立临时数据库的 Adapter。
// 这样测试之间不会共享数据，契约也不需要知道底层是 map 还是 SQLite。
type storeFactory func(t *testing.T) store.Store

// runRequirementStoreContract 验证所有 Store Adapter 必须遵守的 Requirement 行为。
//
// 它只通过 store.Store 公开接口观察结果，不查询 Adapter 内部表或 map。
// 练习 12 第一片：先让 SQLite 通过这组契约，再解锁 TaskRun。
func runRequirementStoreContract(t *testing.T, factory storeFactory) {
	t.Helper()

	t.Run("Requirement 创建并拒绝重复 ID", func(t *testing.T) {
		subject := factory(t)
		ctx := context.Background()
		original := requirementFixture()

		if err := subject.CreateRequirement(ctx, original); err != nil {
			t.Fatalf("CreateRequirement() error = %v", err)
		}
		if err := subject.CreateRequirement(ctx, original); !errors.Is(err, store.ErrAlreadyExists) {
			t.Fatalf("重复 CreateRequirement() error = %v, want ErrAlreadyExists", err)
		}
	})

	t.Run("Requirement 按 ID 读取", func(t *testing.T) {
		subject := factory(t)
		ctx := context.Background()
		original := requirementFixture()
		if err := subject.CreateRequirement(ctx, original); err != nil {
			t.Fatalf("CreateRequirement() error = %v", err)
		}

		got, err := subject.GetRequirement(ctx, original.ID)
		if err != nil {
			t.Fatalf("GetRequirement() error = %v", err)
		}
		if got != original {
			t.Fatalf("GetRequirement() = %#v, want %#v", got, original)
		}

		missing := original
		missing.ID = "req-missing"
		if _, err := subject.GetRequirement(ctx, missing.ID); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("读取不存在 Requirement error = %v, want ErrNotFound", err)
		}
	})

	t.Run("Requirement 乐观锁更新", func(t *testing.T) {
		subject := factory(t)
		ctx := context.Background()
		original := requirementFixture()
		if err := subject.CreateRequirement(ctx, original); err != nil {
			t.Fatalf("CreateRequirement() error = %v", err)
		}

		updated := original
		updated.Title = "新的标题"
		updated.Stage = domain.RequirementStageSpec
		updated.Revision = 999 // Store 根据 expectedRevision 计算新版本，不信任该字段。

		newRevision, err := subject.SaveRequirement(ctx, updated, original.Revision)
		if err != nil {
			t.Fatalf("SaveRequirement() error = %v", err)
		}
		if newRevision != original.Revision+1 {
			t.Fatalf("SaveRequirement() revision = %d, want %d", newRevision, original.Revision+1)
		}

		got, err := subject.GetRequirement(ctx, original.ID)
		if err != nil {
			t.Fatalf("更新后 GetRequirement() error = %v", err)
		}
		if got.Title != updated.Title || got.Stage != updated.Stage || got.Revision != newRevision {
			t.Fatalf("更新后 GetRequirement() = %#v", got)
		}

		stale := updated
		stale.Title = "过期请求不能覆盖的新标题"
		if _, err := subject.SaveRequirement(ctx, stale, original.Revision); !errors.Is(err, store.ErrRevisionConflict) {
			t.Fatalf("过期 SaveRequirement() error = %v, want ErrRevisionConflict", err)
		}

		got, err = subject.GetRequirement(ctx, original.ID)
		if err != nil {
			t.Fatalf("冲突后 GetRequirement() error = %v", err)
		}
		if got.Title != updated.Title || got.Revision != newRevision {
			t.Fatalf("版本冲突修改了已保存值：got %#v", got)
		}

		missing := original
		missing.ID = "req-missing"
		if _, err := subject.SaveRequirement(ctx, missing, 1); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("更新不存在 Requirement error = %v, want ErrNotFound", err)
		}
	})

	t.Run("Requirement 创建遵守 context 取消", func(t *testing.T) {
		subject := factory(t)
		cancelled, cancel := context.WithCancel(context.Background())
		cancel()

		if err := subject.CreateRequirement(cancelled, requirementFixture()); !errors.Is(err, context.Canceled) {
			t.Fatalf("CreateRequirement(cancelled) error = %v, want context.Canceled", err)
		}
	})

	t.Run("Requirement 并发更新只允许一个成功", func(t *testing.T) {
		subject := factory(t)
		ctx := context.Background()
		original := requirementFixture()
		if err := subject.CreateRequirement(ctx, original); err != nil {
			t.Fatalf("CreateRequirement() error = %v", err)
		}

		start := make(chan struct{})
		errorsFromSave := make(chan error, 2)
		var wait sync.WaitGroup
		for index := range 2 {
			wait.Add(1)
			go func() {
				defer wait.Done()
				<-start

				candidate := original
				candidate.Title = "并发标题 " + string(rune('A'+index))
				_, err := subject.SaveRequirement(ctx, candidate, original.Revision)
				errorsFromSave <- err
			}()
		}
		close(start)
		wait.Wait()
		close(errorsFromSave)

		var succeeded int
		var conflicted int
		for err := range errorsFromSave {
			switch {
			case err == nil:
				succeeded++
			case errors.Is(err, store.ErrRevisionConflict):
				conflicted++
			default:
				t.Fatalf("并发 SaveRequirement() 返回意外错误：%v", err)
			}
		}
		if succeeded != 1 || conflicted != 1 {
			t.Fatalf("并发更新结果 success=%d conflict=%d, want 1/1", succeeded, conflicted)
		}
	})
}
