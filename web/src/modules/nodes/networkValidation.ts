export function ipv4(value: string) {
  return value.split('.').length === 4 && value.split('.').every(part => /^\d{1,3}$/.test(part) && Number(part) <= 255);
}

export function ipv6(value: string) {
  try {
    return value.includes(':') && new URL(`http://[${value}]/`).hostname.startsWith('[');
  } catch {
    return false;
  }
}
