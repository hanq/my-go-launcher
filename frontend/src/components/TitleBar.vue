<script setup lang="ts">
import { windowMinimise, windowToggleMaximise, windowQuit } from '../api'
import { store } from '../store'

// 线性 SVG 图标（stroke 风格，viewBox 24×24，stroke-width 2）
</script>

<template>
  <header class="titlebar drag">
    <div class="brand">
      <span class="brand-mark">⌁</span>
      <span class="brand-name mono">GO&nbsp;LAUNCHER</span>
      <span class="brand-sub mono">/ dev console</span>
    </div>

    <div class="metrics mono no-drag">
      <span>GO {{ store.runtimeInfo.go }}</span>
      <span class="sep">·</span>
      <span>{{ store.runtimeInfo.os.toUpperCase() }}/{{ store.runtimeInfo.arch }}</span>
      <span class="sep">·</span>
      <span class="accent">v{{ store.runtimeInfo.version }}</span>
    </div>

    <div class="win-controls no-drag">
      <button class="wc" title="最小化" @click="windowMinimise">
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><line x1="5" y1="12" x2="19" y2="12"/></svg>
      </button>
      <button class="wc" title="最大化/还原" @click="windowToggleMaximise">
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><rect x="5" y="5" width="14" height="14" rx="1"/></svg>
      </button>
      <button class="wc close" title="关闭" @click="windowQuit">
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><line x1="6" y1="6" x2="18" y2="18"/><line x1="18" y1="6" x2="6" y2="18"/></svg>
      </button>
    </div>
  </header>
</template>

<style scoped>
.titlebar {
  display: flex;
  align-items: center;
  height: 38px;
  padding: 0 12px;
  background: var(--bg-deep);
  border-bottom: 1px solid var(--line-faint);
  gap: 12px;
  flex-shrink: 0;
}
.brand {
  display: flex;
  align-items: baseline;
  gap: 8px;
}
.brand-mark {
  color: var(--accent);
  font-size: 15px;
  text-shadow: var(--glow);
  line-height: 1;
}
.brand-name {
  font-size: 13px;
  font-weight: 700;
  letter-spacing: 0.22em;
  color: #eef3f7;
}
.brand-sub {
  font-size: 10px;
  letter-spacing: 0.14em;
  color: var(--text-faint);
}
.metrics {
  margin-left: auto;
  font-size: 10.5px;
  letter-spacing: 0.08em;
  color: var(--text-faint);
  gap: 8px;
  display: flex;
  align-items: center;
}
.metrics .sep {
  opacity: 0.4;
}
.accent {
  color: var(--accent);
}
.win-controls {
  display: flex;
  gap: 2px;
  margin-left: 12px;
}
.wc {
  width: 34px;
  height: 26px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: transparent;
  border: none;
  border-radius: 3px;
  color: var(--text-dim);
  cursor: pointer;
  transition: all 0.12s;
}
.wc:hover {
  background: var(--surface-2);
  color: var(--accent);
}
.wc.close:hover {
  background: var(--danger);
  color: #fff;
}
</style>
