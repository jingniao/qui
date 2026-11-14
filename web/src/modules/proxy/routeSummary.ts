import type { RouteSummary } from './api';

export function routeSummaryLabel(summary: RouteSummary): string {
  return { direct: '直连', relay: '中转', custom: '自定义出站', tunnel: '隧道转发' }[summary.type];
}

export function routeSummaryPath(summary: RouteSummary): string {
  return summary.type === 'relay' || summary.type === 'tunnel' ? summary.path.join(' → ') : '';
}

export function routeSummaryLink(summary: RouteSummary) {
  if (summary.type === 'relay' && summary.schemeId) {
    return { path: '/proxy/relay', query: { schemeId: summary.schemeId } };
  }
}
