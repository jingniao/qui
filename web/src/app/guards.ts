import type { NavigationGuardNext, RouteLocationNormalized } from 'vue-router';
import { authState, ensureSession } from './auth';
import { portalGuard } from '../portal/guard';

export async function setupNavigationGuards(to: RouteLocationNormalized, _from: RouteLocationNormalized, next: NavigationGuardNext) {
  if (to.path.startsWith('/portal/')) {
    const result = await portalGuard(to);
    return result === true ? next() : next(result);
  }
  const publicPaths = ['/login', '/setup', '/403', '/404'];
  try { await ensureSession(); }
  catch {
    return publicPaths.includes(to.path) ? next() : next('/login');
  }
  if (!authState.isInitialized && to.path !== '/setup') return next('/setup');
  if (authState.isInitialized && to.path === '/setup') return next(authState.isAuthenticated ? '/dashboard' : '/login');
  if (!authState.isAuthenticated && !publicPaths.includes(to.path)) return next({ path: '/login', query: { redirect: to.fullPath } });
  if (authState.isAuthenticated && to.path === '/login') return next('/dashboard');
  if (authState.currentUser?.role === 'Viewer' &&
      !['/dashboard', '/nodes', '/403', '/404'].includes(to.path) &&
      !/^\/nodes\/[^/]+$/.test(to.path)) return next('/403');
  next();
}
