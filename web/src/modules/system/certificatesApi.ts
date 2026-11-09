import { request } from '../../app/apiClient';

export interface CloudflareStatus { configured: boolean; last4: string; verifiedAt: string; }
export interface CertificateSettings {
  email: string; directory: 'production' | 'staging'; deployStartHour: number; deployEndHour: number;
  revision: number; cloudflare: CloudflareStatus;
}
// cloudflareToken: omitted keeps the stored token, '' clears it, a value replaces it.
export interface CertificateSettingsInput {
  email: string; directory: 'production' | 'staging'; deployStartHour: number; deployEndHour: number;
  cloudflareToken?: string; expectedRevision: number;
}
export type CertificateStatus = 'pending' | 'issuing' | 'valid' | 'failed';
export interface CertificateUsage { inboundId: string; name: string; nodeId: string; }
export interface ManagedCertificate {
  id: string; domains: string[]; status: CertificateStatus; notBefore: string; notAfter: string;
  lastErrorCode: string; lastErrorText: string; lastAttemptAt: string; nextAttemptAt: string;
  autoDns: boolean; dnsNodeId: string; usedBy: CertificateUsage[]; createdAt: string; updatedAt: string;
}
export const certificatesAPI = {
  settings: () => request<CertificateSettings>('/system/certificate-settings'),
  saveSettings: (input: CertificateSettingsInput) => request<CertificateSettings>('/system/certificate-settings', 'PUT', input),
  list: () => request<{ items: ManagedCertificate[] }>('/certificates').then(result => result.items),
  create: (domains: string[], autoDns: boolean, dnsNodeId: string) => request<ManagedCertificate>('/certificates', 'POST', autoDns ? { domains, autoDns, dnsNodeId } : { domains, autoDns }),
  renew: (id: string) => request<unknown>(`/certificates/${encodeURIComponent(id)}/renew`, 'POST'),
  remove: (id: string) => request<unknown>(`/certificates/${encodeURIComponent(id)}`, 'DELETE')
};

export const CERTIFICATE_STATUS_LABELS: Record<CertificateStatus, string> = { pending: '等待', issuing: '签发中', valid: '有效', failed: '失败' };
// Hours are 0-23, start differs from end, end is exclusive and may wrap midnight.
export function deployWindowError(start: number, end: number): string {
  if (![start, end].every(hour => Number.isInteger(hour) && hour >= 0 && hour <= 23)) return '发布时段的小时须为 0～23 的整数';
  if (start === end) return '发布时段的开始与结束不能相同';
  return '';
}
// Domains one per line; blanks dropped, case folded like the panel does.
export function parseDomainLines(text: string): string[] {
  return text.split(/\r?\n/).map(line => line.trim().toLowerCase()).filter(Boolean);
}
// Certificates still moving through issuance, which the page keeps polling.
export function certificatesInProgress(items: ManagedCertificate[]): boolean {
  return items.some(item => item.status === 'pending' || item.status === 'issuing');
}
