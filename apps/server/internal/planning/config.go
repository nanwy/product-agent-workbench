package planning

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LoadRepositoryRoot 只投影目标配置中的仓库路径，浏览器不能覆盖这个执行边界。
func LoadRepositoryRoot(configPath string) (string, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return "", fmt.Errorf("读取目标项目配置: %w", err)
	}
	var config struct {
		RepositoryRoot string `json:"repositoryRoot"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return "", fmt.Errorf("解析目标项目配置: %w", err)
	}
	root := strings.TrimSpace(config.RepositoryRoot)
	if root == "" {
		return "", ErrInvalidGenerator
	}
	if !filepath.IsAbs(root) {
		root = filepath.Join(filepath.Dir(configPath), root)
	}
	root, err = filepath.EvalSymlinks(filepath.Clean(root))
	if err != nil {
		return "", fmt.Errorf("解析目标项目仓库: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("%w: 目标项目仓库不是目录", ErrInvalidGenerator)
	}
	return root, nil
}
