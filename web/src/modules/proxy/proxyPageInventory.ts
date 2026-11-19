import { shallowReactive } from 'vue';
import { nodeAPI, type NodeView } from '../nodes/api';
import { groupAPI, type NodeGroup } from '../nodes/groupsApi';
import { applyRuntimeSnapshot, type RuntimeSnapshot } from '../nodes/runtimeSnapshots';
import { choices, proxyAPI, type Inbound } from './api';

// The drawer may open while the page is fetching its lists. Share that exact
// read, including an empty result, and release failed reads so retry is fresh.
export function proxyPageInventory() {
  const editor = shallowReactive<{ servers?: Promise<NodeView[]>; inbounds?: Promise<Inbound[]>; groups?: Promise<NodeGroup[]> }>({});
  const latest = new Map<string, RuntimeSnapshot>();
  let generation = '', inventoryRevision = 0;

  function applyEvent(servers: NodeView[], event: RuntimeSnapshot) {
    const changed = generation !== event.generation || event.inventoryRevision > inventoryRevision;
    if (changed) { generation = event.generation; inventoryRevision = event.inventoryRevision; }
    const previous = latest.get(event.node.nodeId);
    if (previous?.generation === event.generation && previous.node.revision >= event.node.revision) return changed;
    latest.set(event.node.nodeId, event);
    const index = servers.findIndex(node => node.id === event.node.nodeId);
    if (index < 0) return changed;
    const updated = applyRuntimeSnapshot(servers[index], event.node);
    if (updated) servers[index] = updated;
    else servers.splice(index, 1);
    return changed;
  }
  function readServers() {
    const started = new Map(latest);
    const read = choices((p, s) => nodeAPI.choices(p, '', s)).then(items => items.flatMap(item => {
      const update = latest.get(item.id);
      if (update === started.get(item.id)) return [item];
      const current = applyRuntimeSnapshot(item, update?.node);
      return current ? [current] : [];
    })).catch(e => { if (editor.servers === read) delete editor.servers; throw e; });
    editor.servers = read;
    return read;
  }
  function readInbounds() {
    const read = choices((p, s) => proxyAPI.inbounds('', p, s)).catch(e => { if (editor.inbounds === read) delete editor.inbounds; throw e; });
    editor.inbounds = read;
    return read;
  }
  function readGroups() {
    const read = choices(groupAPI.list).catch(e => { if (editor.groups === read) delete editor.groups; throw e; });
    editor.groups = read;
    return read;
  }
  return { editor, readServers, readInbounds, readGroups, applyEvent, resetStream: () => { latest.clear(); generation = ''; inventoryRevision = 0; } };
}
