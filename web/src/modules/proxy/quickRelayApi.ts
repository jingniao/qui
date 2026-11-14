import { request } from '../../app/apiClient';
import type { QuickRelayInput, QuickRelayPlan, QuickRelayResult } from './quickRelay';

export const quickRelayAPI = {
  plan: (value: QuickRelayInput) => request<QuickRelayPlan>('/relay-schemes/quick/plan', 'POST', value),
  create: (value: QuickRelayInput) => request<QuickRelayResult>('/relay-schemes/quick', 'POST', value)
};
