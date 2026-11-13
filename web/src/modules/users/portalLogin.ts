// 用户门户登录的纯逻辑：新建用户抽屉的两步保存、门户地址与可发送给用户的文本。
// 不依赖 Vue 或 request，便于自测。

// 密码规则与后台其余密码一致：12～128 个字符（按字符计，不按字节）。
export function portalPasswordError(password: string, confirm: string): string {
  const length = Array.from(password).length;
  if (length < 12 || length > 128) return '门户登录密码须为 12～128 个字符';
  if (password !== confirm) return '两次输入的密码不一致';
  return '';
}

export interface CreatedUserRef { id: string }

export interface CreateWithLoginDeps<U extends CreatedUserRef> {
  create: () => Promise<U>;
  setLogin: (userId: string, password: string) => Promise<unknown>;
}

export type CreateWithLoginResult<U extends CreatedUserRef> =
  | { user: U; loginSet: boolean; loginError: '' }
  | { user: U; loginSet: false; loginError: string };

// 第一步创建用户（失败直接抛出，用户并未创建）；勾选了门户登录时第二步设置密码。
// 第二步失败时用户已存在，只返回原因，调用方提示并转到详情页，绝不再次创建。
export async function createUserWithPortalLogin<U extends CreatedUserRef>(
  deps: CreateWithLoginDeps<U>,
  portalPassword: string | null
): Promise<CreateWithLoginResult<U>> {
  const user = await deps.create();
  if (portalPassword === null) return { user, loginSet: false, loginError: '' };
  try {
    await deps.setLogin(user.id, portalPassword);
    return { user, loginSet: true, loginError: '' };
  } catch (e) {
    return { user, loginSet: false, loginError: (e as Error)?.message || '未知错误' };
  }
}

export function portalLoginFailureMessage(reason: string): string {
  return `用户已创建，门户登录未设置：${reason}`;
}

// 用户门户登录页就是站点根地址。主机与端口取「节点安装连接地址」里的面板地址与
// Web 端口；主机为空时用当前访问地址；端口为 0 或 443 时省略。
export function portalAddress(endpoint: { host?: string; httpPort?: number } | null | undefined, locationHost: string): string {
  const host = (endpoint?.host ?? '').trim();
  if (!host) return `https://${locationHost}/`;
  const port = endpoint?.httpPort ?? 0;
  return port && port !== 443 ? `https://${host}:${port}/` : `https://${host}/`;
}

// 一次发给用户的两行文本。
export function portalShareText(portal: string, subscription: string): string {
  return `门户地址：${portal}\n订阅链接：${subscription}`;
}
