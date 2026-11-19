export interface EnrollmentStage {
  label: string;
  done: boolean;
}

export function enrollmentStage(status: string | undefined): EnrollmentStage {
  switch (status) {
    case 'pending':
      return { label: '等待 Agent 登记', done: false };
    case 'offline':
      return { label: '已登记，等待连接', done: false };
    case 'online':
    case 'degraded':
      return { label: '已上线', done: true };
    case 'applying':
      return { label: '已上线，正在应用配置', done: true };
    default:
      return { label: `状态：${status || '未知'}`, done: false };
  }
}

export function recoveryEnrollmentStage(status: string, sameRecovery: boolean): EnrollmentStage | null {
  if (!sameRecovery) return { label: '恢复码已被替换', done: false };
  if (status === 'consumed') return null;
  const labels: Record<string, string> = { issued: '等待新 Agent 登记', expired: '恢复码已过期', superseded: '恢复码已被替换' };
  return { label: labels[status] || '无法确认恢复状态', done: false };
}
