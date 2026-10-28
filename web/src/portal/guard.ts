import type { RouteLocationNormalized } from 'vue-router';
import { portalAPI } from './api';

export async function portalGuard(to: RouteLocationNormalized) {
  if (to.path === '/portal/login') return true;
  try { await portalAPI.session(); return true; }
  catch { return { path: '/portal/login' }; }
}
