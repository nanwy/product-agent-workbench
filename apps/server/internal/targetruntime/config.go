// Package targetruntime 管理目标项目开发服务与浏览器预览网关。
package targetruntime

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxConfigBytes = 1 << 20

var ErrInvalidConfig = errors.New("目标运行时配置无效")

// Command 对应前端 spawn(command, args)：参数保持数组，不经过 Shell 拼接。
type Command struct {
	Executable string
	Args       []string
}

// Config 只包含目标运行时真正需要的受信任配置。
//
// 前端类比：它像构建时注入的环境配置，而不是表单 state。浏览器不能改写这里的命令、
// 工作目录或代理地址，否则“启动预览”会退化成远程执行任意命令。
type Config struct {
	Name                  string
	RepositoryRoot        string
	ModificationScope     string
	StartCommand          Command
	HealthURL             *url.URL
	PreviewURL            *url.URL
	GatewayPort           int
	StartupTimeout        time.Duration
	ReuseHealthBodyMarker string
}

type configFile struct {
	Name              string `json:"name"`
	RepositoryRoot    string `json:"repositoryRoot"`
	ModificationScope string `json:"modificationScope"`
	StartCommand      struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
	} `json:"startCommand"`
	HealthURL          string `json:"healthUrl"`
	PreviewURL         string `json:"previewUrl"`
	PreviewGatewayPort int    `json:"previewGatewayPort"`
	StartupTimeoutMS   int64  `json:"startupTimeoutMs"`
	RuntimeReuse       *struct {
		HealthBodyIncludes string `json:"healthBodyIncludes"`
	} `json:"runtimeReuse"`
}

// LoadConfig 从仓库管理员维护的 JSON 中投影运行配置，并把相对仓库路径解析成绝对路径。
func LoadConfig(path string) (Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("打开目标运行时配置: %w", err)
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxConfigBytes+1))
	if err != nil {
		return Config{}, fmt.Errorf("读取目标运行时配置: %w", err)
	}
	if len(data) > maxConfigBytes {
		return Config{}, fmt.Errorf("%w: 配置超过 %d 字节", ErrInvalidConfig, maxConfigBytes)
	}
	var raw configFile
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&raw); err != nil {
		return Config{}, fmt.Errorf("%w: %v", ErrInvalidConfig, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return Config{}, fmt.Errorf("%w: 配置只能包含一个 JSON 值", ErrInvalidConfig)
	}

	repositoryRoot := strings.TrimSpace(raw.RepositoryRoot)
	if !filepath.IsAbs(repositoryRoot) {
		repositoryRoot = filepath.Join(filepath.Dir(path), repositoryRoot)
	}
	repositoryRoot, err = filepath.EvalSymlinks(filepath.Clean(repositoryRoot))
	if err != nil {
		return Config{}, fmt.Errorf("解析目标仓库: %w", err)
	}
	healthURL, err := parseLoopbackURL(raw.HealthURL)
	if err != nil {
		return Config{}, err
	}
	previewURL, err := parseLoopbackURL(raw.PreviewURL)
	if err != nil {
		return Config{}, err
	}
	marker := ""
	if raw.RuntimeReuse != nil {
		marker = strings.TrimSpace(raw.RuntimeReuse.HealthBodyIncludes)
		if marker == "" {
			return Config{}, fmt.Errorf("%w: runtimeReuse.healthBodyIncludes 不能为空", ErrInvalidConfig)
		}
	}
	config := Config{
		Name:                  strings.TrimSpace(raw.Name),
		RepositoryRoot:        repositoryRoot,
		ModificationScope:     filepath.ToSlash(filepath.Clean(strings.TrimSpace(raw.ModificationScope))),
		StartCommand:          Command{Executable: strings.TrimSpace(raw.StartCommand.Command), Args: append([]string{}, raw.StartCommand.Args...)},
		HealthURL:             healthURL,
		PreviewURL:            previewURL,
		GatewayPort:           raw.PreviewGatewayPort,
		StartupTimeout:        time.Duration(raw.StartupTimeoutMS) * time.Millisecond,
		ReuseHealthBodyMarker: marker,
	}
	if err := validateConfig(config); err != nil {
		return Config{}, err
	}
	return config, nil
}

func validateConfig(config Config) error {
	info, err := os.Stat(config.RepositoryRoot)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("%w: repositoryRoot 不是目录", ErrInvalidConfig)
	}
	scope := config.ModificationScope
	invalidScope := scope == "" || scope == "." || filepath.IsAbs(scope) || scope == ".." || strings.HasPrefix(scope, "../")
	if config.Name == "" || config.StartCommand.Executable == "" || invalidScope {
		return ErrInvalidConfig
	}
	if config.GatewayPort < 1024 || config.GatewayPort > 65535 || config.StartupTimeout < time.Second {
		return ErrInvalidConfig
	}
	if config.HealthURL == nil || config.PreviewURL == nil {
		return ErrInvalidConfig
	}
	return nil
}

func parseLoopbackURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("%w: URL 无效", ErrInvalidConfig)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("%w: URL 只允许 http/https", ErrInvalidConfig)
	}
	host := parsed.Hostname()
	ip := net.ParseIP(host)
	if !strings.EqualFold(host, "localhost") && (ip == nil || !ip.IsLoopback()) {
		return nil, fmt.Errorf("%w: URL 必须指向 loopback", ErrInvalidConfig)
	}
	return parsed, nil
}
