<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import TitleBar from './components/TitleBar.vue'
import Sidebar from './components/Sidebar.vue'
import AppHeader from './components/AppHeader.vue'
import TerminalPanel from './components/TerminalPanel.vue'
import ShellPanel from './components/ShellPanel.vue'
import AppDetail from './components/AppDetail.vue'
import WorkspaceDialog from './components/WorkspaceDialog.vue'
import WorkspaceEnvDialog from './components/WorkspaceEnvDialog.vue'
import AddCommandDialog from './components/AddCommandDialog.vue'
import ConfirmDialog from './components/ConfirmDialog.vue'
import Icon from './components/Icon.vue'
import { clearError, refresh, store } from './store'
import type { Workspace } from './types'

const showNewWs = ref(false)
const envTarget = ref<Workspace | null>(null)
const addCmdTarget = ref<Workspace | null>(null)

// —— 三栏布局：分隔条拖拽调宽（localStorage 记忆，双击恢复默认） ——
const SIDEBAR_DEFAULT = 264
const DETAIL_DEFAULT = 300
const SIDEBAR_MIN = 200
const SIDEBAR_MAX = 480
const DETAIL_MIN = 220
const DETAIL_MAX = 460
const MIN_CENTER = 360
const SPLITTER_W = 5

function loadWidth(key: string, def: number, min: number, max: number): number {
  try {
    const raw = Number(localStorage.getItem(key))
    if (!Number.isFinite(raw) || raw <= 0) return def
    return Math.min(max, Math.max(min, raw))
  } catch {
    return def
  }
}

const sidebarWidth = ref(loadWidth('gl.layout.sidebar', SIDEBAR_DEFAULT, SIDEBAR_MIN, SIDEBAR_MAX))
const detailWidth = ref(loadWidth('gl.layout.detail', DETAIL_DEFAULT, DETAIL_MIN, DETAIL_MAX))

type SplitterKey = 'sidebar' | 'detail'
const activeSplitter = ref<SplitterKey | null>(null)
let dragStart = { x: 0, w: 0 }

function saveLayout(): void {
  try {
    localStorage.setItem('gl.layout.sidebar', String(sidebarWidth.value))
    localStorage.setItem('gl.layout.detail', String(detailWidth.value))
  } catch {
    /* ignore */
  }
}

/** 窗口变窄时等比收缩两侧，保证中间区最小可用宽度 */
function clampToViewport(): void {
  const avail = window.innerWidth - MIN_CENTER - SPLITTER_W * 2
  const total = sidebarWidth.value + detailWidth.value
  if (total <= avail) return
  const ratio = avail / total
  sidebarWidth.value = Math.max(SIDEBAR_MIN, Math.round(sidebarWidth.value * ratio))
  detailWidth.value = Math.max(DETAIL_MIN, Math.round(detailWidth.value * ratio))
}

function onSplitterDown(which: SplitterKey, e: PointerEvent): void {
  activeSplitter.value = which
  dragStart = { x: e.clientX, w: which === 'sidebar' ? sidebarWidth.value : detailWidth.value }
  ;(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId)
  document.body.classList.add('col-resizing')
}

function onSplitterMove(e: PointerEvent): void {
  const which = activeSplitter.value
  if (!which) return
  const dx = e.clientX - dragStart.x
  if (which === 'sidebar') {
    const max = Math.min(SIDEBAR_MAX, window.innerWidth - MIN_CENTER - DETAIL_MIN - SPLITTER_W * 2)
    sidebarWidth.value = Math.min(max, Math.max(SIDEBAR_MIN, dragStart.w + dx))
  } else {
    const max = Math.min(DETAIL_MAX, window.innerWidth - MIN_CENTER - SIDEBAR_MIN - SPLITTER_W * 2)
    detailWidth.value = Math.min(max, Math.max(DETAIL_MIN, dragStart.w - dx))
  }
}

function onSplitterUp(): void {
  if (!activeSplitter.value) return
  activeSplitter.value = null
  document.body.classList.remove('col-resizing')
  saveLayout()
}

function onSplitterDblClick(which: SplitterKey): void {
  if (which === 'sidebar') sidebarWidth.value = SIDEBAR_DEFAULT
  else detailWidth.value = DETAIL_DEFAULT
  saveLayout()
}

function onWindowResize(): void {
  clampToViewport()
}

onMounted(() => {
  refresh()
  clampToViewport()
  window.addEventListener('resize', onWindowResize)
})
onBeforeUnmount(() => {
  window.removeEventListener('resize', onWindowResize)
  document.body.classList.remove('col-resizing')
})
</script>

<template>
  <div class="root">
    <TitleBar />
    <div class="body">
      <Sidebar
        :style="{ width: sidebarWidth + 'px' }"
        @new-workspace="showNewWs = true"
        @open-env="(ws) => (envTarget = ws)"
        @add-command="(ws) => (addCmdTarget = ws)"
      />
      <div
        class="splitter"
        :class="{ active: activeSplitter === 'sidebar' }"
        title="拖拽调整侧边栏宽度（双击恢复默认）"
        @pointerdown="onSplitterDown('sidebar', $event)"
        @pointermove="onSplitterMove"
        @pointerup="onSplitterUp"
        @pointercancel="onSplitterUp"
        @dblclick="onSplitterDblClick('sidebar')"
      ></div>

      <main class="center">
        <AppHeader />
        <div class="center-tabs">
          <button class="tab" :class="{ active: store.centerTab === 'log' }" @click="store.centerTab = 'log'">
            <Icon name="list" :size="12" />运行日志
          </button>
          <button class="tab" :class="{ active: store.centerTab === 'shell' }" @click="store.centerTab = 'shell'">
            <Icon name="terminal" :size="12" />交互终端
          </button>
        </div>
        <TerminalPanel v-show="store.centerTab === 'log'" />
        <ShellPanel v-show="store.centerTab === 'shell'" />
      </main>

      <div
        class="splitter"
        :class="{ active: activeSplitter === 'detail' }"
        title="拖拽调整信息栏宽度（双击恢复默认）"
        @pointerdown="onSplitterDown('detail', $event)"
        @pointermove="onSplitterMove"
        @pointerup="onSplitterUp"
        @pointercancel="onSplitterUp"
        @dblclick="onSplitterDblClick('detail')"
      ></div>
      <AppDetail :style="{ width: detailWidth + 'px' }" />
    </div>

    <div v-if="store.loading && !store.ready" class="boot">
      <span class="boot-mark">⌁</span>
      <span class="mono">INITIALIZING…</span>
    </div>

    <div v-if="store.error" class="errorbar">
      <span>{{ store.error }}</span>
      <span class="close" @click="clearError">✕</span>
    </div>

    <WorkspaceDialog v-if="showNewWs" @close="showNewWs = false" />
    <WorkspaceEnvDialog v-if="envTarget" :ws="envTarget" @close="envTarget = null" />
    <AddCommandDialog v-if="addCmdTarget" :workspace-id="addCmdTarget.id" @close="addCmdTarget = null" />
    <ConfirmDialog />
  </div>
</template>

<style scoped>
.root {
  display: flex;
  flex-direction: column;
  height: 100%;
}
.body {
  flex: 1;
  display: flex;
  min-height: 0;
}
.center {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
}
.center-tabs {
  display: flex;
  align-items: center;
  gap: 2px;
  padding: 4px 10px 0;
  border-bottom: 1px solid var(--line-faint);
  flex-shrink: 0;
}
.tab {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 28px;
  padding: 0 12px;
  font-family: var(--mono);
  font-size: 11px;
  letter-spacing: 0.06em;
  color: var(--text-faint);
  background: transparent;
  border: none;
  border-bottom: 2px solid transparent;
  cursor: pointer;
  transition: color 0.12s;
}
.tab:hover {
  color: var(--text-dim);
}
.tab.active {
  color: var(--accent);
  border-bottom-color: var(--accent);
}
.boot {
  position: fixed;
  inset: 0;
  background: var(--bg-deep);
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 12px;
  z-index: 200;
  color: var(--text-dim);
  font-size: 12px;
  letter-spacing: 0.2em;
}
.boot-mark {
  color: var(--accent);
  font-size: 22px;
  text-shadow: var(--glow);
  animation: breathe 1.6s ease-in-out infinite;
}
.splitter {
  width: 5px;
  flex-shrink: 0;
  cursor: col-resize;
  position: relative;
  z-index: 30;
  touch-action: none;
  user-select: none;
}
.splitter::after {
  content: '';
  position: absolute;
  top: 0;
  bottom: 0;
  left: 50%;
  width: 1px;
  transform: translateX(-50%);
  background: transparent;
  transition: background 0.15s;
}
.splitter:hover::after,
.splitter.active::after {
  background: var(--accent);
  box-shadow: 0 0 6px rgba(56, 242, 200, 0.45);
}
</style>

<style>
/* 拖拽分隔条期间：全局锁定光标并禁用文本选择 */
body.col-resizing {
  cursor: col-resize !important;
  user-select: none;
}
body.col-resizing * {
  cursor: col-resize !important;
}
</style>
