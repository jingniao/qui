import { authState } from './auth';
function components(date: Date, timezone: string) {
  const parts = new Intl.DateTimeFormat('en-CA', { timeZone: timezone, year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', hourCycle: 'h23' }).formatToParts(date);
  return Object.fromEntries(parts.filter(part => part.type !== 'literal').map(part => [part.type, part.value]));
}
export function localInput(value: string) {
  if (!value) return '';
  const date = new Date(value); if (Number.isNaN(date.getTime())) return '';
  const p = components(date, authState.timezone);
  return `${p.year}-${p.month}-${p.day}T${p.hour}:${p.minute}`;
}
export function formatMinuteParts(value: string): { date: string; time: string } {
  if (!value) return { date: '', time: '' };
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return { date: '', time: '' };
  const parts = new Intl.DateTimeFormat('zh-CN', {
    timeZone: authState.timezone, year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', hourCycle: 'h23'
  }).formatToParts(date);
  const part = (type: string) => parts.find(item => item.type === type)?.value ?? '';
  return { date: `${part('year')}/${part('month')}/${part('day')}`, time: `${part('hour')}:${part('minute')}` };
}

/** Panel-timezone date and HH:mm without seconds, for narrow cells. */
export function formatMinute(value: string): string {
  const { date, time } = formatMinuteParts(value);
  return date ? `${date} ${time}` : '';
}

/** Panel-timezone "10月30日 周五" label (browser timezone must not leak into headers). */
export function panelDateLabel(date: Date): string {
  const parts = new Intl.DateTimeFormat('zh-CN', {
    timeZone: authState.timezone, month: 'numeric', day: 'numeric', weekday: 'short'
  }).formatToParts(date);
  const part = (type: string) => parts.find(item => item.type === type)?.value ?? '';
  return `${part('month')}月${part('day')}日 ${part('weekday')}`;
}
export function utcInput(value: string) {
  if (!value) return '';
  const desired = new Date(value + ':00Z').getTime();
  if (!Number.isFinite(desired) || new Date(desired).toISOString().slice(0, 16) !== value) throw new Error('到期时间无效');
  let instant = desired;
  for (let step = 0; step < 4; step++) {
    const p = components(new Date(instant), authState.timezone);
    const wall = Date.UTC(Number(p.year), Number(p.month) - 1, Number(p.day), Number(p.hour), Number(p.minute), Number(p.second));
    if (wall === desired) return new Date(instant).toISOString();
    instant += desired - wall;
  }
  throw new Error('所选时区中不存在该时间，请调整到期时间');
}

