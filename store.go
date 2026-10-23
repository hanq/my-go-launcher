package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type stateFile struct {
	Workspaces []*Workspace `json:"workspaces"`
	Window     *WindowState `json:"window,omitempty"`
}

// WindowState 窗口尺寸与位置（重启时恢复）
type WindowState struct {
	X         int  `json:"x"`
	Y         int  `json:"y"`
	Width     int  `json:"width"`
	Height    int  `json:"height"`
	Maximised bool `json:"maximised"`
}

// Store 负责工作区与应用配置的持久化（JSON 文件）
type Store struct {
	mu   sync.Mutex
	path string
	data *stateFile
}

func dataDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "my-go-launcher")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func NewStore() *Store {
	s := &Store{data: &stateFile{Workspaces: []*Workspace{}}}
	dir, err := dataDir()
	if err == nil {
		s.path = filepath.Join(dir, "state.json")
		s.load()
	}
	return s
}

func (s *Store) load() {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	var d stateFile
	if err := json.Unmarshal(raw, &d); err != nil {
		return
	}
	if d.Workspaces == nil {
		d.Workspaces = []*Workspace{}
	}
	// 旧版迁移：apps → mainEntries + commands
	for _, w := range d.Workspaces {
		s.migrate(w)
	}
	s.data = &d
}

// migrate 把旧版 apps 字段转成 MainEntries（入库池），不自动生成启动指令
func (s *Store) migrate(w *Workspace) {
	if len(w.LegacyApps) == 0 {
		w.LegacyApps = nil
		return
	}
	for _, app := range w.LegacyApps {
		me := &MainEntry{
			ID:          mainEntryIDFor(app.ModuleDir, app.MainDir),
			WorkspaceID: w.ID,
			Name:        app.Name, // 旧版已推导的名称（根 main=模块名，子目录=相对目录）
			ModuleDir:   app.ModuleDir,
			ModulePath:  app.ModulePath,
			MainDir:     app.MainDir,
			MainFile:    app.MainFile,
		}
		w.MainEntries = append(w.MainEntries, me)
	}
	w.LegacyApps = nil
}

func (s *Store) save() error {
	if s.path == "" {
		return nil
	}
	raw, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// shellHistoryMax 每工作区终端历史指令保留条数
const shellHistoryMax = 200

// findLocked 按 ID 查找工作区（调用方需已持有 s.mu）
func (s *Store) findLocked(id string) *Workspace {
	for _, w := range s.data.Workspaces {
		if w.ID == id {
			return w
		}
	}
	return nil
}

// RecordShellHistory 记录一条终端历史指令（erasedups 去重 + 容量截断），返回更新后的列表（时间序）
func (s *Store) RecordShellHistory(workspaceID, line string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	w := s.findLocked(workspaceID)
	if w == nil {
		return nil
	}
	line = strings.TrimRight(line, " \t")
	if line == "" {
		return append([]string{}, w.ShellHistory...)
	}
	out := make([]string, 0, len(w.ShellHistory)+1)
	for _, h := range w.ShellHistory {
		if h != line {
			out = append(out, h)
		}
	}
	out = append(out, line)
	if len(out) > shellHistoryMax {
		out = out[len(out)-shellHistoryMax:]
	}
	w.ShellHistory = out
	_ = s.save()
	return append([]string{}, out...)
}

// ClearShellHistory 清空终端历史指令，返回空列表
func (s *Store) ClearShellHistory(workspaceID string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	w := s.findLocked(workspaceID)
	if w == nil {
		return nil
	}
	w.ShellHistory = []string{}
	_ = s.save()
	return []string{}
}

// SetShellFavorite 收藏 / 取消收藏一条指令，返回更新后的收藏列表（时间序，最近添加在末尾）
func (s *Store) SetShellFavorite(workspaceID, line string, on bool) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	w := s.findLocked(workspaceID)
	if w == nil {
		return nil
	}
	line = strings.TrimRight(line, " \t")
	if line == "" {
		return append([]string{}, w.ShellFavorites...)
	}
	out := make([]string, 0, len(w.ShellFavorites)+1)
	for _, h := range w.ShellFavorites {
		if h != line {
			out = append(out, h)
		}
	}
	if on {
		out = append(out, line)
		if len(out) > shellHistoryMax {
			out = out[len(out)-shellHistoryMax:]
		}
	}
	w.ShellFavorites = out
	_ = s.save()
	return append([]string{}, out...)
}

// AddWorkspace 新建并持久化
func (s *Store) AddWorkspace(ws *Workspace) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Workspaces = append(s.data.Workspaces, ws)
	return s.save()
}

func (s *Store) List() []*Workspace {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Workspace, 0, len(s.data.Workspaces))
	for _, w := range s.data.Workspaces {
		out = append(out, w.Clone())
	}
	return out
}

func (s *Store) Get(id string) *Workspace {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, w := range s.data.Workspaces {
		if w.ID == id {
			return w.Clone()
		}
	}
	return nil
}

// Update 替换指定工作区（按 ID 匹配）
func (s *Store) Update(ws *Workspace) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, w := range s.data.Workspaces {
		if w.ID == ws.ID {
			s.data.Workspaces[i] = ws
			return s.save()
		}
	}
	return os.ErrNotExist
}

func (s *Store) GetWindowState() *WindowState {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.Window == nil {
		return nil
	}
	w := *s.data.Window
	return &w
}

func (s *Store) SetWindowState(ws *WindowState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Window = ws
	return s.save()
}

func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, w := range s.data.Workspaces {
		if w.ID == id {
			s.data.Workspaces = append(s.data.Workspaces[:i], s.data.Workspaces[i+1:]...)
			return s.save()
		}
	}
	return os.ErrNotExist
}

// FindCommand 跨工作区按 commandID 查找（返回其所属工作区与指令）
func (s *Store) FindCommand(commandID string) (*Workspace, *LaunchCommand) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, w := range s.data.Workspaces {
		for _, c := range w.Commands {
			if c.ID == commandID {
				return w.Clone(), c.Clone()
			}
		}
	}
	return nil, nil
}
