import { reactive } from 'vue';

// Toast notification helper
export interface ToastMessage {
  id: string;
  type: 'success' | 'error' | 'warning' | 'info';
  title?: string;
  message: string;
}

export const toastMessages = reactive<ToastMessage[]>([]);

export function showToast(message: string, type: 'success' | 'error' | 'warning' | 'info' = 'success', title?: string) {
  const id = Math.random().toString(36).substring(2, 9);
  toastMessages.push({ id, message, type, title });
  setTimeout(() => {
    const idx = toastMessages.findIndex(t => t.id === id);
    if (idx !== -1) toastMessages.splice(idx, 1);
  }, 4000);
}

