// 管理员登录地址（v1.5.4 第 10 项）。面板只在请求路径等于当前管理员登录地址时，在
// index.html 里注入 <meta name="qui-entry" content="admin">；其他路径不带这个标记，
// 也就不会显示管理员登录页。站点根地址 `/` 是用户门户登录页。

const STORAGE_KEY = 'qui_admin_entry';

/** 本次页面加载的路径就是管理员登录地址时，返回该路径，否则为 null。 */
export const adminEntryPage: string | null =
  typeof document !== 'undefined' && document.querySelector('meta[name="qui-entry"][content="admin"]')
    ? normalize(window.location.pathname)
    : null;

if (adminEntryPage) rememberAdminEntry(adminEntryPage);

function normalize(path: string): string {
  return path.length > 1 ? path.replace(/\/+$/, '') : path;
}

/** 记住管理员登录地址，退出登录后回到这里。只保存在本浏览器。 */
export function rememberAdminEntry(path: string) {
  try { localStorage.setItem(STORAGE_KEY, path); } catch { /* 存储不可用时退出后回到 `/` */ }
}

/** 退出登录后去的地址：本浏览器记住的管理员登录地址，未知时为用户登录页 `/`。 */
export function adminLogoutTarget(): string {
  try { return localStorage.getItem(STORAGE_KEY) || '/'; } catch { return '/'; }
}

/**
 * 回到管理员登录页。管理员登录路由只在页面从该地址加载时才注册，所以这里整页跳转，
 * 让面板重新判断并注入标记。
 */
export function goToAdminLogin() {
  window.location.assign(adminLogoutTarget());
}
