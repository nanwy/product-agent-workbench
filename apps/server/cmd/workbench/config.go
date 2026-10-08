package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	defaultAddress         = "127.0.0.1:8080"
	defaultConcurrency     = 2
	defaultDrainInterval   = 250 * time.Millisecond
	defaultShutdownTimeout = 10 * time.Second
)

var errInvalidConfig = errors.New("Workbench 启动配置无效")

// processConfig 只保存进程级配置，不保存请求或领域状态。
//
// 前端类比：它类似 Vite 在启动时读取的环境配置。区别是这些路径和可执行文件会授予后端
// 访问本机资源的能力，因此只能来自部署环境，不能来自浏览器请求。
type processConfig struct {
	Address          string
	DatabasePath     string
	ArtifactRoot     string
	WorktreeRoot     string
	TargetConfigPath string
	CodexExecutable  string
	CodexPrefixArgs  []string
	GitExecutable    string
	Concurrency      int
	DrainInterval    time.Duration
	ShutdownTimeout  time.Duration
}

// loadProcessConfig 只在进程启动时读取一次环境变量。
func loadProcessConfig() (processConfig, error) {
	config := processConfig{
		Address:          valueOrDefault("WORKBENCH_ADDRESS", defaultAddress),
		DatabasePath:     valueOrDefault("WORKBENCH_DATABASE_PATH", ".data/server/workbench.db"),
		ArtifactRoot:     valueOrDefault("WORKBENCH_ARTIFACT_ROOT", ".data/server/artifacts"),
		WorktreeRoot:     valueOrDefault("WORKBENCH_WORKTREE_ROOT", ".data/server/worktrees"),
		TargetConfigPath: valueOrDefault("WORKBENCH_TARGET_CONFIG", "config/target-project.json"),
		GitExecutable:    valueOrDefault("WORKBENCH_GIT_COMMAND", "git"),
		Concurrency:      defaultConcurrency,
		DrainInterval:    defaultDrainInterval,
		ShutdownTimeout:  defaultShutdownTimeout,
	}

	if raw := strings.TrimSpace(os.Getenv("WORKBENCH_CONCURRENCY")); raw != "" {
		concurrency, err := strconv.Atoi(raw)
		if err != nil {
			return processConfig{}, fmt.Errorf("%w: WORKBENCH_CONCURRENCY 必须是整数", errInvalidConfig)
		}
		config.Concurrency = concurrency
	}

	var err error
	config.DatabasePath, err = absolutePath(config.DatabasePath)
	if err != nil {
		return processConfig{}, fmt.Errorf("%w: database path: %v", errInvalidConfig, err)
	}
	config.ArtifactRoot, err = absolutePath(config.ArtifactRoot)
	if err != nil {
		return processConfig{}, fmt.Errorf("%w: artifact root: %v", errInvalidConfig, err)
	}
	config.WorktreeRoot, err = absolutePath(config.WorktreeRoot)
	if err != nil {
		return processConfig{}, fmt.Errorf("%w: worktree root: %v", errInvalidConfig, err)
	}
	config.TargetConfigPath, err = absolutePath(config.TargetConfigPath)
	if err != nil {
		return processConfig{}, fmt.Errorf("%w: target config path: %v", errInvalidConfig, err)
	}
	config.CodexExecutable, config.CodexPrefixArgs, err = resolveCodexLaunch()
	if err != nil {
		return processConfig{}, fmt.Errorf("%w: Codex runtime: %v", errInvalidConfig, err)
	}

	if err := validateProcessConfig(config); err != nil {
		return processConfig{}, err
	}
	return config, nil
}

// resolveCodexLaunch 让 Go 和旧前端共用 package.json 锁定的 Codex 版本。
//
// 前端类比：不能让生产页面偷偷使用全局安装的 React；这里同样通过项目内的
// node_modules 启动 Codex。WORKBENCH_CODEX_COMMAND 是部署者显式提供的逃生口，设置后
// 才直接使用 PATH 中的命令。
func resolveCodexLaunch() (string, []string, error) {
	if override := strings.TrimSpace(os.Getenv("WORKBENCH_CODEX_COMMAND")); override != "" {
		return override, nil, nil
	}

	nodeExecutable, err := exec.LookPath("node")
	if err != nil {
		return "", nil, fmt.Errorf("查找 node: %w", err)
	}
	workbenchRoot, err := findWorkbenchRoot()
	if err != nil {
		return "", nil, err
	}
	wrapper := filepath.Join(workbenchRoot, "apps", "web", "node_modules", "@openai", "codex", "bin", "codex.js")
	info, err := os.Stat(wrapper)
	if err != nil {
		return "", nil, fmt.Errorf("查找项目固定的 Codex CLI %s: %w；请先运行 pnpm install", wrapper, err)
	}
	if info.IsDir() {
		return "", nil, fmt.Errorf("项目固定的 Codex CLI 不是文件: %s", wrapper)
	}
	return nodeExecutable, []string{wrapper}, nil
}

func findWorkbenchRoot() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("WORKBENCH_ROOT")); configured != "" {
		return absolutePath(configured)
	}
	current, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("读取当前目录: %w", err)
	}
	for {
		candidate := filepath.Join(current, "apps", "web", "package.json")
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("无法从 %s 定位 Workbench 根目录；请设置 WORKBENCH_ROOT", current)
		}
		current = parent
	}
}

func validateProcessConfig(config processConfig) error {
	host, _, err := net.SplitHostPort(config.Address)
	if err != nil {
		return fmt.Errorf("%w: WORKBENCH_ADDRESS: %v", errInvalidConfig, err)
	}
	isLocalhost := strings.EqualFold(host, "localhost")
	ip := net.ParseIP(host)
	isLoopbackIP := ip != nil && ip.IsLoopback()
	if !isLocalhost && !isLoopbackIP {
		// 当前 HTTP API 没有身份认证，所以绝不能默认暴露到局域网或公网。
		return fmt.Errorf("%w: WORKBENCH_ADDRESS 必须绑定 loopback", errInvalidConfig)
	}
	if config.Concurrency < 1 ||
		config.DrainInterval <= 0 ||
		config.ShutdownTimeout <= 0 ||
		strings.TrimSpace(config.CodexExecutable) == "" ||
		strings.TrimSpace(config.GitExecutable) == "" {
		return errInvalidConfig
	}
	return nil
}

func valueOrDefault(key string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func absolutePath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errInvalidConfig
	}
	path, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	return filepath.Clean(path), nil
}
