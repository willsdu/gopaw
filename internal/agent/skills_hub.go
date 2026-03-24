package agent

import (
	"context"

	"github.com/willisdu/gopaw/internal/app/routers"
)

// HubService 实现 Skills Hub 的搜索与安装，行为对齐 copaw/agents/skills_hub.py（ClawHub、GitHub、
// skills.sh、LobeHub 直链、SkillsMP、以及直链 JSON bundle）。
type HubService struct {
	mgr *ManagerService
}

// NewHubService 创建 Hub 客户端；必须传入非 nil 的 ManagerService，用于落盘技能与启用。
func NewHubService(mgr *ManagerService) *HubService {
	if mgr == nil {
		return nil
	}
	return &HubService{mgr: mgr}
}

// Search 调用远端 Hub 搜索接口（默认 clawhub.ai）。
func (s *HubService) Search(ctx context.Context, q string, limit int) ([]routers.HubSkillSpec, error) {
	return hubSearchSkills(ctx, q, limit)
}

// Install 根据 bundle_url 解析来源并拉取 bundle，写入 customized 技能目录并按需启用。
func (s *HubService) Install(ctx context.Context, req routers.HubInstallRequest) (routers.HubInstallResult, error) {
	return installSkillFromHub(ctx, s.mgr, req)
}
