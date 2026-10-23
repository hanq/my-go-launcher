<script setup lang="ts">
import { ref, watch } from 'vue'
import Icon from './Icon.vue'
import {
  deleteWorkspace,
  killCommand,
  openShellForCommand,
  reorderCommands,
  restartCommand,
  scanWorkspace,
  selectCommand,
  selectedWorkspace,
  selectWorkspace,
  startAll,
  startCommand,
  stopAll,
  stopCommand,
  store,
} from '../store'
import { confirmKillInSystem } from '../process_guard'
import { confirmDialog } from '../dialog'
import type { LaunchCommand, Workspace } from '../types'

const emit = defineEmits<{
  (e: 'new-workspace'): void
  (e: 'add-command', ws: Workspace): void
  (e: 'open-env', ws: Workspace): void
}>()

const expanded = ref<Record<string, boolean>>({})

function toggle(ws: Workspace): void {
  // 已选中且已展开 → 收起；否则展开并选中
  if (store.selectedWorkspaceId === ws.id && expanded.value[ws.id]) {
    expanded.value[ws.id] = false
  } else {
    expanded.value[ws.id] = true
    selectWorkspace(ws.id)
  }
}

// 选中工作区时自动展开（含初始加载与切换）
watch(
  () => store.selectedWorkspaceId,
  (id) => {
    if (id) expanded.value[id] = true
  },
)

function isRunning(cmd: LaunchCommand): boolean {
  return cmd.status === 'running' || cmd.status === 'starting' || cmd.status === 'building'
}

function runningCount(ws: Workspace): number {
  return ws.commands.filter(isRunning).length
}

function isScanning(ws: Workspace): boolean {
  return !!store.scanningWorkspaces[ws.id]
}

async function onStart(cmd: LaunchCommand): Promise<void> {
  selectCommand(cmd.id)
  await startCommand(cmd.workspaceId, cmd.id, cmd.mode || 'run')
}

async function onRestart(cmd: LaunchCommand): Promise<void> {
  selectCommand(cmd.id)
  await restartCommand(cmd.workspaceId, cmd.id, cmd.mode || 'run')
}

async function onKillProcesses(cmd: LaunchCommand): Promise<void> {
  await confirmKillInSystem(cmd.workspaceId, cmd.id, cmd.name)
}

async function onStartAll(ws: Workspace): Promise<void> {
  if (ws.commands.length === 0 || runningCount(ws) >= ws.commands.length) return
  await startAll(ws.id)
}

async function onStopAll(ws: Workspace): Promise<void> {
  if (runningCount(ws) === 0) return
  await stopAll(ws.id)
}

async function onDelete(ws: Workspace): Promise<void> {
  const running = runningCount(ws)
  const message = [
    `该工作区下的 ${ws.commands.length} 条启动指令、环境变量配置都会被一并删除，且不可撤销。`,
    running > 0 ? `其中 ${running} 条指令正在运行，删除前会被强制停止。` : '',
  ]
    .filter(Boolean)
    .join('\n')
  const ok = await confirmDialog({
    title: `删除工作区 · ${ws.name}`,
    message,
    danger: true,
    confirmText: '删除',
  })
  if (!ok) return
  await deleteWorkspace(ws.id)
}

// —— 拖拽排序 ——
const draggingId = ref<string | null>(null)
const dragOverId = ref<string | null>(null)

function onDragStart(e: DragEvent, cmd: LaunchCommand): void {
  draggingId.value = cmd.id
  if (e.dataTransfer) {
    e.dataTransfer.effectAllowed = 'move'
    e.dataTransfer.setData('text/plain', cmd.id)
  }
}

function onDragOver(e: DragEvent, cmd: LaunchCommand): void {
  e.preventDefault()
  if (e.dataTransfer) e.dataTransfer.dropEffect = 'move'
  dragOverId.value = cmd.id
}

async function onDrop(e: DragEvent, target: LaunchCommand): Promise<void> {
  e.preventDefault()
  const ws = selectedWorkspace()
  const sourceId = draggingId.value
  draggingId.value = null
  dragOverId.value = null
  if (!sourceId || sourceId === target.id || !ws) return
  const arr = ws.commands
  const from = arr.findIndex((c) => c.id === sourceId)
  const to = arr.findIndex((c) => c.id === target.id)
  if (from < 0 || to < 0) return
  const [moved] = arr.splice(from, 1)
  arr.splice(to, 0, moved)
  await reorderCommands(ws.id, arr.map((c) => c.id))
}

function onDragEnd(): void {
  draggingId.value = null
  dragOverId.value = null
}
</script>

<template>
  <aside class="sidebar">
    <div class="sidebar-head">
      <span class="section-title">WORKSPACES</span>
      <button class="btn btn-primary btn-sm" @click="emit('new-workspace')">
        <Icon name="plus" :size="12" />新建
      </button>
    </div>

    <div class="sidebar-body">
      <div v-if="store.workspaces.length === 0" class="empty" style="height: auto; padding: 40px 16px">
        <div class="glyph"><Icon name="folder" :size="30" /></div>
        <div>尚未创建工作区<br />选择一个包含 go.mod 的目录</div>
      </div>

      <div
        v-for="ws in store.workspaces"
        :key="ws.id"
        class="ws"
        :class="{ open: expanded[ws.id], active: store.selectedWorkspaceId === ws.id }"
      >
        <div class="ws-head" @click="toggle(ws)">
          <span class="chev" :class="{ open: expanded[ws.id] }"><Icon name="chevron" :size="12" /></span>
          <Icon name="folderOpen" :size="13" class="ws-icon" />
          <span class="ws-name ellipsis">{{ ws.name }}</span>
          <span v-if="runningCount(ws) > 0" class="ws-run mono" :title="`${runningCount(ws)} 条指令运行中`">
            {{ runningCount(ws) }}/{{ ws.commands.length }}
          </span>
          <span class="ws-count mono" :title="`${ws.commands.length} 条启动指令`">{{ ws.commands.length }}</span>
        </div>

        <div v-if="expanded[ws.id]" class="ws-panel" @click.stop>
          <!-- 运行控制区：只读运行态，与配置类操作物理分区 -->
          <section class="ws-zone run-zone" aria-label="运行控制">
            <div class="zone-cap">
              <span class="zone-dot run"></span>运行控制
              <span class="zone-stat mono">{{ runningCount(ws) }}/{{ ws.commands.length }} 运行中</span>
            </div>
            <div class="zone-row">
              <button
                class="act act-run"
                :disabled="ws.commands.length === 0 || runningCount(ws) >= ws.commands.length"
                title="启动该工作区下所有未运行的指令"
                @click.stop="onStartAll(ws)"
              >
                <Icon name="play" :size="11" />全部启动
              </button>
              <button
                class="act act-stop"
                :disabled="runningCount(ws) === 0"
                title="优雅停止该工作区下所有运行中的指令"
                @click.stop="onStopAll(ws)"
              >
                <Icon name="stop" :size="11" />全部停止
              </button>
            </div>
          </section>

          <!-- 配置与管理区：低频操作，删除单独隔离在最右侧 -->
          <section class="ws-zone tool-zone" aria-label="配置与管理">
            <div class="zone-cap">
              <span class="zone-dot tool"></span>配置与管理
            </div>
            <div class="zone-row tool-row">
              <button
                class="act act-icon"
                :class="{ scanning: isScanning(ws) }"
                :disabled="isScanning(ws)"
                :title="isScanning(ws) ? '正在重新扫描 main 入口…' : '重新扫描目录中的 main 入口（不会改动已添加的指令）'"
                :aria-label="isScanning(ws) ? '正在扫描 main 入口' : '重新扫描 main 入口'"
                @click.stop="scanWorkspace(ws.id)"
              >
                <Icon name="refresh" :size="13" :class="{ spin: isScanning(ws) }" />
              </button>
              <button
                class="act act-icon act-accent"
                title="从扫描到的 main 入口添加启动指令"
                aria-label="添加启动指令"
                @click.stop="emit('add-command', ws)"
              >
                <Icon name="plus" :size="13" />
              </button>
              <button
                class="act act-icon"
                title="编辑工作区级环境变量"
                aria-label="工作区环境变量"
                @click.stop="emit('open-env', ws)"
              >
                <Icon name="variable" :size="13" />
              </button>
              <span class="zone-spacer"></span>
              <span class="danger-sep" aria-hidden="true"></span>
              <button
                class="act act-icon act-danger"
                title="删除工作区（需二次确认）"
                aria-label="删除工作区"
                @click.stop="onDelete(ws)"
              >
                <Icon name="trash" :size="13" />
              </button>
            </div>
          </section>

          <div class="ws-commands">
            <div
              v-for="cmd in ws.commands"
              :key="cmd.id"
              class="cmd-row"
              :class="{
                active: store.selectedCommandId === cmd.id,
                dragging: draggingId === cmd.id,
                'drag-over': dragOverId === cmd.id,
              }"
              draggable="true"
              @click="selectCommand(cmd.id)"
              @dragstart="onDragStart($event, cmd)"
              @dragover="onDragOver($event, cmd)"
              @drop="onDrop($event, cmd)"
              @dragend="onDragEnd"
            >
              <span class="dot" :class="cmd.status"></span>
              <div class="cmd-main">
                <div class="cmd-name ellipsis">{{ cmd.name }}</div>
                <div class="cmd-path ellipsis mono">{{ cmd.mainFile }}</div>
              </div>
              <div class="cmd-actions" @click.stop>
                <button
                  class="mini"
                  title="打开终端会话（在模块目录执行命令）"
                  @click="openShellForCommand(cmd.id)"
                >
                  <Icon name="terminal" :size="11" />
                </button>
                <template v-if="isRunning(cmd)">
                  <button class="mini danger" title="强杀进程" @click="killCommand(ws.id, cmd.id)">
                    <Icon name="kill" :size="11" />
                  </button>
                  <button class="mini" title="优雅关闭" @click="stopCommand(ws.id, cmd.id)">
                    <Icon name="stop" :size="11" />
                  </button>
                  <button class="mini" title="重启" @click="onRestart(cmd)">
                    <Icon name="restart" :size="11" />
                  </button>
                </template>
                <template v-else>
                  <button class="mini play" title="Run 启动" @click="onStart(cmd)">
                    <Icon name="play" :size="11" />
                  </button>
                  <button class="mini danger" title="在系统中查找并结束该指令的进程" @click="onKillProcesses(cmd)">
                    <Icon name="kill" :size="11" />
                  </button>
                </template>
              </div>
            </div>
            <div v-if="ws.commands.length === 0" class="empty" style="height: auto; padding: 16px 10px">
              <span class="mono" style="font-size: 10.5px">暂无启动指令 · 点击上方「添加指令」</span>
            </div>
          </div>
        </div>
      </div>
    </div>

    <div class="sidebar-foot">
      <span class="mono">{{ store.runtimeInfo.os }}/{{ store.runtimeInfo.arch }}</span>
      <span class="dot" :class="store.ready ? 'running' : ''"></span>
      <span class="mono">{{ store.ready ? 'BACKEND ONLINE' : 'CONNECTING…' }}</span>
    </div>
  </aside>
</template>

<style scoped>
.sidebar {
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  background: var(--bg-deep);
  border-right: 1px solid var(--line-faint);
}
.sidebar-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 12px 8px;
  gap: 8px;
}
.sidebar-body {
  flex: 1;
  overflow-y: auto;
  padding: 4px 8px 12px;
}
.ws {
  margin-bottom: 6px;
  border-radius: var(--radius-sm);
}
.ws-head {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 7px 8px;
  border-radius: var(--radius-sm);
  cursor: pointer;
  transition: background 0.12s;
}
.ws-head:hover {
  background: var(--surface-2);
}
.ws.active > .ws-head {
  background: var(--surface-2);
  box-shadow: inset 0 0 0 1px var(--line-faint);
}
.chev {
  color: var(--text-faint);
  transition: transform 0.15s;
  display: inline-flex;
}
.chev.open {
  transform: rotate(90deg);
}
.ws-icon {
  color: var(--accent);
  flex-shrink: 0;
}
.ws-name {
  flex: 1;
  font-size: 12.5px;
  font-weight: 600;
  color: #e6edf3;
}
.ws-run {
  font-size: 9.5px;
  color: var(--accent);
  background: var(--accent-soft);
  border: 1px solid rgba(56, 242, 200, 0.3);
  border-radius: 8px;
  padding: 0 6px;
  line-height: 15px;
  flex-shrink: 0;
}
.ws-count {
  font-size: 10px;
  color: var(--text-faint);
  background: var(--surface-3);
  border-radius: 8px;
  padding: 1px 7px;
  flex-shrink: 0;
}

/* ── 展开后的操作面板：两个明确分区 ── */
.ws-panel {
  margin: 2px 0 4px 8px;
  border: 1px solid var(--line-faint);
  border-radius: var(--radius-sm);
  background: var(--surface);
  overflow: hidden;
}
.ws-zone {
  padding: 6px 7px 7px;
}
.ws-zone + .ws-zone {
  border-top: 1px solid var(--line-faint);
}
.run-zone {
  background: linear-gradient(90deg, rgba(56, 242, 200, 0.09), rgba(56, 242, 200, 0.015));
  border-left: 2px solid rgba(56, 242, 200, 0.45);
}
.tool-zone {
  border-left: 2px solid var(--line-faint);
}
.zone-cap {
  display: flex;
  align-items: center;
  gap: 5px;
  margin-bottom: 5px;
  font-family: var(--mono);
  font-size: 9px;
  letter-spacing: 0.12em;
  color: var(--text-faint);
}
.zone-dot {
  width: 5px;
  height: 5px;
  border-radius: 50%;
  flex-shrink: 0;
}
.zone-dot.run {
  background: var(--accent);
  box-shadow: var(--glow);
}
.zone-dot.tool {
  background: var(--text-faint);
}
.zone-stat {
  margin-left: auto;
  font-size: 9px;
  letter-spacing: 0;
  color: var(--text-faint);
  text-transform: none;
}
.zone-row {
  display: flex;
  align-items: center;
  gap: 4px;
  flex-wrap: wrap;
}
.zone-spacer {
  flex: 1;
  min-width: 4px;
}
.danger-sep {
  width: 1px;
  align-self: stretch;
  min-height: 16px;
  background: var(--line-faint);
  margin: 0 2px;
}
.act {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 4px;
  height: 24px;
  padding: 0 8px;
  font-family: var(--mono);
  font-size: 10px;
  letter-spacing: 0.02em;
  color: var(--text-dim);
  background: var(--surface-2);
  border: 1px solid var(--line-faint);
  border-radius: 3px;
  cursor: pointer;
  transition: all 0.12s;
  white-space: nowrap;
}
.act:hover:not(:disabled) {
  color: var(--accent);
  border-color: var(--line);
  background: var(--surface-3);
}
.act:disabled {
  opacity: 0.35;
  cursor: not-allowed;
}
.act-run {
  color: #04231b;
  background: var(--accent);
  border-color: var(--accent);
  font-weight: 700;
}
.act-run:hover:not(:disabled) {
  color: #04231b;
  background: #5cf7d6;
  border-color: #5cf7d6;
}
.act-stop {
  color: var(--danger);
  border-color: rgba(255, 45, 120, 0.35);
}
.act-stop:hover:not(:disabled) {
  color: #fff;
  background: var(--danger);
  border-color: var(--danger);
}
.act-accent {
  color: var(--accent);
  border-color: rgba(56, 242, 200, 0.35);
  background: var(--accent-soft);
}
.act-danger {
  width: 26px;
  padding: 0;
  color: var(--danger);
  border-color: rgba(255, 45, 120, 0.28);
  background: transparent;
}
.act-danger:hover:not(:disabled) {
  color: #fff;
  background: var(--danger);
  border-color: var(--danger);
}
/* ── 配置与管理：单行图标按钮，悬停显示 title 提示 ── */
.tool-row {
  flex-wrap: nowrap;
  gap: 6px;
}
.act-icon {
  width: 26px;
  height: 26px;
  padding: 0;
}
.act-icon.scanning,
.act-icon.scanning:disabled {
  opacity: 1;
  color: var(--accent);
  border-color: var(--line);
  background: var(--surface-3);
}
.spin {
  animation: spin 0.9s linear infinite;
}
@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}

/* ── 指令列表 ── */
.ws-commands {
  padding: 4px 5px;
  border-top: 1px solid var(--line-faint);
}
.cmd-row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 5px 6px;
  border-radius: var(--radius-sm);
  cursor: pointer;
  transition: background 0.12s;
}
.cmd-row:hover {
  background: var(--surface-2);
}
.cmd-row.active {
  background: var(--accent-soft);
  box-shadow: inset 0 0 0 1px var(--line);
}
.cmd-row.dragging {
  opacity: 0.4;
}
.cmd-row.drag-over {
  box-shadow: 0 -2px 0 0 var(--accent);
}
.cmd-main {
  flex: 1;
  min-width: 0;
}
.cmd-name {
  font-size: 12px;
  color: var(--text);
}
.cmd-row.active .cmd-name {
  color: #eef3f7;
}
.cmd-path {
  font-size: 9.5px;
  color: var(--text-faint);
}
.cmd-actions {
  display: flex;
  gap: 2px;
  opacity: 0;
  transition: opacity 0.12s;
}
.cmd-row:hover .cmd-actions,
.cmd-row.active .cmd-actions {
  opacity: 1;
}
.mini {
  width: 20px;
  height: 20px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--surface-3);
  border: 1px solid var(--line-faint);
  border-radius: 3px;
  color: var(--text-dim);
  cursor: pointer;
  transition: all 0.12s;
}
.mini:hover {
  color: var(--accent);
  border-color: var(--line);
}
.mini.play {
  color: var(--accent);
}
.mini.danger:hover {
  color: var(--danger);
  border-color: var(--danger);
}
.sidebar-foot {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 12px;
  border-top: 1px solid var(--line-faint);
  font-size: 9.5px;
  letter-spacing: 0.06em;
  color: var(--text-faint);
}
</style>
