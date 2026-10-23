<script setup lang="ts">
import { ref } from 'vue'
import Icon from './Icon.vue'
import { chooseDirectory, createWorkspace } from '../store'

const emit = defineEmits<{ (e: 'close'): void }>()

const name = ref('')
const rootPath = ref('')
const busy = ref(false)
const error = ref('')

async function pick(): Promise<void> {
  const p = await chooseDirectory()
  if (p) {
    rootPath.value = p
    // 默认名：目录最后一段
    if (!name.value) {
      const seg = p.replace(/[\\/]+$/, '').split(/[\\/]/).pop()
      name.value = seg || 'workspace'
    }
  }
}

async function submit(): Promise<void> {
  if (!name.value.trim() || !rootPath.value.trim()) {
    error.value = '请填写名称并选择目录'
    return
  }
  busy.value = true
  error.value = ''
  const ok = await createWorkspace(name.value.trim(), rootPath.value.trim())
  busy.value = false
  if (ok) emit('close')
  else error.value = '创建失败，请检查目录是否包含 Go 项目'
}
</script>

<template>
  <div class="overlay" @click.self="emit('close')">
    <div class="dialog panel panel-accent">
      <div class="dialog-head">
        <span class="section-title">NEW WORKSPACE</span>
        <button class="btn btn-ghost btn-icon" @click="emit('close')"><Icon name="kill" :size="13" /></button>
      </div>

      <label class="field">
        <span class="label">名称</span>
        <input v-model="name" class="input" placeholder="例如：core-services" @keyup.enter="submit" />
      </label>

      <label class="field">
        <span class="label">根目录（自动识别 go.mod / main）</span>
        <div class="path-pick">
          <input v-model="rootPath" class="input grow" placeholder="选择包含 Go 项目的目录" readonly @click="pick" />
          <button class="btn" @click="pick"><Icon name="folder" :size="13" />浏览</button>
        </div>
      </label>

      <div v-if="error" class="err mono">{{ error }}</div>

      <div class="dialog-foot">
        <button class="btn" @click="emit('close')">取消</button>
        <button class="btn btn-primary" :disabled="busy" @click="submit">
          <Icon name="zap" :size="13" />{{ busy ? '识别中…' : '创建并扫描' }}
        </button>
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
  width: 520px;
  padding: 20px 22px;
  background: var(--surface);
  box-shadow: 0 20px 60px rgba(0, 0, 0, 0.5), var(--glow);
}
.dialog-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 18px;
}
.field {
  display: block;
  margin-bottom: 14px;
}
.label {
  display: block;
  font-size: 11px;
  color: var(--text-dim);
  margin-bottom: 6px;
  letter-spacing: 0.04em;
}
.path-pick {
  display: flex;
  gap: 8px;
}
.path-pick .input {
  cursor: pointer;
}
.err {
  color: var(--danger);
  font-size: 11.5px;
  margin-bottom: 10px;
}
.dialog-foot {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 20px;
}
</style>
