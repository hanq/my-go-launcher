package main

// EnvMap 环境变量键值对（会话注入用）
type EnvMap map[string]string

// Workspace 一个工作区：根路径 + 工作区级环境变量 + main 入口池 + 启动指令
type Workspace struct {
	ID              string           `json:"id"`
	Name            string           `json:"name"`
	RootPath        string           `json:"rootPath"`
	EnvVars         EnvMap           `json:"envVars"`
	LogLevelPattern string           `json:"logLevelPattern,omitempty"` // 工作区级日志级别识别正则（指令未设置时生效）
	CreatedAt       int64            `json:"createdAt"`
	MainEntries     []*MainEntry     `json:"mainEntries"` // 扫描入库的 main 入口池
	Commands        []*LaunchCommand `json:"commands"`    // 用户手动添加的启动指令

	ShellHistory   []string `json:"shellHistory,omitempty"`   // 终端历史指令（时间序，最近执行的在末尾，按工作区共享）
	ShellFavorites []string `json:"shellFavorites,omitempty"` // 终端收藏指令（按工作区共享）

	// 旧版状态迁移用（加载后转成 MainEntries + Commands，保存时不写出）
	LegacyApps []*legacyApp `json:"apps,omitempty"`
}

// MainEntry 扫描识别到的 main 入口（尚未成为启动指令，仅入库存留）
type MainEntry struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	Name        string `json:"name"` // 默认模块名
	ModuleDir   string `json:"moduleDir"`
	ModulePath  string `json:"modulePath"`
	MainDir     string `json:"mainDir"`
	MainFile    string `json:"mainFile"`
}

// LaunchCommand 一条启动指令：绑定一个 main 入口，可独立配置环境变量/参数/外显名
type LaunchCommand struct {
	ID              string   `json:"id"`
	WorkspaceID     string   `json:"workspaceId"`
	MainEntryID     string   `json:"mainEntryId"` // 关联的 main 入口
	Name            string   `json:"name"`        // 外显名（默认模块名，可改）
	ModuleDir       string   `json:"moduleDir"`   // 入口路径快照（自包含，入口被移除后仍可运行）
	ModulePath      string   `json:"modulePath"`
	MainDir         string   `json:"mainDir"`
	MainFile        string   `json:"mainFile"`
	EnvVars         EnvMap   `json:"envVars"`
	RunArgs         []string `json:"runArgs"`
	LogLevelPattern string   `json:"logLevelPattern,omitempty"` // 自定义日志级别识别正则（捕获组1/命名组level 为级别词；空=内置识别）
	LastPID         int      `json:"lastPid"`                   // 历史最后 PID（仅兼容旧数据兜底；清理进程以系统进程扫描为准）

	// —— 运行时状态（不持久化，由 Runner 覆盖）——
	Status   string `json:"status"`
	PID      int    `json:"pid"`
	Mode     string `json:"mode"`
	ExitCode int    `json:"exitCode"`
}

// legacyApp 旧版 GoApp 结构，仅用于 state.json 迁移
type legacyApp struct {
	ID          string   `json:"id"`
	WorkspaceID string   `json:"workspaceId"`
	Name        string   `json:"name"`
	ModuleDir   string   `json:"moduleDir"`
	ModulePath  string   `json:"modulePath"`
	MainDir     string   `json:"mainDir"`
	MainFile    string   `json:"mainFile"`
	EnvVars     EnvMap   `json:"envVars"`
	RunArgs     []string `json:"runArgs"`
	Status      string   `json:"status"`
	PID         int      `json:"pid"`
	Mode        string   `json:"mode"`
	ExitCode    int      `json:"exitCode"`
}

func cloneEnv(m EnvMap) EnvMap {
	if m == nil {
		return EnvMap{}
	}
	c := make(EnvMap, len(m))
	for k, v := range m {
		c[k] = v
	}
	return c
}

func cloneArgs(a []string) []string {
	if a == nil {
		return []string{}
	}
	c := make([]string, len(a))
	copy(c, a)
	return c
}

// Clone 深拷贝工作区
func (w *Workspace) Clone() *Workspace {
	c := &Workspace{
		ID:              w.ID,
		Name:            w.Name,
		RootPath:        w.RootPath,
		EnvVars:         cloneEnv(w.EnvVars),
		LogLevelPattern: w.LogLevelPattern,
		CreatedAt:       w.CreatedAt,
	}
	if w.ShellHistory != nil {
		c.ShellHistory = append([]string{}, w.ShellHistory...)
	}
	if w.ShellFavorites != nil {
		c.ShellFavorites = append([]string{}, w.ShellFavorites...)
	}
	if w.MainEntries != nil {
		c.MainEntries = make([]*MainEntry, 0, len(w.MainEntries))
		for _, e := range w.MainEntries {
			c.MainEntries = append(c.MainEntries, e.Clone())
		}
	}
	if w.Commands != nil {
		c.Commands = make([]*LaunchCommand, 0, len(w.Commands))
		for _, cmd := range w.Commands {
			c.Commands = append(c.Commands, cmd.Clone())
		}
	}
	return c
}

// Clone 深拷贝入口
func (m *MainEntry) Clone() *MainEntry {
	return &MainEntry{
		ID:          m.ID,
		WorkspaceID: m.WorkspaceID,
		Name:        m.Name,
		ModuleDir:   m.ModuleDir,
		ModulePath:  m.ModulePath,
		MainDir:     m.MainDir,
		MainFile:    m.MainFile,
	}
}

// Clone 深拷贝启动指令
func (c *LaunchCommand) Clone() *LaunchCommand {
	return &LaunchCommand{
		ID:              c.ID,
		WorkspaceID:     c.WorkspaceID,
		MainEntryID:     c.MainEntryID,
		Name:            c.Name,
		ModuleDir:       c.ModuleDir,
		ModulePath:      c.ModulePath,
		MainDir:         c.MainDir,
		MainFile:        c.MainFile,
		EnvVars:         cloneEnv(c.EnvVars),
		RunArgs:         cloneArgs(c.RunArgs),
		LogLevelPattern: c.LogLevelPattern,
		LastPID:         c.LastPID,
		Status:          c.Status,
		PID:             c.PID,
		Mode:            c.Mode,
		ExitCode:        c.ExitCode,
	}
}
