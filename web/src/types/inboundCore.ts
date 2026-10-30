import type { CoreName } from '../modules/nodes/api';
export interface InboundCoreFields { core:CoreName|''; userTrafficSupported:boolean|null; deviceGateSupported:boolean|null; }
export interface InboundCoreInput { core?:CoreName|''; }
