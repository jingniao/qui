import type { NavigationGuardNext, RouteLocationNormalized } from 'vue-router';
import { authState, ensureSession } from './auth';
import { adminEntryPage } from './adminEntry';
import { portalGuard } from '../portal/guard';

export async function setupNavigationGuards(to: RouteLocationNormalized, _from: RouteLocationNormalized, next: NavigationGuardNext) {
  if (to.path.startsWith('/portal/')) {
    const result = await portalGuard(to);
    return result === true ? next() : next(result);
  }
  // The site root is the user login page. Signed-out visitors of admin pages are
  // sent there, not to the admin login, so the admin address is never revealed.
  const loginPath = adminEntryPage ?? '';
  const publicPaths = ['/', '/setup', '/403', '/404', ...(loginPath ? [loginPath] : [])];
  try { await ensureSession(); }
  catch {
    return publicPaths.includes(to.path) ? next() : next('/');
  }
  if (!authState.isInitialized && to.path !== '/setup') return next('/setup');
  if (authState.isInitialized && to.path === '/setup') return next(authState.isAuthenticated ? '/dashboard' : '/');
  if (!authState.isAuthenticated && !publicPaths.includes(to.path)) return next('/');
  if (authState.isAuthenticated && (to.path === '/' || to.path === loginPath)) return next('/dashboard');
  if (authState.currentUser?.role === 'Viewer' &&
      !['/dashboard', '/nodes', '/proxy/tunnels', '/proxy/tunnel-rules', '/403', '/404'].includes(to.path) &&
      !(to.path !== '/nodes/versions' && /^\/nodes\/[^/]+$/.test(to.path))) return next('/403');
  next();
}
