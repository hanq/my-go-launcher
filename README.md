# Go Launcher

一个基于 **Wails + Vue3 + xterm.js** 的 Go 应用启动工作台。自动识别目录下的
`go.mod` 与 `main` 启动文件，用一键启动/停止/重启/构建/杀进程管理你的 Go 服务，
并把启动日志与功能路径以 Web 终端（xterm.js）托管到界面。

> 界面风格采用「赛博霓虹」基因（agent-taste-seed-system · code-genes/02）：
> 深蓝黑底、荧光青强调、1px 霓虹描边、等宽代码字体，极客/开发者工具气质。

## 功能

- **工作区（Workspace）**：创建时选择根目录，自动递归识别所有 `go.mod` 模块与
  含 `func main()` 的 main 包/启动文件（跳过 vendor / node_modules / VCS）。
- **会话变量注入**：工作区级环境变量 + 每个 main 独立的覆盖变量，启动时合并进
  子进程；另注入内置变量 `GOLAUNCHER_WORKSPACE` / `GOLAUNCHER_APP_NAME` /
  `GOLAUNCHER_MODULE_DIR` 等。
- **每应用能力**：`Run`（go run）、`Build`（编译后运行）、重启、关闭（优雅）、
  杀进程（强杀整棵进程树）。
- **一键启动 / 一键停止**：工作区级别批量控制全部应用。
- **Web 终端日志**：每个应用的 stdout/stderr/system 日志实时流入 xterm.js，
  按流着色（system 荧光青、stderr 品红），支持历史回放与清屏。
- **功能路径托管**：module 路径 / main 目录 / main 文件在界面展示，点击直达系统
  文件管理器定位。
- **跨平台**：Windows（优先）、Linux、macOS；进程树管理按平台拆分为
  `taskkill /T`（Windows）与进程组信号（Unix）。

## 技术栈

| 层 | 选型 |
|----|------|
| 桌面框架 | Wails v2（Go 后端） |
| UI | Vue 3 + TypeScript + Vite |
| 终端 | xterm.js（`@xterm/xterm` + `@xterm/addon-fit`） |
| 持久化 | JSON 文件（`os.UserConfigDir()/my-go-launcher/state.json`） |

## 目录结构

```
├── main.go              # 窗口入口（无边框、深色背景）
├── app.go               # 绑定到前端的全部方法 + 会话变量注入
├── models.go            # Workspace / GoApp 数据模型
├── scanner.go           # go.mod / main 文件识别
├── runner.go            # 进程管理 + 日志流
├── store.go             # 配置持久化
├── proc_windows.go      # Windows 进程树终止
├── proc_unix.go         # Unix 进程组终止
└── frontend/src/
    ├── App.vue          # 布局（标题栏 + 侧边栏 + 终端 + 配置面板）
    ├── store.ts         # 响应式状态 + 事件订阅 + 操作封装
    ├── api.ts           # Wails 后端调用封装
    └── components/      # TitleBar / Sidebar / TerminalPanel / AppDetail / ...
```

## 开发与构建

```bash
# 开发模式（热更新）
wails dev

# 生产构建
wails build

# 后端单元测试（扫描器识别）
go test ./...
```

## 使用

1. 点击左上角「新建」→ 选择包含 Go 项目的根目录（可含多个嵌套 module）。
2. 侧边栏展开工作区，自动列出识别出的 main 应用与启动文件路径。
3. 选中应用 → 右侧面板配置启动参数、应用级/工作区级环境变量 → 保存。
4. 点击 `Run` / `Build` 启动，终端实时滚动日志；`重启` / `关闭` / `杀进程` 控制进程。
5. 工作区工具条提供「启动 / 停止」一键批量控制与「重新扫描」。
