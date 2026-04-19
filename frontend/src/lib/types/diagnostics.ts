export interface HopUpdate {
  nr: number;
  host: string;
  loss: number;
  sent: number;
  recv: number;
  best: number;  // -1 if no reply yet
  avg: number;   // -1 if no reply yet
  worst: number; // -1 if no reply yet
  last: number;  // -1 if last probe timed out
}

export interface DiagnosticsUpdate {
  hops: HopUpdate[];
  done: boolean;
  error?: string;
}

export function fmtMs(ms: number): string {
  return ms < 0 ? '-' : `${ms}ms`;
}

export function fmtLoss(loss: number): string {
  return `${loss.toFixed(2)}%`;
}
