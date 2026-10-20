export type AdminRole = 'Owner' | 'Viewer';

export interface AdminUser {
  id: string;
  username: string;
  displayName?: string;
  role: AdminRole;
  status: 'active' | 'disabled';
  lastLoginAt: string;
  loginIp: string;
  createdAt: string;
}
