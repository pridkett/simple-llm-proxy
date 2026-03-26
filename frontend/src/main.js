import { createApp } from 'vue'
import App from './App.vue'
import router from './router/index.js'
import VueApexCharts from 'vue3-apexcharts'
import './style.css'

createApp(App).use(router).use(VueApexCharts).mount('#app')
