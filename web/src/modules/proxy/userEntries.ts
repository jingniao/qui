export function isUserEntry(inbound: { internalRelay?: boolean }): boolean {
  return !inbound.internalRelay;
}
