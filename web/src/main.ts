import { createApp } from 'vue'
import { createPinia } from 'pinia'
import router from './router'
import App from './App.vue'

// naive-ui 按需引入即可（组件内直接 import），无需全局安装
createApp(App).use(createPinia()).use(router).mount('#app')
