// 后端 API 薄封装：直接使用 Wails 注入的 window.go.main.App，避免依赖生成文件命名
import type { LaunchCommand, LogLine, ProcessInfo, RuntimeInfo, ShellSessionInfo, Workspace } from './types'

interface Backend {
  GetRuntimeInfo(): Promise<RuntimeInfo>
  ChooseDirectory(): Promise<string>
  CreateWorkspace(name: string, rootPath: string): Promise<Workspace>
  ListWorkspaces(): Promise<Workspace[]>
  ScanWorkspace(id: string): Promise<Workspace>
  UpdateWorkspace(ws: Workspace): Promise<Workspace>
  DeleteWorkspace(id: string): Promise<void>
  AddCommand(workspaceId: string, mainEntryId: string, name: string): Promise<LaunchCommand>
  UpdateCommand(cmd: LaunchCommand): Promise<LaunchCommand>
  DeleteCommand(workspaceId: string, commandId: string): Promise<void>
  ReorderCommands(workspaceId: string, commandIds: string[]): Promise<void>
  StartCommand(workspaceId: string, commandId: string, mode: string): Promise<void>
  StopCommand(workspaceId: string, commandId: string): Promise<void>
  KillCommand(workspaceId: string, commandId: string): Promise<void>
  RestartCommand(workspaceId: string, commandId: string, mode: string): Promise<void>
  StartAll(workspaceId: string): Promise<Record<string, string>>
  StopAll(workspaceId: string): Promise<void>
  GetLog(commandId: string): Promise<LogLine[]>
  FindProcesses(workspaceId: string, commandId: string): Promise<ProcessInfo[]>
  RevealPath(path: string): Promise<void>
  OpenShellSession(workspaceId: string, commandId: string, dir: string): Promise<ShellSessionInfo | null>
  GetShellSession(commandId: string): Promise<ShellSessionInfo | null>
  CloseShellSession(commandId: string): Promise<void>
  RestartShellSession(commandId: string, dir: string): Promise<ShellSessionInfo | null>
  SwitchShellDir(commandId: string, dir: string): Promise<ShellSessionInfo | null>
  RunShellLine(commandId: string, line: string): Promise<string[]>
  GetShellBuffer(commandId: string): Promise<string>
  GetShellHistory(workspaceId: string): Promise<string[]>
  ClearShellHistory(workspaceId: string): Promise<string[]>
  GetShellFavorites(workspaceId: string): Promise<string[]>
  SetShellFavorite(workspaceId: string, line: string, on: boolean): Promise<string[]>
  GetGitBranches(workspaceId: string): Promise<Record<string, string>>
  GetGitRoot(dir: string): Promise<string>
}

const go = (window as any).go
const backend: Backend = go?.main?.App as Backend

export function isBackendReady(): boolean {
  return !!backend
}

export const api: Backend = backend

// —— 事件 / 窗口控制：直接走 Wails runtime 全局对象 ——
interface WailsRuntime {
  EventsOn(eventName: string, callback: (...data: any[]) => void): void
  EventsOff(eventName: string): void
  WindowMinimise(): void
  WindowToggleMaximise(): void
  Quit(): void
}

const rt = (window as any).runtime as WailsRuntime

export function eventsOn(eventName: string, callback: (...data: any[]) => void): void {
  rt?.EventsOn(eventName, callback)
}

export function eventsOff(eventName: string): void {
  rt?.EventsOff(eventName)
}

export function windowMinimise(): void {
  rt?.WindowMinimise()
}

export function windowToggleMaximise(): void {
  rt?.WindowToggleMaximise()
}

export function windowQuit(): void {
  rt?.Quit()
}
