<script setup lang="ts">
import Icon from './Icon.vue'
import { killCommand, loadGitBranches, restartCommand, revealPath, selectedCommand, startCommand, stopCommand, store } from '../store'
import { confirmKillInSystem } from '../process_guard'
import { confirmDialog } from '../dialog'

function cmd() {
  return selectedCommand()
}

const branch = () => (cmd() ? store.gitBranches[cmd()!.id] || '' : '')

async function onRefreshBranches(): Promise<void> {
  const c = cmd()
  if (!c) return
  await loadGitBranches(c.workspaceId)
}

function isRunning(status: string): boolean {
  return status === 'running' || status === 'starting' || status === 'building'
}

async function onRun(mode: 'run' | 'build'): Promise<void> {
  const c = cmd()
  if (!c) return
  await startCommand(c.workspaceId, c.id, mode)
}

async function onRestart(): Promise<void> {
  const c = cmd()
  if (!c) return
  await restartCommand(c.workspaceId, c.id, c.mode || 'run')
}

async function onStop(): Promise<void> {
  const c = cmd()
  if (!c) return
  await stopCommand(c.workspaceId, c.id)
}

async function onKill(): Promise<void> {
  const c = cmd()
  if (!c) return
  if (isRunning(c.status)) {
    const ok = await confirmDialog({
      title: `结束进程 · ${c.name}`,
      message: '将强制结束该指令的整棵进程树，未保存的数据可能丢失。',
      danger: true,
      confirmText: '强制结束',
    })
    if (!ok) return
    await killCommand(c.workspaceId, c.id)
    return
  }
  await confirmKillInSystem(c.workspaceId, c.id, c.name)
}
</script>

<template>
  <div class="app-header">
    <template v-if="cmd()">
      <div class="title-zone">
        <div class="name-row">
          <span class="dot" :class="cmd()!.status"></span>
          <span class="name mono">{{ cmd()!.name }}</span>
          <span class="badge" :class="cmd()!.mode === 'build' ? 'build' : 'run'">
            {{ cmd()!.mode === 'build' ? 'BUILD' : 'RUN' }}
          </span>
          <span v-if="cmd()!.pid" class="pid mono">PID {{ cmd()!.pid }}</span>
          <span v-if="branch()" class="branch-chip mono" :title="`当前分支：${branch()}`">
            <Icon name="gitBranch" :size="11" /><span class="bc-text">{{ branch() }}</span>
          </span>
          <button class="branch-refresh" title="刷新分支信息（在终端里切换分支后可点此更新）" @click="onRefreshBranches">
            <Icon name="refresh" :size="11" />
          </button>
        </div>
        <div class="path-row">
          <span class="path" title="在文件管理器中定位" @click="revealPath(cmd()!.moduleDir)">
            <Icon name="cube" :size="11" /> {{ cmd()!.modulePath }}
          </span>
          <span class="path-sep">▸</span>
          <span class="path" title="定位 main 文件" @click="revealPath(cmd()!.mainDir)">
            <Icon name="file" :size="11" /> {{ cmd()!.mainFile }}
          </span>
        </div>
      </div>

      <div class="actions">
        <button class="btn btn-primary" :disabled="isRunning(cmd()!.status)" title="go run 启动" @click="onRun('run')">
          <Icon name="play" :size="13" />Run
        </button>
        <button class="btn" :disabled="isRunning(cmd()!.status)" title="go build 编译后运行" @click="onRun('build')">
          <Icon name="build" :size="13" />Build
        </button>
        <button class="btn" :disabled="!isRunning(cmd()!.status)" title="重启" @click="onRestart">
          <Icon name="restart" :size="13" />重启
        </button>
        <button class="btn" :disabled="!isRunning(cmd()!.status)" title="优雅关闭" @click="onStop">
          <Icon name="stop" :size="13" />关闭
        </button>
        <button class="btn btn-danger" title="在系统中查找并强制结束该指令相关进程" @click="onKill">
          <Icon name="kill" :size="13" />杀进程
        </button>
      </div>
    </template>

    <div v-else class="empty" style="height: 100%">
      <div class="glyph"><Icon name="terminal" :size="34" /></div>
      <div>从左侧入口「添加指令」后，可在此查看日志与控制</div>
    </div>
  </div>
</template>

<style scoped>
.app-header {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px 16px;
  padding: 10px 16px;
  background: var(--surface);
  border-bottom: 1px solid var(--line-faint);
  flex-shrink: 0;
  min-height: 56px;
}
.title-zone {
  flex: 1 1 300px;
  min-width: 300px;
}
.name-row {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.name {
  font-size: 15px;
  font-weight: 700;
  color: #eef3f7;
  letter-spacing: 0.02em;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.pid {
  font-size: 10px;
  color: var(--text-faint);
  flex-shrink: 0;
}
.branch-chip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  max-width: 200px;
  padding: 1px 8px;
  font-size: 10px;
  color: var(--accent);
  background: var(--accent-soft);
  border: 1px solid rgba(56, 242, 200, 0.3);
  border-radius: 9px;
  line-height: 16px;
  flex-shrink: 0;
}
.branch-chip svg {
  flex-shrink: 0;
}
.bc-text {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.branch-refresh {
  width: 20px;
  height: 20px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: transparent;
  border: 1px solid var(--line-faint);
  border-radius: 3px;
  color: var(--text-faint);
  cursor: pointer;
  flex-shrink: 0;
  transition: all 0.12s;
}
.branch-refresh:hover {
  color: var(--accent);
  border-color: var(--line);
}
.path-row {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 4px;
  flex-wrap: wrap;
  min-width: 0;
}
.path {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 11px;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.path-sep {
  color: var(--text-faint);
  font-size: 10px;
  flex-shrink: 0;
}
.actions {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-left: auto;
  flex-shrink: 0;
}
</style>
