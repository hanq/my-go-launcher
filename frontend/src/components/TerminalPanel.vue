<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { SearchAddon } from '@xterm/addon-search'
import Icon from './Icon.vue'
import { store } from '../store'
import { parseLogLevel, type LogLevel } from '../log_level'
import { copyToClipboard } from '../clipboard'
import type { LogLine } from '../types'

const container = ref<HTMLDivElement>()
let term: Terminal | null = null
let fit: FitAddon | null = null
let search: SearchAddon | null = null
let searchSub: { dispose(): void } | null = null
let resizeObs: ResizeObserver | null = null

let currentApp = ''
/** 已写入终端的最后一条日志 seq（增量渲染游标） */
let renderedSeq = 0
/** 已执行「清屏」的 seq 水位：<= 该值的行不再回填 */
let clearedSeq = 0
let pendingWrites = 0
let seenTrim = 0

// —— 搜索 ——
const searchQuery = ref('')
const searchInfo = reactive({ index: -1, count: 0 })
const searchError = ref('')
let queuedSearch: { q: string; dir: 'next' | 'prev'; noScroll: boolean } | null = null

// —— 级别过滤 ——（级别在入库时解析并存于 LogLine.lv，识别规则见 log_level.ts）
type Level = LogLevel

const LEVELS: { key: Level; label: string; cls: string }[] = [
  { key: 'debug', label: 'DEBUG', cls: 'lv-debug' },
  { key: 'info', label: 'INFO', cls: 'lv-info' },
  { key: 'warn', label: 'WARN', cls: 'lv-warn' },
  { key: 'error', label: 'ERROR', cls: 'lv-error' },
  { key: 'other', label: 'OTHER', cls: 'lv-other' },
]

function levelOf(e: LogLine): Level {
  // 级别在入库时解析并存于 LogLine.lv（见 log_level.ts）；此处仅作兜底
  return e.lv ?? parseLogLevel(e.line) ?? 'other'
}

const activeLevels = ref<Level[]>([])
const counts = reactive<Record<Level, number>>({ debug: 0, info: 0, warn: 0, error: 0, other: 0 })

function allActive(): boolean {
  return activeLevels.value.length === 0
}

function levelActive(lv: Level): boolean {
  return activeLevels.value.includes(lv)
}

function resetCounts(): void {
  for (const lv of LEVELS) counts[lv.key] = 0
}

function toggleLevel(lv: Level): void {
  if (allActive()) {
    // 当前为 ALL：点谁就只看谁
    activeLevels.value = [lv]
  } else if (levelActive(lv)) {
    const next = activeLevels.value.filter((x) => x !== lv)
    activeLevels.value = next.length === 0 ? [] : next
  } else {
    const next = [...activeLevels.value, lv]
    // 全选等价于不过滤
    activeLevels.value = next.length === LEVELS.length ? [] : next
  }
  rebuild()
}

function showAllLevels(): void {
  if (allActive()) return
  activeLevels.value = []
  rebuild()
}

function matches(e: LogLine): boolean {
  return allActive() || activeLevels.value.includes(levelOf(e))
}

const shownCount = computed(() => {
  if (allActive()) return totalCount.value
  return LEVELS.filter((lv) => levelActive(lv.key)).reduce((sum, lv) => sum + counts[lv.key], 0)
})
const totalCount = computed(() => LEVELS.reduce((sum, lv) => sum + counts[lv.key], 0))

// —— ANSI 着色 ——
const ansi = {
  accent: (s: string) => `\x1b[38;2;56;242;200m${s}\x1b[0m`,
  danger: (s: string) => `\x1b[38;2;255;45;120m${s}\x1b[0m`,
  warn: (s: string) => `\x1b[38;2;255;176;45m${s}\x1b[0m`,
  dim: (s: string) => `\x1b[38;2;125;139;163m${s}\x1b[0m`,
}

function colorize(e: LogLine): string {
  if (e.stream === 'system') return ansi.accent(e.line)
  const level = levelOf(e)
  if (e.stream === 'stderr') return ansi.danger(e.line)
  if (level === 'error') return ansi.danger(e.line)
  if (level === 'warn') return ansi.warn(e.line)
  if (level === 'debug') return ansi.dim(e.line)
  return e.line
}

// —— 渲染（增量 + 过滤） ——
function firstAfterSeq(arr: LogLine[], seq: number): number {
  let lo = 0
  let hi = arr.length
  while (lo < hi) {
    const mid = (lo + hi) >> 1
    if ((arr[mid].seq ?? 0) > seq) hi = mid
    else lo = mid + 1
  }
  return lo
}

function writeLine(text: string): void {
  const t = term
  if (!t) return
  pendingWrites++
  try {
    t.writeln(text, () => {
      pendingWrites = Math.max(0, pendingWrites - 1)
      if (pendingWrites === 0) flushSearch()
    })
  } catch {
    pendingWrites = Math.max(0, pendingWrites - 1)
  }
}

function recount(): void {
  resetCounts()
  const arr = store.logs[currentApp] || []
  for (const e of arr) counts[levelOf(e)]++
}

function render(): void {
  if (!term) return
  const arr = store.logs[currentApp] || []
  if (arr.length === 0) return

  // 前端缓冲裁剪会移除旧行：校正统计（不影响已渲染内容）
  const trimmed = store.logTrim[currentApp] || 0
  if (trimmed !== seenTrim) {
    seenTrim = trimmed
    recount()
  }

  for (let i = firstAfterSeq(arr, renderedSeq); i < arr.length; i++) {
    const e = arr[i]
    counts[levelOf(e)]++
    if (e.seq > clearedSeq && matches(e)) writeLine(colorize(e))
    renderedSeq = e.seq
  }
}

/** 过滤条件变化 / 清屏后按当前条件整体重放 */
function rebuild(): void {
  if (!term) return
  term.reset()
  // reset 会丢弃未解析的写入缓冲，回调可能不再触发，这里显式归零
  pendingWrites = 0
  renderedSeq = 0
  seenTrim = store.logTrim[currentApp] || 0
  resetCounts()
  search?.clearDecorations()
  render()
  if (searchQuery.value.trim()) requestSearch('next', true)
}

function switchApp(appId: string): void {
  currentApp = appId
  pendingWrites = 0
  renderedSeq = 0
  clearedSeq = 0
  seenTrim = store.logTrim[appId] || 0
  resetCounts()
  searchInfo.index = -1
  searchInfo.count = 0
  searchError.value = ''
  queuedSearch = null
  search?.clearDecorations()
  term?.reset()
  render()
  if (searchQuery.value.trim()) requestSearch('next', true)
}

function clearScreen(): void {
  if (!term) return
  clearedSeq = Math.max(clearedSeq, renderedSeq)
  term.reset()
  pendingWrites = 0
  search?.clearDecorations()
  searchInfo.index = -1
  searchInfo.count = 0
}

function copySelection(): void {
  const sel = term?.getSelection()?.trim() || ''
  if (sel) {
    copyToClipboard(sel)
    return
  }
  const all = (store.logs[currentApp] || [])
    .filter((e) => e.seq > clearedSeq && matches(e))
    .map((e) => e.line)
    .join('\n')
  copyToClipboard(all)
}

function onContextMenu(e: MouseEvent): void {
  e.preventDefault()
  const sel = term?.getSelection()
  if (sel) copyToClipboard(sel)
}

// —— 搜索 ——
const SEARCH_DECO = {
  matchBackground: 'rgba(56,242,200,0.45)',
  matchOverviewRuler: '#38f2c8',
  activeMatchBackground: '#ff2d78',
  activeMatchColorOverviewRuler: '#ff2d78',
}

function requestSearch(dir: 'next' | 'prev', noScroll: boolean): void {
  const q = searchQuery.value.trim()
  if (!q) {
    search?.clearDecorations()
    searchInfo.index = -1
    searchInfo.count = 0
    queuedSearch = null
    return
  }
  queuedSearch = { q, dir, noScroll }
  // 终端写入是异步的：等本轮写入落地后再搜索，避免搜到旧缓冲
  if (pendingWrites === 0) flushSearch()
}

function flushSearch(): void {
  if (!queuedSearch || !search || !term || pendingWrites > 0) return
  const req = queuedSearch
  queuedSearch = null
  try {
    const opts = { caseSensitive: false, decorations: SEARCH_DECO }
    // 第三个参数 noScroll 未进 typings，但 addon 实现支持
    const run = req.dir === 'next' ? search.findNext : search.findPrevious
    ;(run as any).call(search, req.q, opts, { noScroll: req.noScroll })
    searchError.value = ''
    if (searchInfo.count === 0) searchInfo.index = -1
  } catch (e: any) {
    searchError.value = `搜索失败：${e?.message ?? e}`
  }
}

function doSearch(dir: 'next' | 'prev'): void {
  requestSearch(dir, false)
}

const searchStat = computed(() => {
  if (searchError.value) return ''
  if (!searchQuery.value.trim()) return ''
  if (searchInfo.count <= 0) return '0/0'
  return `${searchInfo.index + 1}/${searchInfo.count}`
})

watch(searchQuery, (q) => {
  if (!q.trim()) {
    queuedSearch = null
    search?.clearDecorations()
    searchInfo.index = -1
    searchInfo.count = 0
    searchError.value = ''
  }
})

// —— 终端初始化 ——
function setup(): void {
  disposeTerm()
  if (!container.value) return

  term = new Terminal({
    // 必须开启：SearchAddon 的 marker / decoration 属于 proposal API，
    // 关闭时 registerDecoration 会抛 "You must set the allowProposedApi option to true"
    allowProposedApi: true,
    theme: {
      background: '#0a0e1a',
      foreground: '#c9d1d9',
      cursor: '#38f2c8',
      cursorAccent: '#0a0e1a',
      selectionBackground: 'rgba(56,242,200,0.25)',
      black: '#0a0e1a',
      red: '#ff2d78',
      green: '#38f2c8',
      yellow: '#ffb02d',
      blue: '#5b8cff',
      magenta: '#ff2d78',
      cyan: '#38f2c8',
      white: '#c9d1d9',
      brightBlack: '#7d8ba3',
      brightWhite: '#eef3f7',
    },
    fontFamily: "'JetBrains Mono', 'Cascadia Code', Consolas, 'Courier New', monospace",
    fontSize: 12.5,
    lineHeight: 1.45,
    cursorBlink: true,
    cursorStyle: 'bar',
    disableStdin: true,
    scrollback: 10000,
    convertEol: true,
  })
  fit = new FitAddon()
  search = new SearchAddon()
  term.loadAddon(fit)
  term.loadAddon(search)
  term.open(container.value)
  fit.fit()

  searchSub = search.onDidChangeResults((e) => {
    searchInfo.index = e.resultIndex
    searchInfo.count = e.resultCount
  })

  container.value.addEventListener('contextmenu', onContextMenu)

  // Ctrl+Shift+C 或（有选中时）Ctrl+C → 复制
  term.attachCustomKeyEventHandler((e) => {
    if (e.ctrlKey && (e.key === 'C' || e.key === 'c')) {
      const sel = term?.getSelection()
      if (e.shiftKey || sel) {
        copySelection()
        return false
      }
    }
    return true
  })

  resizeObs = new ResizeObserver(() => {
    try {
      fit?.fit()
    } catch {
      /* ignore */
    }
  })
  resizeObs.observe(container.value)
}

function disposeTerm(): void {
  resizeObs?.disconnect()
  resizeObs = null
  container.value?.removeEventListener('contextmenu', onContextMenu)
  searchSub?.dispose()
  searchSub = null
  search?.dispose()
  term?.dispose()
  fit?.dispose()
  term = null
  fit = null
  search = null
  pendingWrites = 0
  queuedSearch = null
}

watch(
  () => store.selectedCommandId,
  (id) => {
    if (!id) {
      currentApp = ''
      renderedSeq = 0
      clearedSeq = 0
      resetCounts()
      term?.reset()
      return
    }
    if (id !== currentApp) switchApp(id)
  },
)

watch(
  () => store.logTick,
  () => render(),
)

// 级别规则变更（levelEpoch++）后按新规则整体重建当前视图
watch(
  () => store.levelEpoch,
  () => {
    if (currentApp) rebuild()
  },
)

onMounted(() => {
  setup()
  if (store.selectedCommandId) switchApp(store.selectedCommandId)
})

onBeforeUnmount(() => {
  disposeTerm()
})
</script>

<template>
  <div class="term-panel panel panel-accent">
    <div class="term-head">
      <div class="term-head-row">
        <span class="section-title">
          <Icon name="terminal" :size="12" /> OUTPUT&nbsp;/
          <span class="mono" style="color: var(--accent)">{{ store.selectedCommandId ? 'LOG' : 'CONSOLE' }}</span>
        </span>
        <span class="badge" :class="store.statuses[store.selectedCommandId]?.status === 'running' ? 'run' : ''">
          {{ store.statuses[store.selectedCommandId]?.status || 'idle' }}
        </span>
        <div class="grow"></div>
        <button class="btn btn-ghost btn-sm" title="复制（选中则复制选中，否则复制当前过滤结果）" @click="copySelection">
          <Icon name="copy" :size="12" />复制
        </button>
        <button class="btn btn-ghost btn-sm" title="清空当前视图（不影响继续输出的新日志）" @click="clearScreen">
          <Icon name="refresh" :size="12" />清屏
        </button>
      </div>

      <div class="term-toolbar">
        <div class="search-box" :class="{ bad: !!searchError }">
          <Icon name="search" :size="12" />
          <input
            v-model="searchQuery"
            class="search-input"
            placeholder="搜索当前视图…"
            spellcheck="false"
            @keyup.enter="doSearch('next')"
            @keyup.down="doSearch('next')"
            @keyup.up="doSearch('prev')"
          />
          <span v-if="searchStat" class="search-stat mono" :class="{ zero: searchInfo.count === 0 }">{{ searchStat }}</span>
          <button v-if="searchQuery" class="sbtn" title="清除搜索" @click="searchQuery = ''">
            <Icon name="kill" :size="10" />
          </button>
          <button class="sbtn" title="上一个 (Shift+Enter / ↑)" @click="doSearch('prev')">
            <Icon name="arrowUp" :size="11" />
          </button>
          <button class="sbtn" title="下一个 (Enter / ↓)" @click="doSearch('next')">
            <Icon name="arrowDown" :size="11" />
          </button>
        </div>

        <div class="filters" title="级别按行首 / level 字段的声明识别；未声明级别的行（含启动器消息、堆栈续行）归入 OTHER，ERROR/WARN 的堆栈续行会跟随显示">
          <Icon name="filter" :size="12" class="filter-ic" />
          <button class="chip chip-all" :class="{ active: allActive() }" @click="showAllLevels">ALL</button>
          <button
            v-for="lv in LEVELS"
            :key="lv.key"
            class="chip"
            :class="[lv.cls, { active: levelActive(lv.key), zero: counts[lv.key] === 0 }]"
            :aria-pressed="levelActive(lv.key)"
            @click="toggleLevel(lv.key)"
          >
            {{ lv.label }}<span class="cnt mono">{{ counts[lv.key] }}</span>
          </button>
        </div>

        <span class="shown mono" :class="{ filtering: !allActive() }">
          {{ allActive() ? `${totalCount} 行` : `${shownCount}/${totalCount} 行` }}
        </span>
        <span v-if="searchError" class="search-err mono">{{ searchError }}</span>
      </div>
    </div>

    <div ref="container" class="term-host"></div>
  </div>
</template>

<style scoped>
.term-panel {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  background: var(--bg-deep);
}
.term-head {
  display: flex;
  flex-direction: column;
  border-bottom: 1px solid var(--line-faint);
  flex-shrink: 0;
}
.term-head-row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
}
.grow {
  flex: 1;
}
.term-toolbar {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 6px 12px 8px;
  flex-wrap: wrap;
  border-top: 1px solid var(--line-faint);
}
.search-box {
  display: flex;
  align-items: center;
  gap: 6px;
  height: 26px;
  padding: 0 6px;
  background: var(--bg-deep);
  border: 1px solid var(--line-faint);
  border-radius: var(--radius-sm);
  color: var(--text-faint);
}
.search-box:focus-within {
  border-color: var(--accent);
  box-shadow: 0 0 0 1px rgba(56, 242, 200, 0.3);
}
.search-box.bad {
  border-color: var(--danger);
}
.search-input {
  width: 180px;
  height: 100%;
  background: transparent;
  border: none;
  outline: none;
  color: var(--text);
  font-family: var(--mono);
  font-size: 11.5px;
}
.search-input::placeholder {
  color: var(--text-faint);
}
.search-stat {
  font-size: 10px;
  color: var(--accent);
  min-width: 34px;
  text-align: right;
}
.search-stat.zero {
  color: var(--warn);
}
.sbtn {
  width: 20px;
  height: 20px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: transparent;
  border: none;
  border-radius: 3px;
  color: var(--text-faint);
  cursor: pointer;
}
.sbtn:hover {
  color: var(--accent);
  background: var(--surface-3);
}
.filters {
  display: flex;
  align-items: center;
  gap: 4px;
  flex-wrap: wrap;
}
.filter-ic {
  color: var(--text-faint);
  margin-right: 2px;
}
.chip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  height: 22px;
  padding: 0 8px;
  font-family: var(--mono);
  font-size: 10px;
  letter-spacing: 0.04em;
  color: var(--text-dim);
  background: var(--surface-2);
  border: 1px solid var(--line-faint);
  border-radius: 11px;
  cursor: pointer;
  transition: all 0.12s;
}
.chip:hover {
  color: var(--accent);
  border-color: var(--line);
}
.chip.zero:not(.active) {
  opacity: 0.45;
}
.chip .cnt {
  font-size: 9px;
  opacity: 0.75;
}
.chip.active {
  color: #04231b;
  background: var(--accent);
  border-color: var(--accent);
  font-weight: 700;
}
.chip.active .cnt {
  opacity: 0.9;
}
.chip.active.lv-debug {
  color: #0a0e1a;
  background: #7d8ba3;
  border-color: #7d8ba3;
}
.chip.active.lv-info {
  background: var(--accent);
  border-color: var(--accent);
}
.chip.active.lv-warn {
  color: #0a0e1a;
  background: var(--warn);
  border-color: var(--warn);
}
.chip.active.lv-error {
  color: #fff;
  background: var(--danger);
  border-color: var(--danger);
}
.chip.active.lv-other {
  color: #0a0e1a;
  background: var(--text-dim);
  border-color: var(--text-dim);
}
.shown {
  font-size: 10px;
  color: var(--text-faint);
  margin-left: auto;
  white-space: nowrap;
}
.shown.filtering {
  color: var(--accent);
}
.search-err {
  font-size: 10px;
  color: var(--danger);
}
.term-host {
  flex: 1;
  padding: 8px 4px 4px 12px;
  overflow: hidden;
}
.term-host :deep(.xterm) {
  height: 100%;
}
</style>
