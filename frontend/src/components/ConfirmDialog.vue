<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import Icon from './Icon.vue'
import { dialogState, settleDialog } from '../dialog'

const okBtn = ref<HTMLButtonElement>()

watch(
  () => dialogState.visible,
  async (v) => {
    if (v) {
      await nextTick()
      okBtn.value?.focus()
    }
  },
)

function onKey(e: KeyboardEvent): void {
  if (!dialogState.visible) return
  if (e.key === 'Escape') {
    e.preventDefault()
    settleDialog(false)
  } else if (e.key === 'Enter') {
    e.preventDefault()
    settleDialog(true)
  }
}

onMounted(() => window.addEventListener('keydown', onKey))
onBeforeUnmount(() => window.removeEventListener('keydown', onKey))
</script>

<template>
  <Teleport to="body">
    <div v-if="dialogState.visible" class="dlg-mask" @click.self="settleDialog(false)">
      <div class="dlg panel" :class="{ danger: dialogState.danger }">
        <div class="dlg-head">
          <span class="dlg-icon"><Icon name="warning" :size="13" /></span>
          <span class="dlg-title ellipsis">{{ dialogState.title }}</span>
        </div>
        <div class="dlg-body">
          <p v-if="dialogState.message" class="dlg-msg">{{ dialogState.message }}</p>
          <ul v-if="dialogState.lines.length" class="dlg-lines mono">
            <li v-for="(l, i) in dialogState.lines" :key="i">{{ l }}</li>
          </ul>
        </div>
        <div class="dlg-foot">
          <button class="btn" @click="settleDialog(false)">{{ dialogState.cancelText }}</button>
          <button
            ref="okBtn"
            class="btn"
            :class="dialogState.danger ? 'btn-danger' : 'btn-primary'"
            @click="settleDialog(true)"
          >
            {{ dialogState.confirmText }}
          </button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.dlg-mask {
  position: fixed;
  inset: 0;
  z-index: 1000;
  background: rgba(4, 6, 12, 0.72);
  backdrop-filter: blur(3px);
  display: flex;
  align-items: center;
  justify-content: center;
  animation: dlg-fade 0.14s ease-out;
}
.dlg {
  width: 480px;
  max-width: 86vw;
  border-radius: var(--radius);
  background: var(--surface);
  border: 1px solid var(--line);
  box-shadow: var(--glow-strong), 0 18px 48px rgba(0, 0, 0, 0.55);
  overflow: hidden;
  animation: dlg-pop 0.16s ease-out;
}
.dlg.danger {
  border-color: rgba(255, 45, 120, 0.45);
  box-shadow: var(--danger-glow), 0 18px 48px rgba(0, 0, 0, 0.55);
}
.dlg-head {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 14px;
  border-bottom: 1px solid var(--line-faint);
  background: linear-gradient(90deg, rgba(56, 242, 200, 0.08), transparent 70%);
}
.dlg.danger .dlg-head {
  background: linear-gradient(90deg, rgba(255, 45, 120, 0.1), transparent 70%);
}
.dlg-icon {
  color: var(--danger);
  display: inline-flex;
  flex-shrink: 0;
}
.dlg-title {
  font-family: var(--mono);
  font-size: 12px;
  font-weight: 700;
  letter-spacing: 0.06em;
  color: #eef3f7;
  min-width: 0;
}
.dlg-body {
  padding: 14px;
  max-height: 46vh;
  overflow-y: auto;
}
.dlg-msg {
  font-size: 12.5px;
  color: var(--text);
  line-height: 1.75;
  white-space: pre-line;
}
.dlg-lines {
  list-style: none;
  margin-top: 12px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.dlg-lines li {
  font-size: 10.5px;
  color: var(--text-dim);
  background: var(--bg-deep);
  border: 1px solid var(--line-faint);
  border-radius: 4px;
  padding: 6px 9px;
  line-height: 1.7;
  white-space: pre-wrap;
  word-break: break-all;
}
.dlg-foot {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  padding: 10px 14px;
  border-top: 1px solid var(--line-faint);
}
@keyframes dlg-fade {
  from {
    opacity: 0;
  }
}
@keyframes dlg-pop {
  from {
    transform: translateY(6px) scale(0.98);
    opacity: 0;
  }
}
</style>
