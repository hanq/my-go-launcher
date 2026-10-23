package main

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	rePackageMain = regexp.MustCompile(`(?m)^\s*package\s+main\s*(//.*)?$`)
	reFuncMain    = regexp.MustCompile(`\bfunc\s+main\s*\(`)
	reModuleLine  = regexp.MustCompile(`(?m)^\s*module\s+(\S+)\s*$`)
)

// skipDirs 扫描时跳过的目录（避免 vendor / 前端依赖 / VCS 干扰）
var skipDirs = map[string]bool{
	"vendor":      true,
	"node_modules": true,
	".git":        true,
	".hg":         true,
	".svn":        true,
	".idea":       true,
	".vscode":     true,
	".cache":      true,
	".next":       true,
	".nuxt":       true,
}

// maxGoFileSize 单文件最大读取字节数（足以覆盖 package/func 声明）
const maxGoFileSize = 256 * 1024

// mainEntryIDFor 由路径推导稳定 ID（保证重扫后入口身份不漂移）
func mainEntryIDFor(moduleDir, mainDir string) string {
	h := sha1.Sum([]byte(moduleDir + "\x00" + mainDir))
	return hex.EncodeToString(h[:])[:16]
}

// scanRoot 扫描根目录，识别所有 go module 及其 main 入口（入库，不直接成为启动指令）
func scanRoot(root string) ([]*MainEntry, error) {
	root = filepath.Clean(root)

	gomodFiles := []string{}
	goFiles := []string{}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // 忽略无权限目录，继续扫描
		}
		if d.IsDir() {
			name := d.Name()
			if skipDirs[name] {
				return filepath.SkipDir
			}
			if path != root && strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		base := d.Name()
		switch {
		case base == "go.mod":
			gomodFiles = append(gomodFiles, path)
		case strings.HasSuffix(base, ".go") && !strings.HasSuffix(base, "_test.go"):
			goFiles = append(goFiles, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("扫描目录失败: %w", err)
	}

	// 读取每个 go.mod 的 module 路径
	modules := make([]*moduleInfo, 0, len(gomodFiles))
	for _, f := range gomodFiles {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		m := reModuleLine.FindSubmatch(data)
		path := ""
		if m != nil {
			path = strings.TrimSpace(string(m[1]))
		}
		if path == "" {
			// 没有 module 行时回退为目录名
			path = filepath.Base(filepath.Dir(f))
		}
		modules = append(modules, &moduleInfo{Dir: filepath.Dir(f), Path: path})
	}
	// 按路径长度降序，确保就近匹配最近的 go.mod
	sort.Slice(modules, func(i, j int) bool {
		return len(modules[i].Dir) > len(modules[j].Dir)
	})

	// 找到每个 main 文件所属的 module
	type mainFile struct {
		file string
		dir  string
	}
	mainFiles := []mainFile{}
	for _, f := range goFiles {
		dir := filepath.Dir(f)
		mod := nearestModule(modules, dir)
		if mod == nil {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		if len(data) > maxGoFileSize {
			data = data[:maxGoFileSize]
		}
		if rePackageMain.Match(data) && reFuncMain.Match(data) {
			mainFiles = append(mainFiles, mainFile{file: f, dir: dir})
		}
	}

	// 按 (moduleDir, mainDir) 去重归组
	entries := []*MainEntry{}
	seen := map[string]bool{}
	for _, mf := range mainFiles {
		mod := nearestModule(modules, mf.dir)
		if mod == nil {
			continue
		}
		key := mod.Dir + "\x00" + mf.dir
		if seen[key] {
			continue
		}
		seen[key] = true

		relDir, _ := filepath.Rel(mod.Dir, mf.dir)
		relFile, _ := filepath.Rel(mod.Dir, mf.file)
		// 名称：根 main 用模块名，子目录 main 用相对目录名（避免多 main 模块同名混淆）
		name := filepath.Base(mod.Path)
		if relDir != "." {
			name = filepath.ToSlash(relDir)
		}

		entries = append(entries, &MainEntry{
			ID:         mainEntryIDFor(mod.Dir, mf.dir),
			Name:       name,
			ModuleDir:  mod.Dir,
			ModulePath: mod.Path,
			MainDir:    mf.dir,
			MainFile:   filepath.ToSlash(relFile),
		})
	}

	// 稳定排序：按模块路径 + main 目录
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].ModuleDir != entries[j].ModuleDir {
			return entries[i].ModuleDir < entries[j].ModuleDir
		}
		return entries[i].MainDir < entries[j].MainDir
	})
	return entries, nil
}

type moduleInfo struct {
	Dir  string
	Path string
}

func nearestModule(modules []*moduleInfo, dir string) *moduleInfo {
	for _, m := range modules {
		if dir == m.Dir || strings.HasPrefix(dir, m.Dir+string(filepath.Separator)) {
			return m
		}
	}
	return nil
}
