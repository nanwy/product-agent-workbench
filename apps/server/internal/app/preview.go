package app

import (
	"context"
	"strings"

	"product-agent-workbench/internal/domain"
)

// ResolvePreviewWorkspace 使用已确认 TicketVersion 和执行历史寻找完整候选目录。
//
// 前端只能传 Requirement/TicketVersion ID，不能传 `/tmp/...` 路径；这与前端只传资源 ID、
// 服务端再查数据库得到文件地址是同一个信任边界。
func (a *Application) ResolvePreviewWorkspace(
	ctx context.Context,
	requirementID string,
	ticketVersionID string,
) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	requirementID = strings.TrimSpace(requirementID)
	ticketVersionID = strings.TrimSpace(ticketVersionID)
	if requirementID == "" || ticketVersionID == "" || a.tickets == nil {
		return "", domain.ErrInvalidInput
	}
	version, err := a.tickets.ReadConfirmedTicketVersion(ctx, requirementID, ticketVersionID)
	if err != nil {
		return "", err
	}
	history, err := a.store.ListTasksByTicketVersion(ctx, requirementID, ticketVersionID)
	if err != nil {
		return "", err
	}
	workspacePath, _, err := completedCandidateFacts(version, history)
	if err != nil {
		return "", err
	}
	return workspacePath, nil
}
