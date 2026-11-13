// 管理员登录地址：单段路径，以 / 开头，含斜杠共 4～64 个字符，斜杠后只含 [a-z0-9_-]。
// 与后端同一规则；保留路径冲突由服务端判定（返回 400 invalid_settings 与中文说明）。
export function adminEntryPathError(path: string): string {
  if (!path.startsWith('/')) return '地址须以 / 开头';
  if (path.length < 4 || path.length > 64) return '地址长度须为 4～64 个字符（含开头的 /）';
  if (!/^\/[a-z0-9_-]+$/.test(path)) return '地址只能是一段路径，斜杠后仅含小写字母、数字、- 和 _';
  return '';
}

export function adminEntryURL(origin: string, path: string): string {
  return `${origin}${path}`;
}

export const ADMIN_ENTRY_CONFIRM_NOTE = '保存后旧地址立即失效，请记住新地址；忘记时可在服务器上运行 qui 菜单重置。';
