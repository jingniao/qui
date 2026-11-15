import { APIError } from '../../app/apiClient';

export interface InboundConflict {
  message: string;
  detached?: boolean;
  outbounds?: { id: string; name: string; nodeId: string }[];
  schemes?: { id: string; name: string }[];
}

export function inboundDeleteConflict(error: unknown): InboundConflict | null {
  if (!(error instanceof APIError) || !['inbound_has_relay_outbound', 'inbound_in_canvas_scheme', 'inbound_detached_entry'].includes(error.code)) return null;
  return { ...error.details, message: error.message, detached: error.code === 'inbound_detached_entry' } as InboundConflict;
}
