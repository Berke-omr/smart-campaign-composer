import { createApp } from 'vue'
import App from './App.vue'
import router from './router'
// Fonts are self-hosted from npm (no request to Google Fonts). Each package's
// CSS lists one file per script subset with a unicode-range, so the browser
// downloads only the subsets a page uses (latin and latin-ext for Turkish).
import '@fontsource-variable/inter'
import '@fontsource-variable/urbanist'
import '@fontsource-variable/jetbrains-mono'
import './assets/theme.css'
import './assets/base.css'

const app = createApp(App)

app.use(router)

app.mount('#app')
