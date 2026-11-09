import { request } from '../../app/apiClient';
import type { NodeView } from '../nodes/api';
import type { CoreName } from './coreUpdatesApi';

// GET /api/v1/core-releases answers the newest stable releases of one core and
// source with, per platform, the official asset URL and the digest the vendor
// published for it (Xray .dgst, the GitHub asset digest for sing-box, our own
// SHA256SUMS for the qui build, the reviewed pin for GOST). The panel computes no
// digest: an asset whose published digest is missing or contradictory comes back
// with sha256 '' and the Owner pastes one. Whatever the form ends up with is
// validated again by POST /api/v1/core-updates and by the Agent.
export type CoreSource = 'official' | 'qui';

export interface CoreReleaseAsset {
  platform: string;
  libc: '' | 'musl';
  url: string;
  sha256: string;
  digestSource: '' | 'dgst' | 'github' | 'sha256sums' | 'pinned';
}

export interface CoreRelease { version: string; publishedAt: string; assets: CoreReleaseAsset[]; }
export interface CoreReleaseList { component: CoreName; source: CoreSource; items: CoreRelease[]; }

export const coreReleasesAPI = {
  list: (component: CoreName, source: CoreSource, refresh = false) =>
    request<CoreReleaseList>(`/core-releases?component=${encodeURIComponent(component)}&source=${source}${refresh ? '&refresh=1' : ''}`)
};

export const DIGEST_SOURCE_LABELS: Record<CoreReleaseAsset['digestSource'], string> = {
  '': '',
  dgst: '官方 .dgst 校验文件',
  github: 'GitHub 发布页公布的 SHA-256',
  sha256sums: 'qui 发布的 SHA256SUMS',
  pinned: '已审阅的固定摘要'
};

// The qui sing-box build is offered to an Agent that reports user statistics or
// multi-core support, the same rule the panel applies when it queues the task.
export function quiSourceAllowed(node: NodeView | null | undefined): boolean {
  const caps = node?.capabilities;
  return Boolean(caps?.singbox_user_stats || caps?.multi_core_supported);
}

// The source an upgrade keeps by default: the build the node already runs. A node
// that reports sing-box user statistics runs the qui build; one that reports
// sing-box without them runs the official build. Adding sing-box defaults to the
// qui build where the Agent accepts it, like the installer does.
export function defaultCoreSource(component: CoreName, node: NodeView | null | undefined): CoreSource {
  if (component !== 'sing-box') return 'official';
  const core = node?.capabilities?.cores?.find(item => item.name === 'sing-box');
  if (core && typeof core.singbox_user_stats === 'boolean') return core.singbox_user_stats ? 'qui' : 'official';
  if (!core && node?.capabilities?.core_name === 'sing-box' && typeof node.capabilities.singbox_user_stats === 'boolean') {
    return node.capabilities.singbox_user_stats ? 'qui' : 'official';
  }
  return quiSourceAllowed(node) ? 'qui' : 'official';
}

export function nodeArchitecture(node: NodeView | null | undefined): 'amd64' | 'arm64' | '' {
  if (!node) return '';
  const caps = node.capabilities as Record<string, unknown> | undefined;
  const arch = String(node.capabilities?.host?.architecture || caps?.architecture || '');
  if (arch === 'amd64' || arch === 'arm64') return arch;
  const platform = String(caps?.platform || node.os || '');
  return platform.endsWith('/amd64') ? 'amd64' : platform.endsWith('/arm64') ? 'arm64' : '';
}

// Alpine is the musl host this project supports; every other reported system is
// treated as glibc. Only the official sing-box release ships a separate musl
// build, so for Xray, GOST and the qui build this changes nothing.
export function nodeUsesMusl(node: NodeView | null | undefined): boolean {
  return /alpine/i.test(node?.capabilities?.host?.osPrettyName || '');
}

// deriveAssetURL rebuilds the official asset URL from a version typed by hand,
// for the same server the form shows. It answers '' when the URL cannot be
// derived from the core version: the qui build lives under a panel release tag,
// GOST is one pinned archive, and an unknown architecture has no asset at all.
// Whatever it answers is only a convenience - the panel and the Agent validate
// the URL, the asset name and the platform again.
export function deriveAssetURL(component: CoreName, source: CoreSource, version: string, node: NodeView | null | undefined): string {
  if (source !== 'official' || !version.trim()) return '';
  const arch = nodeArchitecture(node);
  if (!arch) return '';
  if (component === 'xray') {
    return `https://github.com/XTLS/Xray-core/releases/download/v${version}/${arch === 'arm64' ? 'Xray-linux-arm64-v8a.zip' : 'Xray-linux-64.zip'}`;
  }
  if (component === 'sing-box') {
    return `https://github.com/SagerNet/sing-box/releases/download/v${version}/sing-box-${version}-linux-${arch}${nodeUsesMusl(node) ? '-musl' : ''}.tar.gz`;
  }
  return '';
}

// pickAsset answers the archive of a release for one node, or null when the
// release has none for its architecture (or C library) - never a guess.
export function pickAsset(item: CoreRelease | undefined, node: NodeView | null | undefined): CoreReleaseAsset | null {
  if (!item) return null;
  const arch = nodeArchitecture(node);
  if (!arch) return null;
  const candidates = item.assets.filter(asset => asset.platform === 'linux/' + arch);
  const hasMusl = candidates.some(asset => asset.libc === 'musl');
  const libc = nodeUsesMusl(node) && hasMusl ? 'musl' : '';
  if (nodeUsesMusl(node) && !hasMusl && item.assets.some(asset => asset.libc === 'musl')) return null;
  return candidates.find(asset => asset.libc === libc) ?? null;
}

