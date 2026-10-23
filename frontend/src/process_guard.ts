import { confirmDialog } from './dialog'
import { findCommandProcesses, killCommand, store } from './store'

/**
 * 在系统进程表中查找与指令相关的进程，列出候选并二次确认后强制结束。
 * 不依赖 lastPid：手动启动、其他工具启动、启动器闪退遗留的进程都能被识别。
 */
export async function confirmKillInSystem(wsId: string, commandId: string, name: string): Promise<boolean> {
  const procs = await findCommandProcesses(wsId, commandId)
  if (procs.length === 0) {
    store.error = `未在系统中找到「${name}」相关进程`
    return false
  }

  const lines = procs.slice(0, 8).map((p) => {
    const target = p.executable || p.commandLine || '未知路径'
    return `PID ${p.pid}  ${target}\n${p.reason}`
  })
  if (procs.length > 8) lines.push(`… 另有 ${procs.length - 8} 个进程`)

  const ok = await confirmDialog({
    title: `结束进程 · ${name}`,
    message: `在系统中找到 ${procs.length} 个相关进程，强制结束后不可恢复。`,
    lines,
    danger: true,
    confirmText: '全部结束',
  })
  if (!ok) return false
  return killCommand(wsId, commandId)
}
