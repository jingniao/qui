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
