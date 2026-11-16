import type { CoreName, NodeView } from '../nodes/api';
import type { CanvasPublishResult, CanvasTunnelPlan } from './api';
import { nodeCores } from '../nodes/coreSummary';
import { protocolChoices } from './components/node-editor/nodeEditorModel';

// canvas_publish.go limits a linear path to three servers (two edges).
export const MAX_QUICK_RELAY_MIDDLE_NODES = 1;

export type QuickRelayGroupMode = 'exit_groups' | 'explicit' | 'none';
export interface QuickRelayForm {
  entryNodeId: string;
  middleNodeIds: string[];
  exitInboundId: string;
  // An imported exit (v1.6.0); exactly one of the two exits is set.
  exitImportId?: string;
  entryProtocol: string;
  entryCore: CoreName | '';
  entrySecurity?: '' | 'none' | 'tls' | 'reality';
  entryTransport?: '' | 'tcp' | 'xhttp';
  entryFlow?: '' | 'xtls-rprx-vision';
  entryRealityTarget?: string;
  entryRealityServerName?: string;
  groupMode: QuickRelayGroupMode;
  groupIds: string[];
}
export interface QuickRelayInput {
  requestId: string;
  entryNodeId: string;
  middleNodeIds: string[];
  exitInboundId: string;
  exitImportId?: string;
  entryProtocol: string;
  entryCore?: CoreName;
  entrySecurity?: QuickRelayForm['entrySecurity'];
  entryTransport?: QuickRelayForm['entryTransport'];
  entryFlow?: QuickRelayForm['entryFlow'];
  entryRealityTarget?: string;
  entryRealityServerName?: string;
  entryGroups: { mode: QuickRelayGroupMode; groupIds?: string[] };
}
export interface QuickRelayEntry {
  instanceId: string;
  mode: string;
  nodeId: string;
  nodeName: string;
  inboundId?: string;
  protocol: string;
  port: number;
  name: string;
  route: string;
  groups: { id: string; name: string }[];
  previousRoute?: string;
}
export interface QuickRelayPlan extends CanvasTunnelPlan {
  entries: QuickRelayEntry[];
}
export interface QuickRelayResult {
  schemeId: string;
  publication: CanvasPublishResult & { entries?: QuickRelayEntry[] };
  warnings: string[];
}

export function buildQuickRelayInput(form: QuickRelayForm, requestId: string): QuickRelayInput {
  return {
    requestId,
    entryNodeId: form.entryNodeId,
    middleNodeIds: [...form.middleNodeIds],
    exitInboundId: form.exitImportId ? '' : form.exitInboundId,
    ...(form.exitImportId ? { exitImportId: form.exitImportId } : {}),
    entryProtocol: form.entryProtocol,
    ...(form.entryCore ? { entryCore: form.entryCore } : {}),
    ...(form.entryProtocol === 'vless' ? {
      ...(form.entrySecurity !== undefined ? { entrySecurity: form.entrySecurity } : {}),
      ...(form.entryTransport ? { entryTransport: form.entryTransport } : {}),
      ...(form.entryFlow ? { entryFlow: form.entryFlow } : {}),
      ...(form.entrySecurity === 'reality' ? { entryRealityTarget: form.entryRealityTarget, entryRealityServerName: form.entryRealityServerName } : {})
    } : {}),
    entryGroups: form.groupMode === 'explicit'
      ? { mode: form.groupMode, groupIds: [...form.groupIds] }
      : { mode: form.groupMode }
  };
}

export function quickRelayProtocolChoices(server: NodeView | undefined): { protocol: string; core: CoreName }[] {
  return nodeCores(server).filter(core => core.name !== 'gost').flatMap(core => {
    // Forward listeners are not user entries. All core availability and inbound
    // support checks belong to the same helper used by the inbound editor.
    const protocols = [...new Set(core.protocols.map(item => item.protocol).filter(protocol => protocol !== 'forward'))];
    return protocolChoices(server, core.name, protocols, '').map(protocol => ({ protocol, core: core.name }));
  });
}
