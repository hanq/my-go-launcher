<script setup lang="ts">
import { ref, watch } from 'vue'
import Icon from './Icon.vue'
import EnvEditor from './EnvEditor.vue'
import { updateWorkspaceEnv } from '../store'
import type { EnvMap, Workspace } from '../types'

const props = defineProps<{ ws: Workspace }>()
const emit = defineEmits<{ (e: 'close'): void }>()

const env = ref<EnvMap>({})
const busy = ref(false)

watch(
  () => props.ws,
  (w) => {
    env.value = { ...(w?.envVars || {}) }
  },
  { immediate: true },
)

async function save(): Promise<void> {
  busy.value = true
  const ok = await updateWorkspaceEnv(props.ws, env.value)
  busy.value = false
  if (ok) emit('close')
}
</script>

<template>
  <div class="overlay" @click.self="emit('close')">
    <div class="dialog panel panel-accent">
      <div class="d-head">
        <span class="section-title">WORKSPACE ENV · {{ ws.name }}</span>
        <button class="btn btn-ghost btn-icon" @click="emit('close')"><Icon name="kill" :size="13" /></button>
      </div>
      <p class="hint">工作区级系统变量，启动时作为会话变量注入所有子进程（可被应用级覆盖）。</p>
      <EnvEditor v-model="env" />
      <div class="d-foot">
        <button class="btn" @click="emit('close')">取消</button>
        <button class="btn btn-primary" :disabled="busy" @click="save">
          <Icon name="zap" :size="13" />保存
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
.d-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 10px;
}
.hint {
  font-size: 11.5px;
  color: var(--text-dim);
  line-height: 1.6;
  margin-bottom: 14px;
}
.d-foot {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 18px;
}
</style>
