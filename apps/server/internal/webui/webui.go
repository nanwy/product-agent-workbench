package webui

import (
	"embed"
	"errors"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
)

var ErrInvalidDependencies = errors.New("Web UI 依赖无效")

// embeddedFiles 始终包含 fallback.html 和 dist/README.txt，因此未执行 Vite 构建时 Go 代码
// 仍可编译。正式的单命令启动会先生成 dist/index.html 与 assets，再执行 go run/build。
//
//go:embed fallback.html all:dist
var embeddedFiles embed.FS

// handler 同时持有 API 与只读静态资源。
//
// 前端类比：它相当于生产环境的反向代理配置：/api 交给后端，其余路径交给 SPA。
type handler struct {
	api      http.Handler
	static   http.Handler
	dist     fs.FS
	index    []byte
	spaReady bool
}

// New 创建生产 HTTP 入口，不启动监听端口。
func New(api http.Handler) (http.Handler, error) {
	if api == nil {
		return nil, ErrInvalidDependencies
	}
	dist, err := fs.Sub(embeddedFiles, "dist")
	if err != nil {
		return nil, err
	}
	fallback, err := embeddedFiles.ReadFile("fallback.html")
	if err != nil {
		return nil, err
	}
	return newHandler(api, dist, fallback), nil
}

func newHandler(api http.Handler, dist fs.FS, fallback []byte) http.Handler {
	index, err := fs.ReadFile(dist, "index.html")
	spaReady := err == nil
	if !spaReady {
		index = append([]byte(nil), fallback...)
	}
	return &handler{
		api:      api,
		static:   http.FileServer(http.FS(dist)),
		dist:     dist,
		index:    index,
		spaReady: spaReady,
	}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 第一步：API 优先。未知 /api 路径也必须由 API 返回 404，不能回退成 HTML。
	isAPI := r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/")
	if isAPI {
		h.api.ServeHTTP(w, r)
		return
	}

	// 第二步：静态页面只允许读取。业务写请求只能进入上面的显式 API 路由。
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", http.MethodGet+", "+http.MethodHead)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	requestedPath := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if requestedPath != "" && requestedPath != "." && requestedPath != "index.html" {
		if _, err := fs.Stat(h.dist, requestedPath); err == nil {
			// 固定文件名必须每次向服务器校验，避免升级二进制后浏览器继续使用旧 JS。
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			h.static.ServeHTTP(w, r)
			return
		}
		// 带扩展名通常是缺失资源；返回 HTML 会让浏览器把 404 页面当 JavaScript 解析。
		if path.Ext(requestedPath) != "" {
			http.NotFound(w, r)
			return
		}
	}

	// 第三步：/ 和无扩展名的前端路由都回退 index.html，交给浏览器 SPA 接管。
	status := http.StatusOK
	if !h.spaReady {
		status = http.StatusServiceUnavailable
	}
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(h.index)
}

// 注册 Vite 常见静态资源类型；平台缺少 mime 数据库时也能返回稳定 Content-Type。
func init() {
	_ = mime.AddExtensionType(".js", "text/javascript; charset=utf-8")
	_ = mime.AddExtensionType(".css", "text/css; charset=utf-8")
}
