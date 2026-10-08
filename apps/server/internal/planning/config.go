package planning

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Target 保存 21B/21C 规划阶段允许读取和修改的受信任目标边界。
type Target struct {
	RepositoryRoot    string
	ModificationScope string
}

// LoadRepositoryRoot 只投影目标配置中的仓库路径，浏览器不能覆盖这个执行边界。
func LoadRepositoryRoot(configPath string) (string, error) {
	target, err := LoadTarget(configPath)
	if err != nil {
		return "", err
	}
	return target.RepositoryRoot, nil
}

func LoadTarget(configPath string) (Target, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return Target{}, fmt.Errorf("读取目标项目配置: %w", err)
	}
	var config struct {
		RepositoryRoot    string `json:"repositoryRoot"`
		ModificationScope string `json:"modificationScope"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return Target{}, fmt.Errorf("解析目标项目配置: %w", err)
	}
	root := strings.TrimSpace(config.RepositoryRoot)
	if root == "" {
		return Target{}, ErrInvalidGenerator
	}
	if !filepath.IsAbs(root) {
		root = filepath.Join(filepath.Dir(configPath), root)
	}
	root, err = filepath.EvalSymlinks(filepath.Clean(root))
	if err != nil {
		return Target{}, fmt.Errorf("解析目标项目仓库: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return Target{}, fmt.Errorf("%w: 目标项目仓库不是目录", ErrInvalidGenerator)
	}
	scope := filepath.ToSlash(filepath.Clean(strings.TrimSpace(config.ModificationScope)))
	invalidScope := scope == "" || scope == "." || filepath.IsAbs(scope) ||
		scope == ".." || strings.HasPrefix(scope, "../")
	if invalidScope {
		return Target{}, fmt.Errorf("%w: modificationScope 必须是仓库内相对路径", ErrInvalidGenerator)
	}
	return Target{RepositoryRoot: root, ModificationScope: scope}, nil
}
