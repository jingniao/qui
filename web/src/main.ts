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
watch(() => [authState.isAuthenticated, router.currentRoute.value.path] as const, ([authenticated, path]) => {
  if (authenticated && !path.startsWith('/portal/')) connectEvents();
  else disconnectEvents();
}, { immediate: true });
window.addEventListener('auth-expired', () => {
  clearSession();
  if (!router.currentRoute.value.path.startsWith('/portal/')) void router.replace('/login');
});
