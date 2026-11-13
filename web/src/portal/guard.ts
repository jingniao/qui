import type { RouteLocationNormalized } from 'vue-router';
import { portalAPI } from './api';

// Unauthenticated portal visitors go to the site root, which is the user login page.
export async function portalGuard(to: RouteLocationNormalized) {
  if (to.path === '/') return true;
  try { await portalAPI.session(); return true; }
  catch { return { path: '/' }; }
}
