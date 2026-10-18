import { createApp } from 'vue';
import { watch } from 'vue';
import App from './App.vue';
import { router } from './app/router';
import { authState, clearSession } from './app/auth';
import { connectEvents, disconnectEvents } from './app/events';
import './styles/main.css';

const app = createApp(App);

app.use(router);

app.mount('#app');
watch(() => authState.isAuthenticated, value => value ? connectEvents() : disconnectEvents(), { immediate: true });
window.addEventListener('auth-expired', () => { clearSession(); router.replace('/login'); });
