<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import Icon from './Icon.vue'
import { addCommand, selectedWorkspace } from '../store'
import type { MainEntry } from '../types'

const props = defineProps<{ workspaceId: string }>()
const emit = defineEmits<{ (e: 'close'): void }>()

const ws = () => selectedWorkspace()

// 每个入口一个可编辑的默认名（预填模块名）
const names = ref<Record<string, string>>({})
const busy = ref(false)
const query = ref('')

watch(
  () => props.workspaceId,
  () => {
    names.value = {}
    const w = ws()
    for (const e of w?.mainEntries || []) {
      names.value[e.id] = e.name
    }
    query.value = ''
  },
  { immediate: true },
)

const filteredEntries = computed<MainEntry[]>(() => {
  const q = query.value.trim().toLowerCase()
  const all = ws()?.mainEntries || []
  if (!q) return all
  return all.filter(
    (e) =>
      e.name.toLowerCase().includes(q) ||
      e.mainFile.toLowerCase().includes(q) ||
      e.modulePath.toLowerCase().includes(q) ||
      e.mainDir.toLowerCase().includes(q),
  )
})

function commandCount(entryId: string): number {
  return ws()?.commands.filter((c) => c.mainEntryId === entryId).length ?? 0
}

async function onAdd(entry: MainEntry): Promise<void> {
  if (busy.value) return
  busy.value = true
  const ok = await addCommand(props.workspaceId, entry.id, names.value[entry.id] || '')
  busy.value = false
  if (!ok) return
}
</script>

<template>
  <div class="overlay" @click.self="emit('close')">
    <div class="dialog panel panel-accent">
      <div class="d-head">
        <span class="section-title">添加启动指令</span>
        <button class="btn btn-ghost btn-icon" @click="emit('close')"><Icon name="kill" :size="13" /></button>
      </div>

      <div class="search-row">
        <div class="search-box">
          <Icon name="search" :size="12" />
          <input
            v-model="query"
            class="search-input"
            placeholder="搜索入口名 / main 文件 / module 路径…"
            spellcheck="false"
          />
          <button v-if="query" class="sbtn" title="清空" @click="query = ''"><Icon name="kill" :size="11" /></button>
        </div>
        <span class="count mono">{{ filteredEntries.length }} / {{ ws()?.mainEntries.length ?? 0 }}</span>
      </div>

      <div class="entries">
        <div v-for="e in filteredEntries" :key="e.id" class="entry">
          <div class="e-info">
            <div class="e-name mono ellipsis">{{ e.name }}</div>
            <div class="e-path mono ellipsis">{{ e.mainFile }}</div>
            <div v-if="commandCount(e.id) > 0" class="badge">{{ commandCount(e.id) }} 条指令</div>
          </div>
          <div class="e-actions">
            <input
              v-model="names[e.id]"
              class="input name-input"
              placeholder="指令名（默认模块名）"
              spellcheck="false"
            />
            <button class="btn btn-primary btn-sm" :disabled="busy" @click="onAdd(e)">
              <Icon name="plus" :size="12" />添加
            </button>
          </div>
        </div>

        <div v-if="(ws()?.mainEntries.length ?? 0) === 0" class="empty" style="padding: 30px">
          <span class="mono" style="font-size: 11px">暂无 main 入口 · 请先在侧边栏点击「扫描」</span>
        </div>
        <div v-else-if="filteredEntries.length === 0" class="empty" style="padding: 30px">
          <span class="mono" style="font-size: 11px">无匹配条目</span>
        </div>
      </div>

      <div class="d-foot">
        <button class="btn" @click="emit('close')">完成</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.overlay {
  position: fixed;
  inset: 0;
  background: rgba(4, 6, 12, 0.66);
  backdrop-filter: blur(4px);
  z-index: 100;
  display: flex;
  align-items: center;
  justify-content: center;
}
.dialog {
  width: 620px;
  max-height: 80vh;
  display: flex;
  flex-direction: column;
  padding: 20px 22px;
  background: var(--surface);
  box-shadow: 0 20px 60px rgba(0, 0, 0, 0.5), var(--glow);
}
.d-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 12px;
}
.search-row {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 12px;
}
.search-box {
  flex: 1;
  display: flex;
  align-items: center;
  gap: 6px;
  height: 30px;
  padding: 0 8px;
  background: var(--bg-deep);
  border: 1px solid var(--line-faint);
  border-radius: var(--radius-sm);
  color: var(--text-faint);
}
.search-box:focus-within {
  border-color: var(--accent);
  box-shadow: 0 0 0 1px rgba(56, 242, 200, 0.3);
}
.search-input {
  flex: 1;
  height: 100%;
  background: transparent;
  border: none;
  outline: none;
  color: var(--text);
  font-family: var(--mono);
  font-size: 12px;
}
.search-input::placeholder {
  color: var(--text-faint);
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
  color: var(--danger);
  background: var(--surface-3);
}
.count {
  font-size: 10.5px;
  color: var(--text-faint);
  flex-shrink: 0;
}
.entries {
  flex: 1;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.entry {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 12px;
  background: var(--surface-2);
  border: 1px solid var(--line-faint);
  border-radius: var(--radius-sm);
}
.e-info {
  flex: 1;
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 8px;
}
.e-name {
  font-size: 12.5px;
  font-weight: 600;
  color: #e6edf3;
  flex-shrink: 0;
}
.e-path {
  font-size: 10.5px;
  color: var(--text-faint);
  flex: 1;
  min-width: 0;
}
.e-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}
.name-input {
  width: 160px;
  font-size: 11px;
}
.d-foot {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 16px;
}
</style>
