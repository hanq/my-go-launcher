// 与 Go 后端结构对齐的类型定义

export type EnvMap = Record<string, string>

/** 扫描入库的 main 入口（尚未成为启动指令） */
export interface MainEntry {
  id: string
  workspaceId: string
  name: string // 默认模块名
  moduleDir: string
  modulePath: string
  mainDir: string
  mainFile: string
}

/** 一条启动指令：绑定 main 入口，可独立配置环境变量/参数/外显名 */
export interface LaunchCommand {
  id: string
  workspaceId: string
  mainEntryId: string
  name: string
  moduleDir: string
  modulePath: string
  mainDir: string
  mainFile: string
  envVars: EnvMap
  runArgs: string[]
  /** 自定义日志级别识别正则（捕获组 1 / 命名组 level 为级别词；留空用内置识别） */
  logLevelPattern?: string
  lastPid: number
  status: string
  pid: number
  mode: string
  exitCode: number
}

export interface Workspace {
  id: string
  name: string
  rootPath: string
  envVars: EnvMap
  /** 工作区级日志级别识别正则（指令未设置时生效） */
  logLevelPattern?: string
  createdAt: number
  mainEntries: MainEntry[]
  commands: LaunchCommand[]
}

export interface LogLine {
  stream: string // stdout | stderr | system
  line: string
  /** 后端日志环的单调递增序号，用于前端增量渲染与去重 */
  seq: number
  /** 入库时解析的级别（仅前端使用，识别规则见 log_level.ts） */
  lv?: 'debug' | 'info' | 'warn' | 'error' | 'other'
}

/** 系统进程表中匹配到的、属于某条启动指令的进程 */
export interface ProcessInfo {
  pid: number
  executable: string
  commandLine: string
  reason: string
}

export interface LogEvent {
  appId: string
  stream: string
  line: string
  /** 与 LogLine.seq 对应 */
  seq: number
}

export interface StatusEvent {
  appId: string
  status: string
  pid: number
  mode: string
  exitCode: number
}

export interface RuntimeInfo {
  os: string
  arch: string
  go: string
  version: string
}

export type AppStatus =
  | 'stopped'
  | 'starting'
  | 'building'
  | 'running'
  | 'stopping'
  | 'killed'
  | 'exited'
  | 'error'

/** 终端会话信息（交互 Shell，后端 ShellSessionInfo 对齐） */
export interface ShellSessionInfo {
  id: string
  workspaceId: string
  commandId: string
  dir: string
  shell: string
  startedAt: number
}

/** 终端会话输出事件（字节块） */
export interface ShellOutEvent {
  commandId: string
  data: string
}

/** 终端会话退出事件 */
export interface ShellExitEvent {
  commandId: string
  /** 会话 ID：会话被重启替换后，旧会话的退出事件据此被忽略 */
  sessionId: string
  code: number
}
