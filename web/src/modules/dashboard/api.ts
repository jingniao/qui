import { request } from '../../app/apiClient';
import type { NodeView } from '../nodes/api';
import type { ProxyChain } from '../proxy/api';
import type { Revision } from '../system/deploymentApi';
export interface Counts { totalNodes:number; onlineNodes:number; offlineNodes:number; abnormalNodes:number; onlineUsers:number|null; onlineDevices:number|null; activeConnections:number|null; measuredNodes:number; expectedNodes:number; measuredUsers:number; measuredDevices:number; measuredConnections:number; todayBlocked:number; }
export interface Recent { time:string; nodeName:string; userName:string; deviceName:string; domain:string; targetIp:string; action:string; allowed:boolean; connections:number; }
export interface DashboardBase { generatedAt:string; timezone:string; dayStart:string; counts:Counts; nodes:NodeView[]; recentAccess?:Recent[]; recentBlocked?:Recent[]; revisions?:Revision[]; chains?:ProxyChain[]; nodeNames?:Record<string,string>; }
export interface ExchangeRates { base:string; updatedAt:string; nextUpdateAt:string; source:string; sourceUrl:string; rates:Record<string,number>; }
export const dashboardAPI={base:()=>request<DashboardBase>('/dashboard/base'), exchangeRates:()=>request<ExchangeRates>('/dashboard/exchange-rates')};
