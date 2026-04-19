export type SpeedtestPhase = 'ping' | 'download' | 'upload' | 'done' | 'failed' | 'idle';

export interface SpeedtestUpdate {
  phase: SpeedtestPhase;
  speed: number;
  ping: number;
  jitter: number;
  loss: number;
  download: number;
  upload: number;
  error?: string;
}

export interface SpeedtestSession {
  id: string;
  started_at: string;
  download_mbps: number;
  upload_mbps: number;
  ping_ms: number;
  jitter_ms: number;
  loss_pct: number;
  server: string;
  failed: boolean;
  fail_reason?: string;
}

export function fmtMbps(v: number): string {
  if (v < 0) return '--';
  return v.toFixed(1);
}

export function fmtMs(v: number): string {
  if (v < 0) return '--';
  return `${v}`;
}

export function relativeTime(iso: string): string {
  const diff = Date.now() - new Date(iso).getTime();
  const mins = Math.floor(diff / 60000);
  if (mins < 1) return 'just now';
  if (mins < 60) return `${mins} minute${mins === 1 ? '' : 's'} ago`;
  const hrs = Math.floor(mins / 60);
  if (hrs < 24) return `${hrs} hour${hrs === 1 ? '' : 's'} ago`;
  return `${Math.floor(hrs / 24)} days ago`;
}
