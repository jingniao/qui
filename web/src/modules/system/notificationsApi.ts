import { request } from '../../app/apiClient';

export interface Notification {
  id: number;
  at: string;
  kind: string;
  severity: 'info' | 'warning' | 'critical' | 'resolved' | string;
  nodeId?: string;
  title: string;
  body: string;
  read: boolean;
}

export interface NotificationListResponse {
  items: Notification[];
  total: number;
  unread: number;
}

export interface NotificationRule {
  kind: string;
  enabled: boolean;
  threshold: number;
  durationMinutes: number;
  cooldownMinutes: number;
}

export interface NotificationChannelConfig {
  botToken?: string;
  chatId?: string;
  url?: string;
}

export interface NotificationChannel {
  id: string;
  kind: 'telegram' | 'webhook' | string;
  name: string;
  enabled: boolean;
  last4: string;
  lastResultCode: string;
  lastSentAt?: string;
}

export interface NotificationChannelInput {
  id?: string;
  kind: string;
  name: string;
  enabled: boolean;
  config?: NotificationChannelConfig;
}

export function severityTone(s: string): 'info' | 'warning' | 'danger' | 'success' {
  switch (s) {
    case 'resolved':
      return 'success';
    case 'critical':
      return 'danger';
    case 'warning':
      return 'warning';
    default:
      return 'info';
  }
}

export const notificationsAPI = {
  list(unreadOnly = false, page = 1, pageSize = 20): Promise<NotificationListResponse> {
    const params = new URLSearchParams({
      page: String(page),
      pageSize: String(pageSize)
    });
    if (unreadOnly) {
      params.set('unread', '1');
    }
    return request<NotificationListResponse>(`/notifications?${params.toString()}`);
  },

  readAll(): Promise<void> {
    return request<void>('/notifications/read-all', 'POST');
  },

  async rules(): Promise<NotificationRule[]> {
    const res = await request<{ items: NotificationRule[] }>('/notification-rules');
    return res.items || [];
  },

  saveRules(items: NotificationRule[]): Promise<{ items: NotificationRule[] }> {
    return request<{ items: NotificationRule[] }>('/notification-rules', 'PUT', { items });
  },

  async channels(): Promise<NotificationChannel[]> {
    const res = await request<{ items: NotificationChannel[] }>('/notification-channels');
    return res.items || [];
  },

  saveChannel(input: NotificationChannelInput): Promise<NotificationChannel> {
    if (input.id) {
      return request<NotificationChannel>(`/notification-channels/${input.id}`, 'PUT', {
        name: input.name,
        enabled: input.enabled,
        config: input.config
      });
    }
    return request<NotificationChannel>('/notification-channels', 'POST', {
      kind: input.kind,
      name: input.name,
      enabled: input.enabled,
      config: input.config
    });
  },

  deleteChannel(id: string): Promise<void> {
    return request<void>(`/notification-channels/${id}`, 'DELETE');
  },

  testChannel(id: string): Promise<{ code: string }> {
    return request<{ code: string }>(`/notification-channels/${id}/test`, 'POST');
  }
};
