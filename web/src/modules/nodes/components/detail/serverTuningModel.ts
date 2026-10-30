import type { NodeTuningInput } from '../../api';
export interface TuningField {key:string; label:string; choices?:string[]; min?:number; max?:number;}
export const tuningFields: TuningField[] = [
  {key:'net.ipv4.tcp_congestion_control',label:'TCP 拥塞控制',choices:['bbr','cubic','reno','htcp','hybla','bbr2','bbr3']},
  {key:'net.core.default_qdisc',label:'排队算法',choices:['fq','fq_codel','cake','pfifo_fast']},
  ...['tcp_ecn','tcp_tw_reuse','tcp_mtu_probing'].map(key=>({key:'net.ipv4.'+key,label:key,min:0,max:2})),
  {key:'fs.file-max',label:'文件句柄上限',min:65536,max:16777216},
  ...['tcp_rmem','tcp_wmem'].map(key=>({key:'net.ipv4.'+key,label:key+'（仅最大值）',min:4194304,max:268435456})),
  {key:'net.core.somaxconn',label:'连接队列',min:128,max:65535},
  {key:'net.ipv4.tcp_max_syn_backlog',label:'半连接队列',min:128,max:262144},
  ...['rmem_max','wmem_max'].map(key=>({key:'net.core.'+key,label:key,min:212992,max:268435456})),
  {key:'net.ipv4.tcp_fastopen',label:'TCP Fast Open',min:0,max:3}
];
export function tuningStatus(status: string): string {
  return ({pending:'等待 Agent 执行',applied:'已应用',cleared:'已清除',failed:'执行失败',unconfirmed:'结果未确认'} as Record<string,string>)[status] || '未知状态';
}
/** Complete draft for every whitelisted key: an empty value selects the "不设置" option. */
export function tuningDraft(settings:Record<string,string> = {}): Record<string,string> {
  const draft:Record<string,string> = {};
  for (const field of tuningFields) {
    const value = settings[field.key];
    draft[field.key] = typeof value === 'string' ? value : '';
  }
  return draft;
}
export function tuningRequest(nodeId:string, draft:Record<string,string>, clear=false): NodeTuningInput {
  if (clear) return {settings:{},clear:true,confirmNodeId:nodeId};
  const settings:Record<string,string> = {};
  for (const [key, raw] of Object.entries(draft)) {
    const value = raw.trim();
    if (!value) continue;
    const field = tuningFields.find(f=>f.key===key);
    if (!field) throw new Error('非白名单参数：'+key);
    if (field.choices ? !field.choices.includes(value) : !/^\d+$/.test(value) || !Number.isSafeInteger(Number(value)) || Number(value)<field.min! || Number(value)>field.max!) throw new Error('参数超出允许范围：'+key);
    settings[key]=value;
  }
  if (!Object.keys(settings).length) throw new Error('请填写至少一个参数；恢复原值请使用清除调优');
  return {settings,clear:false,confirmNodeId:nodeId};
}
export const tuningPresets: {name:string;settings:Record<string,string>}[] = [
  {name:'BBR / FQ 草稿',settings:{'net.ipv4.tcp_congestion_control':'bbr','net.core.default_qdisc':'fq'}},
  {name:'Cubic / FQ-CoDel 草稿',settings:{'net.ipv4.tcp_congestion_control':'cubic','net.core.default_qdisc':'fq_codel'}}
];
