import { createApp } from 'vue'
import '@xterm/xterm/css/xterm.css'
import './style.css'
import App from './App.vue'
import { initStore } from './store'

initStore()
createApp(App).mount('#app')
