package scheduler_test

import (
	"reflect"
	"testing"

	"product-agent-workbench/internal/domain"
	"product-agent-workbench/internal/scheduler"
)

// TestReadyTickets_DiamondDAG 固定练习 13 的第一个公开 seam。
//
// A、B 是可以并行入队的根；C 必须等待 A、B 全部成功；D 必须等待 C 成功。
// 测试只观察 ReadyTickets 的返回值，不关心你内部使用几次循环或建立哪些 map。
func TestReadyTickets_DiamondDAG(t *testing.T) {
	version := scheduler.TicketVersion{
		ID:            "req-1-tickets-v1",
		RequirementID: "req-1",
		Tickets: []scheduler.Ticket{
			{ID: "A"},
			{ID: "B"},
			{ID: "C", BlockedBy: []string{"A", "B"}},
			{ID: "D", BlockedBy: []string{"C"}},
		},
	}

	tests := []struct {
		name     string
		snapshot scheduler.ExecutionSnapshot
		wantIDs  []string
	}{
		{name: "首次计算释放两个并行根", snapshot: scheduler.ExecutionSnapshot{}, wantIDs: []string{"A", "B"}},
		{
			name: "一个根完成而另一个仍在队列时汇合节点继续阻塞",
			snapshot: scheduler.ExecutionSnapshot{
				"A": domain.TaskStatusCompleted,
				"B": domain.TaskStatusQueued,
			},
			wantIDs: []string{},
		},
		{
			name: "两个根都完成后释放汇合节点",
			snapshot: scheduler.ExecutionSnapshot{
				"A": domain.TaskStatusCompleted,
				"B": domain.TaskStatusCompleted,
			},
			wantIDs: []string{"C"},
		},
		{
			name: "汇合节点完成后释放链式后继",
			snapshot: scheduler.ExecutionSnapshot{
				"A": domain.TaskStatusCompleted,
				"B": domain.TaskStatusCompleted,
				"C": domain.TaskStatusCompleted,
			},
			wantIDs: []string{"D"},
		},
		{
			name: "失败的前置票不能解锁后继",
			snapshot: scheduler.ExecutionSnapshot{
				"A": domain.TaskStatusCompleted,
				"B": domain.TaskStatusFailed,
			},
			wantIDs: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ready, err := scheduler.ReadyTickets(version, tt.snapshot)
			if err != nil {
				t.Fatalf("ReadyTickets() error = %v", err)
			}
			gotIDs := make([]string, 0, len(ready))
			for _, ticket := range ready {
				gotIDs = append(gotIDs, ticket.ID)
			}
			if !reflect.DeepEqual(gotIDs, tt.wantIDs) {
				t.Fatalf("ReadyTickets() IDs = %v, want %v", gotIDs, tt.wantIDs)
			}
		})
	}
}
