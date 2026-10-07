package main

import (
	"errors"
	"fmt"
	"net"
	"os"
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
	Address         string
	DatabasePath    string
	ArtifactRoot    string
	WorktreeRoot    string
	CodexExecutable string
	GitExecutable   string
	Concurrency     int
	DrainInterval   time.Duration
	ShutdownTimeout time.Duration
}

// loadProcessConfig 只在进程启动时读取一次环境变量。
func loadProcessConfig() (processConfig, error) {
	config := processConfig{
		Address:         valueOrDefault("WORKBENCH_ADDRESS", defaultAddress),
		DatabasePath:    valueOrDefault("WORKBENCH_DATABASE_PATH", ".data/server/workbench.db"),
		ArtifactRoot:    valueOrDefault("WORKBENCH_ARTIFACT_ROOT", ".data/server/artifacts"),
		WorktreeRoot:    valueOrDefault("WORKBENCH_WORKTREE_ROOT", ".data/server/worktrees"),
		CodexExecutable: valueOrDefault("WORKBENCH_CODEX_COMMAND", "codex"),
		GitExecutable:   valueOrDefault("WORKBENCH_GIT_COMMAND", "git"),
		Concurrency:     defaultConcurrency,
		DrainInterval:   defaultDrainInterval,
		ShutdownTimeout: defaultShutdownTimeout,
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

	if err := validateProcessConfig(config); err != nil {
		return processConfig{}, err
	}
	return config, nil
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
