package verification

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

const maxPlanBytes = 1 << 20

// Environment 决定验证命令看到的 NODE_ENV。
type Environment string

const (
	EnvironmentProduction  Environment = "production"
	EnvironmentTest        Environment = "test"
	EnvironmentDevelopment Environment = "development"
)

// ProxyPolicy 决定验证子进程是否继承宿主代理变量。
type ProxyPolicy string

const (
	ProxyPolicyAuto    ProxyPolicy = "auto"
	ProxyPolicyInherit ProxyPolicy = "inherit"
	ProxyPolicyOmit    ProxyPolicy = "omit"
)

// Command 是一条来自受信任项目配置的声明式验证命令。
//
// Executable 与 Args 分开保存，执行器直接调用 exec.CommandContext，不经过 Shell。前端可以
// 把它理解成 `spawn(command, args)`，而不是 `eval(commandString)`。
type Command struct {
	ID              string
	Label           string
	Executable      string
	Args            []string
	Timeout         time.Duration
	Environment     Environment
	ProxyPolicy     ProxyPolicy
	Required        bool
	RequiresRuntime bool
}

// Plan 是 18B 从目标项目配置读取的可信投影。
type Plan struct {
	Commands  []Command
	HealthURL string
}

type planFile struct {
	ValidationCommands []commandFile `json:"validationCommands"`
	HealthURL          string        `json:"healthUrl"`
}

type commandFile struct {
	ID              string      `json:"id"`
	Label           string      `json:"label"`
	Executable      string      `json:"command"`
	Args            []string    `json:"args"`
	TimeoutMS       int64       `json:"timeoutMs"`
	Environment     Environment `json:"environment"`
	ProxyPolicy     ProxyPolicy `json:"proxyPolicy"`
	RequiresRuntime bool        `json:"requiresRuntime"`
}

// LoadCommands 只从仓库管理员提供的目标项目配置读取命令。
//
// 浏览器请求和 Agent 输出都不能传可执行文件或参数；否则一个“验证按钮”会退化成
// 远程 Shell。配置中的其它前端字段由对应模块读取，因此这里只投影验证所需字段。
func LoadPlan(path string) (Plan, error) {
	file, err := os.Open(path)
	if err != nil {
		return Plan{}, fmt.Errorf("打开 Verification 配置: %w", err)
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxPlanBytes+1))
	if err != nil {
		return Plan{}, fmt.Errorf("读取 Verification 配置: %w", err)
	}
	if len(data) > maxPlanBytes {
		return Plan{}, fmt.Errorf("%w: 配置超过 %d 字节", ErrInvalidPlan, maxPlanBytes)
	}

	var raw planFile
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&raw); err != nil {
		return Plan{}, fmt.Errorf("%w: %v", ErrInvalidPlan, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return Plan{}, fmt.Errorf("%w: 配置只能包含一个 JSON 值", ErrInvalidPlan)
	}
	commands := make([]Command, 0, len(raw.ValidationCommands))
	for _, item := range raw.ValidationCommands {
		commands = append(commands, Command{
			ID:              strings.TrimSpace(item.ID),
			Label:           strings.TrimSpace(item.Label),
			Executable:      strings.TrimSpace(item.Executable),
			Args:            append([]string{}, item.Args...),
			Timeout:         time.Duration(item.TimeoutMS) * time.Millisecond,
			Environment:     item.Environment,
			ProxyPolicy:     item.ProxyPolicy,
			Required:        true,
			RequiresRuntime: item.RequiresRuntime,
		})
	}
	if err := validateCommands(commands); err != nil {
		return Plan{}, err
	}
	return Plan{Commands: commands, HealthURL: strings.TrimSpace(raw.HealthURL)}, nil
}

// LoadCommands 保留给只需要命令列表的调用方；生产装配使用 LoadPlan 同时读取 healthUrl。
func LoadCommands(path string) ([]Command, error) {
	plan, err := LoadPlan(path)
	if err != nil {
		return nil, err
	}
	return plan.Commands, nil
}

func validateCommands(commands []Command) error {
	if len(commands) == 0 {
		return fmt.Errorf("%w: 至少需要一条验证命令", ErrInvalidPlan)
	}
	seen := make(map[string]struct{}, len(commands))
	for index := range commands {
		command := &commands[index]
		command.ID = strings.TrimSpace(command.ID)
		command.Label = strings.TrimSpace(command.Label)
		command.Executable = strings.TrimSpace(command.Executable)
		command.Args = append([]string{}, command.Args...)

		validIdentity := command.ID != "" && command.Label != "" && command.Executable != ""
		if !validIdentity || command.Timeout < time.Second {
			return fmt.Errorf("%w: 命令 %q 缺少字段或超时小于一秒", ErrInvalidPlan, command.ID)
		}
		if _, exists := seen[command.ID]; exists {
			return fmt.Errorf("%w: 命令 ID %q 重复", ErrInvalidPlan, command.ID)
		}
		seen[command.ID] = struct{}{}

		switch command.Environment {
		case EnvironmentProduction, EnvironmentTest, EnvironmentDevelopment:
		default:
			return fmt.Errorf("%w: 命令 %q 的 environment 无效", ErrInvalidPlan, command.ID)
		}
		switch command.ProxyPolicy {
		case ProxyPolicyAuto, ProxyPolicyInherit, ProxyPolicyOmit:
		default:
			return fmt.Errorf("%w: 命令 %q 的 proxyPolicy 无效", ErrInvalidPlan, command.ID)
		}
	}
	return nil
}
