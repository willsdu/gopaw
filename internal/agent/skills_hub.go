package agent

import (
	"context"
	"errors"

	"gopaw/internal/app/routers"
)

// HubService 负责 skills hub 能力（search/install），与管理能力分离。
// 目前作为独立服务入口，后续可对齐 copaw/agents/skills_hub.py 的远端拉取/安装逻辑。
type HubService struct{}

func NewHubService() *HubService {
	return &HubService{}
}

func (s *HubService) Search(
	ctx context.Context,
	q string,
	limit int,
) ([]routers.HubSkillSpec, error) {
	_ = ctx
	_ = q
	_ = limit
	return nil, errors.New("skills hub not implemented yet")
}

func (s *HubService) Install(
	ctx context.Context,
	req routers.HubInstallRequest,
) (routers.HubInstallResult, error) {
	_ = ctx
	_ = req
	return routers.HubInstallResult{}, errors.New("skills hub not implemented yet")
}
