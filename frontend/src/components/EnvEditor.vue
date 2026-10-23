<script setup lang="ts">
import { ref, watch } from 'vue'
import Icon from './Icon.vue'
import type { EnvMap } from '../types'

const props = defineProps<{ modelValue: EnvMap; placeholder?: string }>()
const emit = defineEmits<{ (e: 'update:modelValue', v: EnvMap): void }>()

interface Row {
  key: string
  value: string
}

const rows = ref<Row[]>([])

watch(
  () => props.modelValue,
  (m) => {
    rows.value = Object.entries(m || {}).map(([key, value]) => ({ key, value }))
  },
  { immediate: true },
)

function sync(): void {
  const out: EnvMap = {}
  for (const r of rows.value) {
    const k = r.key.trim()
    if (k) out[k] = r.value
  }
  emit('update:modelValue', out)
}

function addRow(): void {
  rows.value.push({ key: '', value: '' })
}

function removeRow(i: number): void {
  rows.value.splice(i, 1)
  sync()
}

function onEdit(): void {
  sync()
}
</script>

<template>
  <div class="env-editor">
    <div v-for="(row, i) in rows" :key="i" class="kv-row">
      <input v-model="row.key" class="input key" :placeholder="`变量名`" spellcheck="false" @input="onEdit" />
      <input v-model="row.value" class="input" :placeholder="placeholder || '变量值'" spellcheck="false" @input="onEdit" />
      <button class="mini danger" title="删除" @click="removeRow(i)"><Icon name="kill" :size="11" /></button>
    </div>
    <button class="btn btn-ghost btn-sm add" @click="addRow"><Icon name="plus" :size="12" />添加变量</button>
  </div>
</template>

<style scoped>
.env-editor {
  display: flex;
  flex-direction: column;
}
.kv-row .key {
  color: var(--accent);
}
.kv-row .mini {
  width: 28px;
  height: 28px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: transparent;
  border: 1px solid var(--line-faint);
  border-radius: var(--radius-sm);
  color: var(--text-faint);
  cursor: pointer;
  transition: all 0.12s;
}
.kv-row .mini:hover {
  color: var(--danger);
  border-color: var(--danger);
}
.add {
  align-self: flex-start;
  margin-top: 8px;
}
</style>
