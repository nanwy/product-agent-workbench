package verification

// Status 是单条验证记录允许出现的三种结果。
type Status string

const (
	StatusVerified Status = "verified"
	StatusFailed   Status = "failed"
	StatusNotRun   Status = "not-run"
)

// Evidence 是证据收集 Adapter 返回的原始事实。
type Evidence struct {
	ID       string
	Label    string
	Status   Status
	Detail   string
	Reason   string
	Required bool
}

// TODO(学习者):新增纯函数 BuildMatrix，把 Evidence 转为可展示的验证矩阵。
// 必须拒绝没有 Reason 的 not-run，并提供 Required 检查失败时阻止交付的判断方法。
