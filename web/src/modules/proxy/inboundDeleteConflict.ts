import { APIError } from '../../app/apiClient';

export interface InboundConflict {
  message: string;
  outbounds: { id: string; name: string; nodeId: string }[];
  schemes?: { id: string; name: string }[];
}

export function inboundDeleteConflict(error: unknown): InboundConflict | null {
  if (!(error instanceof APIError) || error.code !== 'inbound_has_relay_outbound') return null;
  return { message: error.message, ...error.details } as InboundConflict;
}
