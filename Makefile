.PHONY: spa build start check

# 练习 20：先生成浏览器静态文件，随后 Go 编译时通过 embed.FS 把它们装进二进制。
spa:
	pnpm build:spa

build: spa
	go build -o apps/server/workbench ./apps/server/cmd/workbench

# 一个命令启动完整工作台；当前目录必须保持为 monorepo 根目录，配置相对路径才稳定。
start: spa
	go run ./apps/server/cmd/workbench

check:
	pnpm check
