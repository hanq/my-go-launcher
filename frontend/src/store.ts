import { reactive } from 'vue'
import { api, eventsOn } from './api'
import { compileLevelPattern, resolveLevel, type LogLevel } from './log_level'
import type {
  EnvMap,
  LaunchCommand,
  LogEvent,
  LogLine,
  ProcessInfo,
  RuntimeInfo,
  ShellExitEvent,
  ShellSessionInfo,
  StatusEvent,
  Workspace,
} from './types'

/** 终端会话目录选项 */
export type ShellDirChoice = 'module' | 'main' | 'root'

interface State {
  ready: boolean
  runtimeInfo: RuntimeInfo
  workspaces: Workspace[]
  selectedWorkspaceId: string
  selectedCommandId: string
  statuses: Record<string, StatusEvent>
  logs: Record<string, LogLine[]>
  logTick: number
  /** 每个指令被前端环形缓冲裁剪的行数，用于校正级别统计 */
  logTrim: Record<string, number>
  /** 级别规则版本：自定义规则变更后 +1，触发终端整体重建 */
  levelEpoch: number
  /** 中间列页签：log=运行日志 shell=交互终端 */
  centerTab: 'log' | 'shell'
  /** 终端会话（key: commandId） */
  shellSessions: Record<string, ShellSessionInfo>
  /** 每条指令最近一次选择的会话目录类型 */
  shellDirs: Record<string, ShellDirChoice>
  /** 选中工作区的终端历史指令（时间序，最近执行的在末尾） */
  shellHistory: string[]
  /** 选中工作区的收藏指令 */
  shellFavorites: string[]
  /** 工作区各指令的 git 分支（key: commandId；非 git 仓库为空串） */
  gitBranches: Record<string, string>
  loading: boolean
  scanningWorkspaces: Record<string, boolean>
  error: string
}

export const MAX_LOG_LINES = 4000

export const store = reactive<State>({
  ready: false,
  runtimeInfo: { os: '', arch: '', go: '', version: '' },
  workspaces: [],
  selectedWorkspaceId: '',
  selectedCommandId: '',
  statuses: {},
  logs: {},
  logTick: 0,
  logTrim: {},
  levelEpoch: 0,
  centerTab: 'log',
  shellSessions: {},
  shellDirs: {},
  shellHistory: [],
  shellFavorites: [],
  gitBranches: {},
  loading: false,
  scanningWorkspaces: {},
  error: '',
})

// —— 派生选择 ——
export function selectedWorkspace(): Workspace | undefined {
  return store.workspaces.find((w) => w.id === store.selectedWorkspaceId)
}

export function selectedCommand(): LaunchCommand | undefined {
  return selectedWorkspace()?.commands.find((c) => c.id === store.selectedCommandId)
}

export function commandById(commandId: string): { ws: Workspace; cmd: LaunchCommand } | undefined {
  for (const ws of store.workspaces) {
    const cmd = ws.commands.find((c) => c.id === commandId)
    if (cmd) return { ws, cmd }
  }
  return undefined
}

// —— 事件订阅（全局一次） ——
export function initStore(): void {
  eventsOn('app:log', (ev: LogEvent) => {
    appendLog(ev.appId, { stream: ev.stream, line: ev.line, seq: ev.seq ?? 0 })
  })
  eventsOn('app:status', (ev: StatusEvent) => {
    store.statuses[ev.appId] = ev
    const found = commandById(ev.appId)
    if (found) {
      found.cmd.status = ev.status
      found.cmd.pid = ev.pid
      found.cmd.exitCode = ev.exitCode
      if (ev.mode) found.cmd.mode = ev.mode
    }
  })
  eventsOn('shell:exit', (ev: ShellExitEvent) => {
    // 会话被重启替换后，旧会话的退出事件不能清掉新会话的状态
    const cur = store.shellSessions[ev.commandId]
    if (cur && ev.sessionId && cur.id !== ev.sessionId) return
    delete store.shellSessions[ev.commandId]
  })
}

// 每条指令的「续行锚点」：ERROR/WARN 后的无级别行（堆栈等）继承其级别
const levelAnchors: Record<string, LogLevel | null> = {}

// 每条指令生效的级别正则（指令级优先，未设置时回退工作区级；空 = 仅内置识别）
const patternCache = new Map<string, RegExp | null>()

// 依据当前工作区/指令配置重建级别正则缓存（refresh、指令增删改、工作区配置变更后调用）
function rebuildPatternCache(): void {
  patternCache.clear()
  for (const ws of store.workspaces) {
    const wsRaw = (ws.logLevelPattern || '').trim()
    for (const c of ws.commands) {
      const raw = (c.logLevelPattern || '').trim() || wsRaw
      patternCache.set(c.id, compileLevelPattern(raw).re)
    }
  }
}

// 级别规则变更后，重算该指令已有日志的级别并 +1 触发终端重建
function relevelCommand(commandId: string): void {
  const arr = store.logs[commandId]
  if (arr) {
    let anchor: LogLevel | null = null
    const re = patternCache.get(commandId) ?? null
    for (const e of arr) {
      const r = resolveLevel(e.line, anchor, re)
      e.lv = r.level
      anchor = r.anchor
    }
    levelAnchors[commandId] = anchor
  }
  store.levelEpoch++
}

// 入库时解析级别（识别规则见 log_level.ts），计数/着色/过滤统一使用 lv
function assignLevel(commandId: string, entry: LogLine): void {
  const r = resolveLevel(entry.line, levelAnchors[commandId] ?? null, patternCache.get(commandId) ?? null)
  entry.lv = r.level
  levelAnchors[commandId] = r.anchor
}

function appendLog(commandId: string, entry: LogLine): void {
  const arr = store.logs[commandId] || (store.logs[commandId] = [])
  const last = arr.length > 0 ? arr[arr.length - 1] : undefined
  // 事件与 GetLog 快照可能交错：seq 已存在则跳过，避免重复行
  if (last && entry.seq > 0 && entry.seq <= last.seq) return
  // 兑底：事件缺失 seq 时按前一条递增，保证增量渲染游标始终有效
  if (!entry.seq) entry.seq = (last?.seq ?? 0) + 1
  assignLevel(commandId, entry)
  arr.push(entry)
  if (arr.length > MAX_LOG_LINES) {
    const drop = arr.length - MAX_LOG_LINES
    arr.splice(0, drop)
    store.logTrim[commandId] = (store.logTrim[commandId] || 0) + drop
  }
  store.logTick++
}

// —— 数据加载 ——
export async function refresh(): Promise<void> {
  store.loading = true
  store.error = ''
  try {
    const info = await api.GetRuntimeInfo()
    store.runtimeInfo = info
    store.workspaces = await api.ListWorkspaces()
    rebuildPatternCache()
    if (!store.selectedWorkspaceId && store.workspaces.length > 0) {
      selectWorkspace(store.workspaces[0].id)
    } else if (store.selectedWorkspaceId) {
      const ws = store.workspaces.find((w) => w.id === store.selectedWorkspaceId)
      if (!ws) selectWorkspace(store.workspaces[0]?.id ?? '')
    }
    void loadGitBranches(store.selectedWorkspaceId)
    store.ready = true
  } catch (e: any) {
    store.error = String(e?.message ?? e)
  } finally {
    store.loading = false
  }
}

export function selectWorkspace(id: string): void {
  store.selectedWorkspaceId = id
  const ws = store.workspaces.find((w) => w.id === id)
  const first = ws?.commands[0]
  store.selectedCommandId = first?.id ?? ''
  if (first) loadLog(first.id)
  void loadShellData(id)
  void loadGitBranches(id)
}

export function selectCommand(commandId: string): void {
  store.selectedCommandId = commandId
  loadLog(commandId)
}

export async function loadLog(commandId: string): Promise<void> {
  if (!commandId) return
  try {
    const entries = (await api.GetLog(commandId)) ?? []
    const cur = store.logs[commandId]
    if (!cur || cur.length === 0) {
      for (const e of entries) assignLevel(commandId, e)
      store.logs[commandId] = entries
    } else {
      // 保留内存中比快照更新的行（修复运行中切回指令时日志被覆盖丢失）
      const lastSeq = cur[cur.length - 1]?.seq ?? 0
      const fresh = entries.filter((e) => (e.seq ?? 0) > lastSeq)
      for (const e of fresh) assignLevel(commandId, e)
      if (fresh.length > 0) cur.push(...fresh)
    }
    store.logTick++
  } catch {
    // 忽略：后端可能尚未就绪
  }
}

export function clearError(): void {
  store.error = ''
}

// —— 操作（带错误提示） ——
export async function run(op: () => Promise<unknown>): Promise<boolean> {
  store.error = ''
  try {
    await op()
    return true
  } catch (e: any) {
    store.error = String(e?.message ?? e)
    return false
  }
}

// —— 工作区操作 ——
export async function createWorkspace(name: string, rootPath: string): Promise<boolean> {
  const ok = await run(async () => {
    const ws = await api.CreateWorkspace(name, rootPath)
    await refresh()
    selectWorkspace(ws.id)
  })
  return ok
}

export async function scanWorkspace(id: string): Promise<boolean> {
  store.scanningWorkspaces[id] = true
  const ok = await run(async () => {
    const ws = await api.ScanWorkspace(id)
    const idx = store.workspaces.findIndex((w) => w.id === id)
    if (idx >= 0) store.workspaces[idx] = ws
    if (store.selectedWorkspaceId === id && !ws.commands.find((c) => c.id === store.selectedCommandId)) {
      store.selectedCommandId = ws.commands[0]?.id ?? ''
    }
    void loadGitBranches(id)
  })
  store.scanningWorkspaces[id] = false
  return ok
}

export async function deleteWorkspace(id: string): Promise<boolean> {
  const ok = await run(async () => {
    await api.DeleteWorkspace(id)
    if (store.selectedWorkspaceId === id) {
      store.selectedWorkspaceId = ''
      store.selectedCommandId = ''
    }
    await refresh()
  })
  return ok
}

export async function updateWorkspaceEnv(
  ws: Workspace,
  env: EnvMap,
  logLevelPattern?: string,
): Promise<boolean> {
  const updated = { ...ws, envVars: env, logLevelPattern: logLevelPattern ?? ws.logLevelPattern ?? '' }
  const ok = await run(async () => {
    const result = await api.UpdateWorkspace(updated)
    const idx = store.workspaces.findIndex((w) => w.id === ws.id)
    if (idx >= 0) store.workspaces[idx] = result
    rebuildPatternCache()
    // 工作区级规则变化会影响未单独设置规则的全部指令
    if ((ws.logLevelPattern || '').trim() !== (result.logLevelPattern || '').trim()) {
      for (const c of result.commands) relevelCommand(c.id)
    }
  })
  return ok
}

// —— 启动指令操作 ——
export async function addCommand(wsId: string, mainEntryId: string, name: string): Promise<boolean> {
  const ok = await run(async () => {
    const cmd = await api.AddCommand(wsId, mainEntryId, name)
    const ws = store.workspaces.find((w) => w.id === wsId)
    if (ws) ws.commands.push(cmd)
    rebuildPatternCache()
    if (!store.selectedCommandId) {
      store.selectedCommandId = cmd.id
      loadLog(cmd.id)
    }
    void loadGitBranches(wsId)
  })
  return ok
}

export async function updateCommand(cmd: LaunchCommand, patch: Partial<LaunchCommand>): Promise<boolean> {
  const updated: LaunchCommand = { ...cmd, ...patch }
  return run(async () => {
    const result = await api.UpdateCommand(updated)
    const found = commandById(cmd.id)
    if (found) {
      const i = found.ws.commands.findIndex((c) => c.id === cmd.id)
      if (i >= 0) found.ws.commands[i] = result
    }
    rebuildPatternCache()
    // 级别规则变化时，重算该指令已有日志的级别并刷新终端
    if ((cmd.logLevelPattern || '').trim() !== (result.logLevelPattern || '').trim()) {
      relevelCommand(cmd.id)
    }
  })
}

export async function deleteCommand(wsId: string, commandId: string): Promise<boolean> {
  const ok = await run(async () => {
    await api.DeleteCommand(wsId, commandId)
    const ws = store.workspaces.find((w) => w.id === wsId)
    if (ws) {
      const i = ws.commands.findIndex((c) => c.id === commandId)
      if (i >= 0) ws.commands.splice(i, 1)
    }
    patternCache.delete(commandId)
    if (store.selectedCommandId === commandId) {
      store.selectedCommandId = ws?.commands[0]?.id ?? ''
      if (store.selectedCommandId) loadLog(store.selectedCommandId)
    }
  })
  return ok
}

export async function reorderCommands(wsId: string, orderedIds: string[]): Promise<boolean> {
  return run(() => api.ReorderCommands(wsId, orderedIds))
}

export async function startCommand(wsId: string, commandId: string, mode: string): Promise<boolean> {
  return run(() => api.StartCommand(wsId, commandId, mode))
}

export async function stopCommand(wsId: string, commandId: string): Promise<boolean> {
  return run(() => api.StopCommand(wsId, commandId))
}

export async function killCommand(wsId: string, commandId: string): Promise<boolean> {
  return run(() => api.KillCommand(wsId, commandId))
}

/** 在系统进程表中查找该指令相关进程（不依赖历史 PID） */
export async function findCommandProcesses(wsId: string, commandId: string): Promise<ProcessInfo[]> {
  store.error = ''
  try {
    return await api.FindProcesses(wsId, commandId)
  } catch (e: any) {
    store.error = String(e?.message ?? e)
    return []
  }
}

export async function restartCommand(wsId: string, commandId: string, mode: string): Promise<boolean> {
  return run(() => api.RestartCommand(wsId, commandId, mode))
}

export async function startAll(wsId: string): Promise<boolean> {
  store.error = ''
  try {
    const results = await api.StartAll(wsId)
    const failures = Object.entries(results)
      .filter(([, v]) => v !== 'started' && v !== 'skipped')
      .map(([, v]) => v)
    if (failures.length > 0) {
      store.error = `部分指令启动失败：${failures.join('；')}`
      return false
    }
    return true
  } catch (e: any) {
    store.error = String(e?.message ?? e)
    return false
  }
}

export async function stopAll(wsId: string): Promise<boolean> {
  return run(() => api.StopAll(wsId))
}

export async function revealPath(path: string): Promise<boolean> {
  return run(() => api.RevealPath(path))
}

export async function chooseDirectory(): Promise<string> {
  try {
    return await api.ChooseDirectory()
  } catch (e: any) {
    store.error = String(e?.message ?? e)
    return ''
  }
}

// —— 终端会话（交互 Shell） ——

/** 解析目录选项对应的路径 */
export function shellDirPath(cmd: LaunchCommand, ws: Workspace | undefined, choice: ShellDirChoice): string {
  if (!cmd) return ''
  if (choice === 'main') return cmd.mainDir
  if (choice === 'root') return ws?.rootPath ?? ''
  return cmd.moduleDir
}

/** 加载工作区的终端历史与收藏（切换工作区时调用） */
export async function loadShellData(workspaceId: string): Promise<void> {
  if (!workspaceId) return
  try {
    const [hist, favs] = await Promise.all([api.GetShellHistory(workspaceId), api.GetShellFavorites(workspaceId)])
    // 异步返回时可能已切走工作区，避免串数据
    if (store.selectedWorkspaceId === workspaceId) {
      store.shellHistory = hist ?? []
      store.shellFavorites = favs ?? []
    }
  } catch {
    // 忽略：后端可能尚未就绪
  }
}

/** 加载工作区各指令的 git 分支（refresh / 切换工作区 / 手动刷新时调用） */
export async function loadGitBranches(workspaceId: string): Promise<void> {
  if (!workspaceId) return
  try {
    const m = await api.GetGitBranches(workspaceId)
    // 异步返回时可能已切走工作区，避免串数据
    if (store.selectedWorkspaceId === workspaceId) {
      store.gitBranches = m ?? {}
    }
  } catch {
    // 忽略：git 未安装或后端未就绪
  }
}

export async function openShellSession(commandId: string, dir: string): Promise<boolean> {
  const found = commandById(commandId)
  if (!found) return false
  return run(async () => {
    const info = await api.OpenShellSession(found.ws.id, commandId, dir)
    if (info) store.shellSessions[commandId] = info
  })
}

export async function closeShellSession(commandId: string): Promise<boolean> {
  const ok = await run(() => api.CloseShellSession(commandId))
  if (ok) delete store.shellSessions[commandId]
  return ok
}

/** 重开会话（dir 为空沿用原目录） */
export async function restartShellSession(commandId: string, dir = ''): Promise<boolean> {
  return run(async () => {
    const info = await api.RestartShellSession(commandId, dir)
    if (info) store.shellSessions[commandId] = info
  })
}

/** 会话内切换工作目录 */
export async function switchShellDir(commandId: string, dir: string): Promise<boolean> {
  return run(async () => {
    const info = await api.SwitchShellDir(commandId, dir)
    if (info) store.shellSessions[commandId] = info
  })
}

/** 提交一行指令（后端写入 Shell stdin 并记录历史），返回是否成功 */
export async function runShellLine(commandId: string, line: string): Promise<boolean> {
  const found = commandById(commandId)
  if (!found) return false
  return run(async () => {
    const hist = await api.RunShellLine(commandId, line)
    if (store.selectedWorkspaceId === found.ws.id) store.shellHistory = hist ?? []
  })
}

/** 获取会话输出回放缓冲（重新挂载时一次性回放） */
export async function fetchShellBuffer(commandId: string): Promise<string> {
  store.error = ''
  try {
    return (await api.GetShellBuffer(commandId)) ?? ''
  } catch {
    return ''
  }
}

export async function clearShellHistory(): Promise<boolean> {
  const wsId = store.selectedWorkspaceId
  if (!wsId) return false
  return run(async () => {
    store.shellHistory = (await api.ClearShellHistory(wsId)) ?? []
  })
}

export async function toggleShellFavorite(line: string, on: boolean): Promise<boolean> {
  const wsId = store.selectedWorkspaceId
  if (!wsId) return false
  return run(async () => {
    store.shellFavorites = (await api.SetShellFavorite(wsId, line, on)) ?? []
  })
}

/** 打开某模块的终端：选中模块 + 切到终端页签 + 确保会话开启（侧边栏「终端」按钮入口） */
export async function openShellForCommand(commandId: string): Promise<void> {
  const found = commandById(commandId)
  if (!found) return
  selectCommand(commandId)
  store.centerTab = 'shell'
  if (!store.shellSessions[commandId]) {
    const choice = store.shellDirs[commandId] ?? 'module'
    await openShellSession(commandId, shellDirPath(found.cmd, found.ws, choice))
  }
}
