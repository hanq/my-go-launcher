<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import Icon from './Icon.vue'
import EnvEditor from './EnvEditor.vue'
import { api } from '../api'
import { compileLevelPattern, resolveLevel } from '../log_level'
import { confirmDialog } from '../dialog'
import {
  deleteCommand,
  openShellSession,
  revealPath,
  runShellLine,
  selectedCommand,
  selectedWorkspace,
  store,
  switchShellDir,
  updateCommand,
  updateWorkspaceEnv,
} from '../store'
import type { EnvMap } from '../types'

const cmd = () => selectedCommand()
const ws = () => selectedWorkspace()

const name = ref('')
const cmdEnv = ref<EnvMap>({})
const wsEnv = ref<EnvMap>({})
const args = ref('')
const saved = ref(false)
const cmdPattern = ref('')
const wsPattern = ref('')
const sampleLine = ref('')
const sampleLevel = ref('')

// —— 层叠分组（与左侧一致的可折叠组；展开状态记忆） ——
const GROUP_DEFAULTS: Record<string, boolean> = {
  basic: true,
  paths: false,
  loglevel: true,
  env: false,
  inject: false,
  git: true,
}
const GROUP_META: { key: string; title: string; icon: string }[] = [
  { key: 'basic', title: '基础配置', icon: 'gear' },
  { key: 'paths', title: '路径信息', icon: 'cube' },
  { key: 'loglevel', title: '日志规则', icon: 'filter' },
  { key: 'env', title: '环境变量', icon: 'variable' },
  { key: 'inject', title: '注入预览', icon: 'zap' },
  { key: 'git', title: 'Git 管理', icon: 'gitBranch' },
]
const groups = reactive<Record<string, boolean>>({ ...GROUP_DEFAULTS, ...loadGroups() })

function loadGroups(): Record<string, boolean> {
  try {
    const raw = localStorage.getItem('gl.detail.groups')
    if (raw) {
      const parsed = JSON.parse(raw) as Record<string, boolean>
      if (parsed && typeof parsed === 'object') return parsed
    }
  } catch {
    /* ignore */
  }
  return {}
}

function toggleGroup(key: string): void {
  groups[key] = !groups[key]
  try {
    localStorage.setItem('gl.detail.groups', JSON.stringify({ ...groups }))
  } catch {
    /* ignore */
  }
}

// —— Git 管理 ——
const gitRoot = ref('')
const gitBusy = ref(false)
const SUBMODULE_INIT = 'git submodule update --init'
const SUBMODULE_SYNC =
  "git submodule foreach -q --recursive 'git checkout $(git config -f $toplevel/.gitmodules submodule.$name.branch || echo master)'"
const branchName = computed(() => (cmd() ? store.gitBranches[cmd()!.id] || '' : ''))

async function loadGitRoot(): Promise<void> {
  const c = cmd()
  if (!c) {
    gitRoot.value = ''
    return
  }
  try {
    gitRoot.value = (await api.GetGitRoot(c.moduleDir)) ?? ''
  } catch {
    gitRoot.value = ''
  }
}

/** 在 git 根目录执行指令：无会话则以 git 根开启；目录不同先 cd；过程与结果在交互终端查看 */
async function execGit(gitCmd: string): Promise<void> {
  const c = cmd()
  if (!c || gitBusy.value) return
  gitBusy.value = true
  try {
    const root = gitRoot.value || (await api.GetGitRoot(c.moduleDir)) || ''
    if (!root) {
      store.error = `「${c.name}」不在 git 仓库中，无法执行子模块操作`
      return
    }
    if (!store.shellSessions[c.id]) {
      if (!(await openShellSession(c.id, root))) return
    } else if (store.shellSessions[c.id].dir !== root) {
      await switchShellDir(c.id, root)
    }
    store.centerTab = 'shell'
    await runShellLine(c.id, gitCmd)
  } finally {
    gitBusy.value = false
  }
}

const injectionPreview = computed(() => {
  const c = cmd()
  const w = ws()
  if (!c) return []
  const rows: { k: string; v: string; builtin: boolean }[] = [
    { k: 'GOLAUNCHER_WORKSPACE', v: w?.name ?? '', builtin: true },
    { k: 'GOLAUNCHER_WORKSPACE_ROOT', v: w?.rootPath ?? '', builtin: true },
    { k: 'GOLAUNCHER_APP_NAME', v: c.name, builtin: true },
    { k: 'GOLAUNCHER_APP_DIR', v: c.mainDir, builtin: true },
    { k: 'GOLAUNCHER_MODULE_DIR', v: c.moduleDir, builtin: true },
    { k: 'GOLAUNCHER_MODULE_PATH', v: c.modulePath, builtin: true },
  ]
  for (const [k, v] of Object.entries(wsEnv.value)) rows.push({ k, v, builtin: false })
  for (const [k, v] of Object.entries(cmdEnv.value)) {
    const i = rows.findIndex((r) => r.k === k && !r.builtin)
    if (i >= 0) rows[i].v = v
    else rows.push({ k, v, builtin: false })
  }
  return rows
})

const patternError = computed(() => {
  const checks: [string, string][] = [
    ['指令级', cmdPattern.value],
    ['工作区级', wsPattern.value],
  ]
  for (const [label, raw] of checks) {
    const { error } = compileLevelPattern(raw)
    if (error) return `${label}正则无效：${error}`
  }
  return ''
})

watch(
  () => cmd()?.id,
  () => {
    const c = cmd()
    const w = ws()
    name.value = c?.name ?? ''
    cmdEnv.value = { ...(c?.envVars || {}) }
    wsEnv.value = { ...(w?.envVars || {}) }
    args.value = (c?.runArgs || []).join(' ')
    cmdPattern.value = c?.logLevelPattern ?? ''
    wsPattern.value = w?.logLevelPattern ?? ''
    saved.value = false
    void loadGitRoot()
  },
  { immediate: true },
)

function runSample(): void {
  if (!sampleLine.value.trim()) {
    sampleLevel.value = ''
    return
  }
  // 与运行时同一条解析链：自定义规则优先，未设置/未命中回退内置识别
  const custom = compileLevelPattern(cmdPattern.value.trim() || wsPattern.value.trim()).re
  sampleLevel.value = resolveLevel(sampleLine.value, null, custom).level.toUpperCase()
}

async function save(): Promise<void> {
  const c = cmd()
  const w = ws()
  if (!c) return
  const argArr = args.value
    .split(/\s+/)
    .map((s) => s.trim())
    .filter(Boolean)
  await updateCommand(c, {
    name: name.value.trim() || c.name,
    envVars: cmdEnv.value,
    runArgs: argArr,
    logLevelPattern: cmdPattern.value.trim(),
  })
  if (w) await updateWorkspaceEnv(w, wsEnv.value, wsPattern.value.trim())
  saved.value = true
  setTimeout(() => (saved.value = false), 2000)
}

async function remove(): Promise<void> {
  const c = cmd()
  if (!c) return
  const ok = await confirmDialog({
    title: `删除指令 · ${c.name}`,
    message: '删除后该指令的启动配置（环境变量、参数）将被移除；运行中的进程会先被强制停止。',
    danger: true,
    confirmText: '删除',
  })
  if (!ok) return
  await deleteCommand(c.workspaceId, c.id)
}
</script>

<template>
  <aside class="detail panel">
    <template v-if="cmd()">
      <div class="d-head">
        <span class="section-title">COMMAND CONFIG</span>
        <div class="flex gap6">
          <button class="btn btn-danger btn-sm" title="删除指令" @click="remove">
            <Icon name="trash" :size="12" />
          </button>
          <button class="btn btn-primary btn-sm" @click="save">
            <Icon name="zap" :size="12" />{{ saved ? '已保存' : '保存' }}
          </button>
        </div>
      </div>

      <div class="d-body">
        <!-- 基础配置 -->
        <section class="group">
          <button class="group-head" @click="toggleGroup('basic')">
            <span class="chev" :class="{ open: groups.basic }"><Icon name="chevron" :size="12" /></span>
            <Icon name="gear" :size="12" class="g-ic" />
            <span class="g-title">基础配置</span>
          </button>
          <div v-show="groups.basic" class="group-body">
            <h4 class="d-sec"><Icon name="terminal" :size="12" />外显名称</h4>
            <input v-model="name" class="input full" placeholder="默认使用模块名" spellcheck="false" />
            <h4 class="d-sec" style="margin-top: 14px"><Icon name="terminal" :size="12" />启动参数（空格分隔）</h4>
            <input v-model="args" class="input full" placeholder="例如：--port 8080 --debug" spellcheck="false" />
          </div>
        </section>

        <!-- 路径信息 -->
        <section class="group">
          <button class="group-head" @click="toggleGroup('paths')">
            <span class="chev" :class="{ open: groups.paths }"><Icon name="chevron" :size="12" /></span>
            <Icon name="cube" :size="12" class="g-ic" />
            <span class="g-title">路径信息</span>
          </button>
          <div v-show="groups.paths" class="group-body">
            <div class="path-item">
              <span class="lbl">module</span>
              <span class="path mono" @click="revealPath(cmd()!.moduleDir)">{{ cmd()!.modulePath }}</span>
            </div>
            <div class="path-item">
              <span class="lbl">main dir</span>
              <span class="path mono" @click="revealPath(cmd()!.mainDir)">{{ cmd()!.mainDir }}</span>
            </div>
            <div class="path-item">
              <span class="lbl">main file</span>
              <span class="path mono" @click="revealPath(cmd()!.mainDir)">{{ cmd()!.mainFile }}</span>
            </div>
          </div>
        </section>

        <!-- 日志规则 -->
        <section class="group">
          <button class="group-head" @click="toggleGroup('loglevel')">
            <span class="chev" :class="{ open: groups.loglevel }"><Icon name="chevron" :size="12" /></span>
            <Icon name="filter" :size="12" class="g-ic" />
            <span class="g-title">日志规则</span>
          </button>
          <div v-show="groups.loglevel" class="group-body">
            <p class="hint">
              正则捕获组 1（或命名组 level）作为级别词，自动映射 INF/ERR/WRN/DBG/FTL/TRC 等写法；未命中时回退内置识别。留空使用内置识别。
            </p>
            <div class="pat-row">
              <span class="lbl">指令级</span>
              <input
                v-model="cmdPattern"
                class="input full"
                :class="{ bad: !!patternError }"
                placeholder="例：^\S+\s+\S+\s+(INF|WRN|ERR|DBG|FTL)\s"
                spellcheck="false"
              />
            </div>
            <div class="pat-row">
              <span class="lbl">工作区级</span>
              <input
                v-model="wsPattern"
                class="input full"
                placeholder="指令未设置时生效，格式同上"
                spellcheck="false"
              />
            </div>
            <p v-if="patternError" class="pat-warn">{{ patternError }}</p>
            <div class="pat-row">
              <span class="lbl">试一试</span>
              <input
                v-model="sampleLine"
                class="input full"
                placeholder="粘贴一行日志后回车，预览识别结果"
                spellcheck="false"
                @keyup.enter="runSample"
              />
            </div>
            <p v-if="sampleLevel" class="pat-sample">
              识别级别：<span class="lv mono">{{ sampleLevel }}</span>
            </p>
          </div>
        </section>

        <!-- 环境变量 -->
        <section class="group">
          <button class="group-head" @click="toggleGroup('env')">
            <span class="chev" :class="{ open: groups.env }"><Icon name="chevron" :size="12" /></span>
            <Icon name="variable" :size="12" class="g-ic" />
            <span class="g-title">环境变量</span>
          </button>
          <div v-show="groups.env" class="group-body">
            <h4 class="d-sec"><Icon name="variable" :size="12" />指令级（覆盖工作区）</h4>
            <EnvEditor v-model="cmdEnv" placeholder="值（覆盖工作区）" />
            <h4 class="d-sec" style="margin-top: 14px"><Icon name="variable" :size="12" />工作区级（全部指令生效）</h4>
            <EnvEditor v-model="wsEnv" />
          </div>
        </section>

        <!-- 注入预览 -->
        <section class="group">
          <button class="group-head" @click="toggleGroup('inject')">
            <span class="chev" :class="{ open: groups.inject }"><Icon name="chevron" :size="12" /></span>
            <Icon name="zap" :size="12" class="g-ic" />
            <span class="g-title">注入预览</span>
          </button>
          <div v-show="groups.inject" class="group-body">
            <div class="inj">
              <div v-for="r in injectionPreview" :key="r.k" class="inj-row" :class="{ builtin: r.builtin }">
                <span class="k mono">{{ r.k }}</span>
                <span class="v mono ellipsis">{{ r.v }}</span>
              </div>
            </div>
          </div>
        </section>

        <!-- Git 管理 -->
        <section class="group">
          <button class="group-head" @click="toggleGroup('git')">
            <span class="chev" :class="{ open: groups.git }"><Icon name="chevron" :size="12" /></span>
            <Icon name="gitBranch" :size="12" class="g-ic" />
            <span class="g-title">Git 管理</span>
          </button>
          <div v-show="groups.git" class="group-body">
            <div class="git-row">
              <span class="lbl">分支</span>
              <span v-if="branchName" class="git-branch mono">
                <Icon name="gitBranch" :size="11" />{{ branchName }}
              </span>
              <span v-else class="git-none">非 git 仓库</span>
            </div>
            <div class="git-row">
              <span class="lbl">仓库根</span>
              <span v-if="gitRoot" class="git-root mono ellipsis" :title="gitRoot" @click="revealPath(gitRoot)">
                {{ gitRoot }}
              </span>
              <span v-else class="git-none">—</span>
            </div>
            <div class="git-actions">
              <button
                class="btn btn-sm git-btn"
                :disabled="gitBusy"
                :title="SUBMODULE_INIT"
                @click="execGit(SUBMODULE_INIT)"
              >
                <Icon name="download" :size="12" />同步初始化子模块
              </button>
              <button
                class="btn btn-sm git-btn"
                :disabled="gitBusy"
                :title="SUBMODULE_SYNC"
                @click="execGit(SUBMODULE_SYNC)"
              >
                <Icon name="refresh" :size="12" />同步子模块
              </button>
            </div>
            <p class="git-hint">在当前微服务的 git 根目录执行，执行过程与结果在「交互终端」中查看。</p>
          </div>
        </section>
      </div>
    </template>
    <div v-else class="empty">
      <div class="glyph"><Icon name="gear" :size="30" /></div>
      <span>无选中指令</span>
    </div>
  </aside>
</template>

<style scoped>
.detail {
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  border-left: 1px solid var(--line-faint);
  border-top: none;
  border-bottom: none;
  border-radius: 0;
}
.d-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 12px;
  border-bottom: 1px solid var(--line-faint);
}
.d-body {
  flex: 1;
  overflow-y: auto;
  padding: 10px 10px 12px;
}

/* —— 层叠分组 —— */
.group {
  border: 1px solid var(--line-faint);
  border-radius: var(--radius-sm);
  margin-bottom: 8px;
  overflow: hidden;
}
.group-head {
  display: flex;
  align-items: center;
  gap: 6px;
  width: 100%;
  padding: 7px 9px;
  background: var(--surface-2);
  border: none;
  color: var(--text-dim);
  cursor: pointer;
  transition: background 0.12s;
}
.group-head:hover {
  background: var(--surface-3);
}
.chev {
  color: var(--text-faint);
  transition: transform 0.15s;
  display: inline-flex;
}
.chev.open {
  transform: rotate(90deg);
}
.g-ic {
  color: var(--accent);
  flex-shrink: 0;
}
.g-title {
  font-family: var(--mono);
  font-size: 10.5px;
  letter-spacing: 0.1em;
  text-transform: uppercase;
}
.group-body {
  padding: 10px 9px;
}

.d-sec {
  display: flex;
  align-items: center;
  gap: 6px;
  font-family: var(--mono);
  font-size: 10.5px;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: var(--text-faint);
  margin-bottom: 10px;
}
.d-sec :deep(svg) {
  color: var(--accent);
}
.path-item {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  margin-bottom: 6px;
}
.lbl {
  width: 58px;
  flex-shrink: 0;
  font-family: var(--mono);
  font-size: 9.5px;
  color: var(--text-faint);
  letter-spacing: 0.06em;
  text-transform: uppercase;
  padding-top: 2px;
}
.hint {
  font-size: 10.5px;
  color: var(--text-faint);
  margin-bottom: 8px;
  line-height: 1.5;
}
.full {
  width: 100%;
}
.inj {
  display: flex;
  flex-direction: column;
  gap: 3px;
}
.inj-row {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 10.5px;
  padding: 3px 6px;
  border-radius: 3px;
}
.inj-row.builtin {
  background: var(--accent-soft);
}
.inj-row .k {
  color: var(--accent);
  flex-shrink: 0;
}
.inj-row .v {
  color: var(--text-dim);
  text-align: right;
}
.pat-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 6px;
}
.pat-row .input {
  flex: 1;
  min-width: 0;
}
.pat-warn {
  font-size: 10.5px;
  color: var(--danger);
  margin: -2px 0 6px;
  line-height: 1.5;
}
.pat-sample {
  font-size: 10.5px;
  color: var(--text-dim);
}
.pat-sample .lv {
  color: var(--accent);
  font-weight: 700;
  letter-spacing: 0.08em;
}
.input.bad {
  border-color: var(--danger);
}

/* —— Git 管理 —— */
.git-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 6px;
}
.git-branch {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  color: var(--accent);
  font-size: 11px;
}
.git-root {
  flex: 1;
  min-width: 0;
  font-size: 10.5px;
  color: var(--text-dim);
  cursor: pointer;
}
.git-root:hover {
  color: var(--accent);
}
.git-none {
  font-size: 10.5px;
  color: var(--text-faint);
}
.git-actions {
  display: flex;
  gap: 6px;
  margin-top: 6px;
}
.git-btn {
  flex: 1;
  justify-content: center;
}
.git-hint {
  font-size: 10px;
  color: var(--text-faint);
  margin: 8px 0 0;
  line-height: 1.5;
}
</style>
