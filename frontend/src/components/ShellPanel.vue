<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import Icon from './Icon.vue'
import { api, eventsOff, eventsOn } from '../api'
import { confirmDialog } from '../dialog'
import { copyToClipboard } from '../clipboard'
import {
  clearShellHistory,
  closeShellSession,
  fetchShellBuffer,
  openShellSession,
  restartShellSession,
  runShellLine,
  selectedCommand,
  selectedWorkspace,
  shellDirPath,
  store,
  switchShellDir,
  toggleShellFavorite,
} from '../store'
import type { ShellDirChoice } from '../store'
import type { ShellOutEvent } from '../types'

const container = ref<HTMLDivElement>()
let term: Terminal | null = null
let fit: FitAddon | null = null
let resizeObs: ResizeObserver | null = null

const cmd = () => selectedCommand()
const ws = () => selectedWorkspace()

const busy = ref(false)
const drawerOpen = ref(false)
/** 当前输入行（镜像给工具栏：收藏按钮可用性） */
const inputLine = ref('')

const session = computed(() => (store.selectedCommandId ? store.shellSessions[store.selectedCommandId] : undefined))
const dirChoice = computed<ShellDirChoice>(() => store.shellDirs[store.selectedCommandId] ?? 'module')
const dirPath = computed(() => {
  const c = cmd()
  return c ? shellDirPath(c, ws(), dirChoice.value) : ''
})
const historyDesc = computed(() => [...store.shellHistory].reverse())
const favDesc = computed(() => [...store.shellFavorites].reverse())
const favSet = computed(() => new Set(store.shellFavorites))

// —— 行编辑 / 历史导航 ——
const PROMPT = '\x1b[90m❯\x1b[0m '
/** 提示符防抖：输出静默该时长后绘制/重画待输入提示符 */
const PROMPT_DELAY = 150
/** 是否正在等待输入（提示符已显示在光标处） */
let promptVisible = false
let promptTimer: number | null = null
let histIdx = -1
let draft = ''

// —— 挂载期实时输出缓冲（与回放快照去重） ——
let attachSeq = 0
/** 正在挂载的 attach 序号（0 = 无）。旧挂载被新挂载取代时也必须收尾，
 *  否则卡在“挂起”会让所有 shell:out 事件永久滞留 pendingOut，表现为执行结果不显示 */
let attachingSeq = 0
const pendingOut: { commandId: string; data: string }[] = []

// —— 右键菜单 ——
const ctxMenu = ref<{ x: number; y: number } | null>(null)
const ctxHasSel = ref(false)
let ctxEl: HTMLElement | null = null

function isFav(line: string): boolean {
  return favSet.value.has(line.replace(/\s+$/, ''))
}

// —— 复制 / 粘贴 / 右键菜单 ——
function onContextMenu(e: MouseEvent): void {
  e.preventDefault()
  ctxHasSel.value = !!term?.getSelection()
  const x = Math.max(4, Math.min(e.clientX, window.innerWidth - 170))
  const y = Math.max(4, Math.min(e.clientY, window.innerHeight - 130))
  ctxMenu.value = { x, y }
}

function closeCtxMenu(refocus = true): void {
  if (ctxMenu.value === null) return
  ctxMenu.value = null
  if (refocus) term?.focus()
}

function ctxSelectAll(): void {
  term?.selectAll()
  closeCtxMenu()
}

function ctxCopy(): void {
  const sel = term?.getSelection()
  if (sel) void copyToClipboard(sel)
  closeCtxMenu()
}

function ctxPaste(): void {
  void doPaste()
}

/** 读取剪贴板并插入当前输入行（多行内容按既有约定逐行执行） */
async function doPaste(): Promise<void> {
  closeCtxMenu()
  let text = ''
  try {
    text = await navigator.clipboard.readText()
  } catch {
    return
  }
  insertPasted(text)
}

/** 粘贴内容进入终端：去掉尾部换行后单行只插入不执行；含内部换行则按行执行 */
function insertPasted(text: string): void {
  if (!session.value) return
  const trimmed = text.replace(/[\r\n]+$/, '')
  if (!trimmed) return
  term?.focus()
  if (trimmed.includes('\n') || trimmed.includes('\r')) {
    handleData(trimmed)
    return
  }
  feed(trimmed)
}

// —— 常驻提示符 ——
/** 输出静默 PROMPT_DELAY 后绘制待输入提示符（会话就绪 / 提交命令 / 收到输出后均会调度） */
function schedulePrompt(delay = PROMPT_DELAY): void {
  if (promptTimer !== null) window.clearTimeout(promptTimer)
  promptTimer = window.setTimeout(() => {
    promptTimer = null
    drawPrompt()
  }, delay)
}

function drawPrompt(): void {
  const t = term
  if (!t || !session.value || attachingSeq !== 0) return
  if (promptVisible || inputLine.value) return
  // 光标不在行首（上一段输出未带换行）：先换行，提示符始终顶格
  if (t.buffer.active.cursorX !== 0) t.write('\r\n')
  t.write(PROMPT)
  promptVisible = true
}

/** 字符终端显示宽度：CJK/全角/常见 emoji 占 2 列，其余 1 列 */
function charWidth(ch: string): number {
  const cp = ch.codePointAt(0) ?? 0
  if (
    (cp >= 0x1100 && cp <= 0x115f) || // Hangul Jamo
    (cp >= 0x2e80 && cp <= 0x303e) || // CJK 部首、标点
    (cp >= 0x3041 && cp <= 0x33ff) || // 假名、注音等
    (cp >= 0x3400 && cp <= 0x4dbf) || // CJK 扩展 A
    (cp >= 0x4e00 && cp <= 0x9fff) || // CJK 基本区
    (cp >= 0xa000 && cp <= 0xa4cf) || // 彝文
    (cp >= 0xa960 && cp <= 0xa97f) || // Hangul Jamo 扩展 A
    (cp >= 0xac00 && cp <= 0xd7a3) || // Hangul 音节
    (cp >= 0xf900 && cp <= 0xfaff) || // CJK 兼容表意
    (cp >= 0xfe10 && cp <= 0xfe19) || // 竖排形式
    (cp >= 0xfe30 && cp <= 0xfe6f) || // CJK 兼容形式
    (cp >= 0xff00 && cp <= 0xff60) || // 全角 ASCII/标点
    (cp >= 0xffe0 && cp <= 0xffe6) || // 全角符号
    (cp >= 0x1f300 && cp <= 0x1f64f) || // 常见 emoji
    (cp >= 0x1f680 && cp <= 0x1f6ff) ||
    (cp >= 0x1f900 && cp <= 0x1f9ff) ||
    (cp >= 0x1fa70 && cp <= 0x1faff) ||
    (cp >= 0x20000 && cp <= 0x2fffd) || // CJK 扩展 B-F
    (cp >= 0x30000 && cp <= 0x3fffd) // CJK 扩展 G+
  ) {
    return 2
  }
  return 1
}

// —— 终端初始化 ——
function setup(): void {
  disposeTerm()
  if (!container.value) return
  term = new Terminal({
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
    scrollback: 5000,
    convertEol: true,
  })
  fit = new FitAddon()
  term.loadAddon(fit)
  term.open(container.value)
  fit.fit()
  term.onData(handleData)
  term.attachCustomKeyEventHandler((e) => {
    if (e.type !== 'keydown') return true
    // Esc：关闭右键菜单
    if (e.key === 'Escape' && ctxMenu.value) {
      closeCtxMenu()
      return false
    }
    // Ctrl+C（含 Ctrl+Shift+C）：有选中文本 → 复制并清除选区，不作为 ^C 输入
    // （与 Windows Terminal 约定一致）；无选中 → 不拦截，交由输入处理清空当前行
    if (e.ctrlKey && !e.altKey && (e.key === 'c' || e.key === 'C')) {
      const sel = term?.getSelection()
      if (sel) {
        void copyToClipboard(sel)
        term?.clearSelection()
        return false
      }
    }
    // Ctrl+V / Ctrl+Shift+V / Shift+Insert：粘贴。
    // xterm 会把 Ctrl+V 拦截为插入控制符并取消浏览器默认粘贴，必须在此拦截并自行读剪贴板
    if (
      (e.ctrlKey && !e.altKey && (e.key === 'v' || e.key === 'V')) ||
      (e.shiftKey && e.key === 'Insert')
    ) {
      void doPaste()
      return false
    }
    return true
  })
  ctxEl = container.value.parentElement ?? container.value
  ctxEl.addEventListener('contextmenu', onContextMenu)
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
  ctxEl?.removeEventListener('contextmenu', onContextMenu)
  ctxEl = null
  ctxMenu.value = null
  term?.dispose()
  fit?.dispose()
  term = null
  fit = null
  if (promptTimer !== null) {
    window.clearTimeout(promptTimer)
    promptTimer = null
  }
  inputLine.value = ''
  promptVisible = false
  histIdx = -1
  draft = ''
}

// —— 输入处理 ——
function handleData(d: string): void {
  if (!session.value || !term) return
  // 粘贴多行：逐行执行
  if (d.includes('\n') || d.includes('\r')) {
    const parts = d.split(/\r\n|\r|\n/)
    for (let i = 0; i < parts.length; i++) {
      if (parts[i]) feed(parts[i])
      if (i < parts.length - 1) submit()
    }
    return
  }
  feed(d)
}

function feed(d: string): void {
  const t = term
  if (!t) return
  switch (d) {
    case '\x7f': {
      // 退格：中文等全角字符在终端占 2 列，需按显示宽度擦除；
      // 否则宽字符只擦掉一半而状态已删，表现为「前一个字删不掉」
      const chars = Array.from(inputLine.value)
      const removed = chars[chars.length - 1]
      if (!removed) return
      inputLine.value = chars.slice(0, -1).join('')
      const w = charWidth(removed)
      t.write('\b'.repeat(w) + ' '.repeat(w) + '\b'.repeat(w))
      return
    }
    case '\x03':
      // Ctrl+C（无选中时才会到这里，有选中已在 attachCustomKeyEventHandler 中复制）：
      // 清空当前输入行；真正的中断请用「重启」（管道模式无法透传信号）
      if (inputLine.value) t.write('\r\n')
      else if (promptVisible) t.write('\r\x1b[2K')
      t.write('\x1b[90m^C\x1b[0m\r\n')
      inputLine.value = ''
      promptVisible = false
      histIdx = -1
      schedulePrompt()
      return
    case '\u001b[A':
      navHistory(-1)
      return
    case '\u001b[B':
      navHistory(1)
      return
    case '\u001b[C':
    case '\u001b[D':
    case '\t':
      return
  }
  // 其余控制字符忽略（不可见，避免污染输入行）
  if (d.length === 1 && d.charCodeAt(0) < 0x20) return
  // 提示符常驻：防抖间隙内打字时立即补画
  if (!promptVisible && !inputLine.value) {
    t.write(PROMPT)
    promptVisible = true
  }
  inputLine.value += d
  t.write(d)
}

function navHistory(dir: -1 | 1): void {
  const hist = store.shellHistory // 时间序：旧 → 新
  const t = term
  if (!t || hist.length === 0) return
  if (histIdx === -1) {
    if (dir === 1) return
    draft = inputLine.value
    histIdx = hist.length - 1
  } else {
    histIdx += dir
    if (histIdx >= hist.length) {
      histIdx = -1
      setInput(draft)
      return
    }
    if (histIdx < 0) histIdx = 0
  }
  setInput(hist[histIdx])
}

/** 重画当前输入行（历史导航 / 抽屉回填） */
function setInput(text: string): void {
  const t = term
  if (!t) return
  inputLine.value = text
  t.write(`\r${PROMPT}\x1b[J${text}`)
  promptVisible = true
}

function submit(): void {
  const t = term
  if (!t) return
  const line = inputLine.value
  if (promptVisible) {
    // 清掉提示符行后重画命令回显，保证显示一致
    t.write('\r\x1b[2K')
    promptVisible = false
  }
  if (line) t.write(PROMPT + line + '\r\n')
  else t.write('\r\n')
  inputLine.value = ''
  histIdx = -1
  draft = ''
  const id = store.selectedCommandId
  if (id && session.value) void runShellLine(id, line)
  schedulePrompt()
}

// —— 抽屉操作 ——
function fillInput(text: string): void {
  if (!session.value) return
  setInput(text)
  term?.focus()
}

async function submitLine(text: string): Promise<void> {
  const t = term
  if (!t || !session.value) return
  if (promptVisible) {
    t.write('\r\x1b[2K')
    promptVisible = false
  }
  if (inputLine.value) t.write('\r\n')
  t.write(`${PROMPT}${text}\r\n`)
  inputLine.value = ''
  histIdx = -1
  const id = store.selectedCommandId
  if (id) await runShellLine(id, text)
  schedulePrompt()
}

async function starInput(): Promise<void> {
  const line = inputLine.value.replace(/\s+$/, '')
  if (!line) return
  await toggleShellFavorite(line, !isFav(line))
}

async function onClearHistory(): Promise<void> {
  if (store.shellHistory.length === 0) return
  const ok = await confirmDialog({
    title: '清空历史指令',
    message: `将清除当前工作区全部 ${store.shellHistory.length} 条历史指令记录（收藏不受影响）。`,
    confirmText: '清空',
  })
  if (ok) await clearShellHistory()
}

// —— 会话操作 ——
async function onOpen(): Promise<void> {
  const c = cmd()
  if (!c || busy.value) return
  busy.value = true
  await openShellSession(c.id, dirPath.value)
  busy.value = false
  term?.focus()
}

async function onClose(): Promise<void> {
  const c = cmd()
  if (!c || busy.value) return
  busy.value = true
  await closeShellSession(c.id)
  busy.value = false
}

async function onRestart(): Promise<void> {
  const c = cmd()
  if (!c || busy.value) return
  busy.value = true
  await restartShellSession(c.id)
  busy.value = false
  term?.focus()
}

async function onDirChange(e: Event): Promise<void> {
  const c = cmd()
  if (!c) return
  const choice = (e.target as HTMLSelectElement).value as ShellDirChoice
  store.shellDirs[c.id] = choice
  if (session.value) await switchShellDir(c.id, shellDirPath(c, ws(), choice))
}

// —— 挂载 / 回放 ——
async function attach(): Promise<void> {
  const seq = ++attachSeq
  const id = store.selectedCommandId
  const t = term
  if (!t) return
  t.reset()
  inputLine.value = ''
  promptVisible = false
  histIdx = -1
  draft = ''
  if (!id || !store.shellSessions[id]) return
  attachingSeq = seq
  try {
    const buf = await fetchShellBuffer(id)
    if (seq !== attachSeq || store.selectedCommandId !== id) return
    t.write(buf)
    // 回放期间到达的实时输出：仅补写当前指令的块，且与快照尾部比对去重
    let seen = buf
    for (const p of pendingOut.splice(0)) {
      if (p.commandId !== id || !p.data) continue
      if (seen.endsWith(p.data)) {
        seen = seen.slice(0, -p.data.length)
        continue
      }
      t.write(p.data)
    }
    schedulePrompt()
  } finally {
    // 只要“挂起”标记仍是自己的序号就清掉（含被新 attach 取代后返回的旧挂载），
    // 避免事件永久滞留 pendingOut
    if (attachingSeq === seq) attachingSeq = 0
  }
}

function onShellOut(ev: ShellOutEvent): void {
  if (ev.commandId !== store.selectedCommandId) return
  if (attachingSeq !== 0) {
    pendingOut.push({ commandId: ev.commandId, data: ev.data })
    return
  }
  writeIncoming(ev.data)
}

/** 会话输出写入终端：待输入提示符显示中则先擦除，输出结束后防抖重画 */
function writeIncoming(chunk: string): void {
  const t = term
  if (!t) return
  if (inputLine.value) {
    // 用户正在输入：不打断输入行
    t.write(chunk)
    return
  }
  if (promptVisible) {
    t.write('\r\x1b[2K')
    promptVisible = false
  }
  t.write(chunk)
  schedulePrompt()
}

watch(
  () => store.selectedCommandId,
  () => {
    void attach()
  },
)

// 会话开启 / 重启（session.id 变化）后重新挂载回放
watch(
  () => session.value?.id,
  (id) => {
    if (id) void attach()
  },
)

// 面板外入口（Git 管理按钮等）请求回显并执行指令
watch(
  () => store.pendingEcho,
  (pe) => {
    if (!pe) return
    const target = pe.commandId
    store.pendingEcho = null
    if (target !== store.selectedCommandId || !session.value) return
    void submitLine(pe.line)
  },
)

onMounted(() => {
  setup()
  eventsOn('shell:out', onShellOut)
  if (store.selectedCommandId && store.shellSessions[store.selectedCommandId]) {
    void attach()
  }
})

onBeforeUnmount(() => {
  eventsOff('shell:out')
  disposeTerm()
})
</script>

<template>
  <div class="shell-panel panel panel-accent">
    <div class="shell-head">
      <div class="shell-head-row">
        <span class="section-title">
          <Icon name="terminal" :size="12" /> SHELL
        </span>
        <span class="badge" :class="{ run: !!session }">{{ session ? session.shell : 'offline' }}</span>
        <span v-if="session" class="shell-cwd mono ellipsis" :title="session.dir">{{ session.dir }}</span>
        <div class="grow"></div>
        <select v-if="cmd()" class="dir-select mono" :value="dirChoice" title="会话工作目录" @change="onDirChange">
          <option value="module">模块目录</option>
          <option value="main">main 目录</option>
          <option value="root">工作区根目录</option>
        </select>
        <template v-if="session">
          <button
            class="btn btn-ghost btn-sm"
            title="中断并重开会话（管道模式 Ctrl+C 无法中断子进程）"
            :disabled="busy"
            @click="onRestart"
          >
            <Icon name="restart" :size="12" />重启
          </button>
          <button class="btn btn-ghost btn-sm" title="关闭会话并终止 Shell 进程树" :disabled="busy" @click="onClose">
            <Icon name="stop" :size="12" />关闭
          </button>
        </template>
        <button
          v-else
          class="btn btn-primary btn-sm"
          :disabled="!cmd() || busy"
          title="在所选目录打开交互 Shell"
          @click="onOpen"
        >
          <Icon name="play" :size="12" />打开会话
        </button>
        <button
          class="btn btn-ghost btn-sm"
          :disabled="!session || !inputLine.trim()"
          :title="isFav(inputLine) ? '取消收藏当前指令' : '收藏当前输入的指令'"
          @click="starInput"
        >
          <Icon name="star" :size="12" />{{ isFav(inputLine) ? '已收藏' : '收藏' }}
        </button>
        <button class="btn btn-ghost btn-sm" :class="{ on: drawerOpen }" title="收藏与历史列表" @click="drawerOpen = !drawerOpen">
          <Icon name="history" :size="12" />历史
        </button>
      </div>
    </div>

    <div class="shell-host-wrap">
      <div ref="container" class="shell-host"></div>

      <!-- 未选模块 -->
      <div v-if="!cmd()" class="shell-empty">
        <div class="glyph"><Icon name="terminal" :size="28" /></div>
        <p class="empty-desc">先在左侧选择一个启动模块</p>
      </div>
      <!-- 未开会话 -->
      <div v-else-if="!session" class="shell-empty">
        <div class="glyph"><Icon name="terminal" :size="28" /></div>
        <p class="empty-title">交互终端</p>
        <p class="empty-desc">在下方目录打开一个常驻 Shell 会话，可执行任意命令</p>
        <div class="empty-dir mono" :title="dirPath">{{ dirPath }}</div>
        <button class="btn btn-primary" :disabled="busy" @click="onOpen">
          <Icon name="play" :size="12" />打开会话
        </button>
      </div>

      <!-- 收藏 / 历史抽屉 -->
      <div v-if="drawerOpen" class="cmd-drawer">
        <div class="drawer-head">
          <span class="section-title">收藏 / 历史</span>
          <div class="grow"></div>
          <button class="sbtn" title="收起" @click="drawerOpen = false"><Icon name="kill" :size="10" /></button>
        </div>
        <div class="drawer-body">
          <section>
            <div class="drawer-cap">
              <Icon name="star" :size="11" /> 收藏
              <span class="mono cnt">{{ store.shellFavorites.length }}</span>
            </div>
            <div v-if="favDesc.length === 0" class="drawer-empty">
              暂无收藏：输入指令后点「收藏」，或点历史条目的星标
            </div>
            <div v-for="f in favDesc" :key="'f:' + f" class="drawer-item">
              <span class="di-cmd mono ellipsis" :title="f" @click="fillInput(f)">{{ f }}</span>
              <button class="di-act" title="立即执行" @click="submitLine(f)"><Icon name="play" :size="11" /></button>
              <button class="di-act faved" title="取消收藏" @click="toggleShellFavorite(f, false)">
                <Icon name="star" :size="11" />
              </button>
            </div>
          </section>
          <section>
            <div class="drawer-cap">
              <Icon name="history" :size="11" /> 历史
              <span class="mono cnt">{{ store.shellHistory.length }}</span>
              <span class="grow"></span>
              <button class="sbtn" title="清空历史" @click="onClearHistory"><Icon name="trash" :size="11" /></button>
            </div>
            <div v-if="historyDesc.length === 0" class="drawer-empty">暂无历史指令</div>
            <div v-for="h in historyDesc" :key="'h:' + h" class="drawer-item">
              <span class="di-cmd mono ellipsis" :title="h" @click="fillInput(h)">{{ h }}</span>
              <button class="di-act" title="立即执行" @click="submitLine(h)"><Icon name="play" :size="11" /></button>
              <button
                class="di-act"
                :class="{ faved: favSet.has(h) }"
                :title="favSet.has(h) ? '取消收藏' : '收藏'"
                @click="toggleShellFavorite(h, !favSet.has(h))"
              >
                <Icon name="star" :size="11" />
              </button>
            </div>
          </section>
        </div>
        <p class="drawer-hint">逐行执行（↑/↓ 翻历史；Ctrl+C 有选中复制、无选中清行；Ctrl+V 粘贴；右键菜单：全选 / 复制 / 粘贴）；全屏程序不可用，任务卡住请点「重启」。</p>
      </div>

      <!-- 右键菜单：全选 / 复制 / 粘贴 -->
      <template v-if="ctxMenu">
        <div class="ctx-mask" @pointerdown.stop.prevent="closeCtxMenu()" @contextmenu.prevent.stop="closeCtxMenu()"></div>
        <div class="ctx-menu" :style="{ left: ctxMenu.x + 'px', top: ctxMenu.y + 'px' }">
          <button class="ctx-item" :disabled="!session" @click="ctxSelectAll">
            <Icon name="list" :size="13" />全选
          </button>
          <button class="ctx-item" :disabled="!session || !ctxHasSel" @click="ctxCopy">
            <Icon name="copy" :size="13" />复制
          </button>
          <button class="ctx-item" :disabled="!session" @click="ctxPaste">
            <Icon name="clipboard" :size="13" />粘贴
          </button>
        </div>
      </template>
    </div>
  </div>
</template>

<style scoped>
.shell-panel {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  background: var(--bg-deep);
}
.shell-head {
  display: flex;
  flex-direction: column;
  border-bottom: 1px solid var(--line-faint);
  flex-shrink: 0;
}
.shell-head-row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
}
.grow {
  flex: 1;
}
.shell-cwd {
  font-size: 10.5px;
  color: var(--text-faint);
  max-width: 280px;
}
.btn.on {
  color: var(--accent);
  border-color: var(--line);
  background: var(--accent-soft);
}
.dir-select {
  height: 24px;
  padding: 0 4px;
  background: var(--bg-deep);
  border: 1px solid var(--line-faint);
  border-radius: var(--radius-sm);
  color: var(--text-dim);
  font-size: 10.5px;
  outline: none;
  cursor: pointer;
}
.dir-select:focus {
  border-color: var(--accent);
}
.shell-host-wrap {
  flex: 1;
  position: relative;
  padding: 8px 4px 4px 12px;
  overflow: hidden;
}
.shell-host {
  height: 100%;
}
.shell-host :deep(.xterm) {
  height: 100%;
}
.shell-empty {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 10px;
  background: var(--bg-deep);
  z-index: 5;
  padding: 20px;
}
.glyph {
  color: var(--accent);
  opacity: 0.75;
}
.empty-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--text);
  letter-spacing: 0.08em;
}
.empty-desc {
  font-size: 11px;
  color: var(--text-faint);
}
.empty-dir {
  max-width: 70%;
  font-size: 10.5px;
  color: var(--accent);
  background: var(--accent-soft);
  border: 1px solid var(--line-faint);
  border-radius: var(--radius-sm);
  padding: 5px 12px;
}

/* —— 收藏 / 历史抽屉 —— */
.cmd-drawer {
  position: absolute;
  top: 8px;
  right: 8px;
  bottom: 8px;
  width: 300px;
  display: flex;
  flex-direction: column;
  background: var(--bg-deep);
  border: 1px solid var(--line);
  border-radius: var(--radius-sm);
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.5);
  overflow: hidden;
  z-index: 10;
}
.drawer-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 10px;
  border-bottom: 1px solid var(--line-faint);
  flex-shrink: 0;
}
.drawer-body {
  flex: 1;
  overflow-y: auto;
  padding: 6px 10px;
}
.drawer-cap {
  display: flex;
  align-items: center;
  gap: 6px;
  margin: 6px 0;
  font-family: var(--mono);
  font-size: 10px;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: var(--text-faint);
}
.drawer-cap .cnt {
  font-size: 9px;
  letter-spacing: 0;
}
.drawer-empty {
  font-size: 10.5px;
  color: var(--text-faint);
  padding: 2px 2px 10px;
  line-height: 1.5;
}
.drawer-item {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 2px 4px;
  border-radius: 3px;
  margin-bottom: 2px;
}
.drawer-item:hover {
  background: var(--surface-2);
}
.di-cmd {
  flex: 1;
  min-width: 0;
  font-size: 11px;
  color: var(--text-dim);
  cursor: pointer;
  text-align: left;
}
.di-cmd:hover {
  color: var(--accent);
}
.di-act {
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
  flex-shrink: 0;
  transition: all 0.12s;
}
.di-act:hover {
  color: var(--accent);
  background: var(--surface-3);
}
.di-act.faved {
  color: var(--warn);
}
.di-act.faved :deep(svg) {
  fill: currentColor;
}
.drawer-hint {
  font-size: 10px;
  color: var(--text-faint);
  line-height: 1.5;
  padding: 8px 10px;
  border-top: 1px solid var(--line-faint);
  flex-shrink: 0;
  margin: 0;
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

/* —— 右键菜单 —— */
.ctx-mask {
  position: fixed;
  inset: 0;
  z-index: 90;
}
.ctx-menu {
  position: fixed;
  z-index: 95;
  min-width: 150px;
  padding: 4px;
  display: flex;
  flex-direction: column;
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: var(--radius-sm);
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.55);
}
.ctx-item {
  display: flex;
  align-items: center;
  gap: 8px;
  height: 28px;
  padding: 0 10px;
  border: none;
  background: transparent;
  border-radius: 3px;
  color: var(--text-dim);
  font-size: 12px;
  cursor: pointer;
  text-align: left;
}
.ctx-item:hover:not(:disabled) {
  background: var(--surface-3);
  color: var(--accent);
}
.ctx-item:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}
</style>
