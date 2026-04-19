# Plan: Dynamic Dashboard

## Context
Halaman dashboard saat ini sepenuhnya menggunakan data statis (mockup). Semua angka, grafik, dan daftar diagnostics/logs adalah hardcoded. Backend sudah memiliki semua data yang diperlukan melalui API yang ada (`GetSpeedtestHistory`, `GetSessions`, `GetHistory`). Plan ini mengganti semua data statis dengan data nyata menggunakan komputasi di frontend — **tanpa perlu menambahkan method baru di backend Go**.

---

## File yang Dimodifikasi

| File | Aksi |
|------|------|
| `frontend/src/routes/dashboard/+page.svelte` | Rewrite — tambah `<script>` block, ganti semua static data |

*Tidak ada perubahan di backend (app.go, storage.go, dll).*

---

## Data Sources (API yang sudah ada)

```typescript
import { GetSpeedtestHistory, GetSessions, GetHistory } from '../../../wailsjs/go/main/App';
import { relativeTime, fmtMbps } from '$lib/types/speedtest';
import { fmtMs, fmtLoss } from '$lib/types/diagnostics';
import { main } from '../../../wailsjs/go/models';
```

---

## Step 1 — Script Block (State + Computation)

### State Variables
```typescript
let speedtestSessions = $state<main.SpeedtestSession[]>([]);
let diagSessions      = $state<main.DiagSession[]>([]);
let historyHosts      = $state<string[]>([]);
```

### Derived Stat Cards

**Network Health** — persentase speedtest yang tidak gagal:
```typescript
let networkHealth = $derived.by(() => {
  if (speedtestSessions.length === 0) return null;
  const ok = speedtestSessions.filter(s => !s.failed).length;
  return ((ok / speedtestSessions.length) * 100).toFixed(1);
});
```

**Avg Latency** — rata-rata ping dari speedtest sessions terakhir (maks 10):
```typescript
let avgLatency = $derived.by(() => {
  const valid = speedtestSessions.filter(s => !s.failed && s.ping_ms > 0).slice(0, 10);
  if (valid.length === 0) return null;
  return Math.round(valid.reduce((sum, s) => sum + s.ping_ms, 0) / valid.length);
});
```

**Total Outages** — speedtest gagal + diag sessions dengan loss rata-rata > 50%:
```typescript
let totalOutages = $derived.by(() => {
  const stFailed = speedtestSessions.filter(s => s.failed).length;
  const diagFailed = diagSessions.filter(s => {
    if (s.hops.length === 0) return false;
    const avgLoss = s.hops.reduce((sum, h) => sum + h.loss, 0) / s.hops.length;
    return avgLoss > 50;
  }).length;
  return stFailed + diagFailed;
});
```

**Active Endpoints** — jumlah unique hosts dari riwayat diagnostics:
```typescript
let activeEndpoints = $derived(() => historyHosts.length);
```

---

## Step 2 — Connection Stability Chart

Gunakan ping_ms dari speedtest sessions untuk bar chart. Ambil maks 40 sesi, urutkan dari terlama ke terbaru.

```typescript
let chartBars = $derived.by(() => {
  const sessions = [...speedtestSessions]
    .filter(s => !s.failed && s.ping_ms > 0)
    .reverse()          // oldest first
    .slice(-40);        // last 40 entries

  if (sessions.length === 0) return [];

  const maxPing = Math.max(...sessions.map(s => s.ping_ms));
  return sessions.map(s => ({
    heightPct: Math.max(5, (s.ping_ms / maxPing) * 100),
    ping: s.ping_ms,
    time: relativeTime(s.started_at),
  }));
});

let chartMax = $derived(() =>
  chartBars.length > 0 ? Math.max(...chartBars.map(b => b.ping)) : 100
);
```

**Bar rendering** — ganti `{#each Array(40)}` dengan `{#each chartBars as bar}`.

**Y-axis labels** — ganti hardcoded `100 / 50 / 20 / 0` dengan:
```svelte
{chartMax} / {Math.round(chartMax * 0.5)} / {Math.round(chartMax * 0.2)} / 0
```

**X-axis timestamps** — ambil 5 titik dari `chartBars`:
```typescript
let xLabels = $derived.by(() => {
  if (chartBars.length < 2) return [];
  const pts = [0, 0.25, 0.5, 0.75, 1].map(f =>
    chartBars[Math.min(Math.floor(f * (chartBars.length - 1)), chartBars.length - 1)]?.time ?? ''
  );
  return pts;
});
```

**Empty state** — tampil teks di atas chart jika `chartBars.length === 0`:
```svelte
{#if chartBars.length === 0}
  <div class="absolute inset-0 flex items-center justify-center text-slate-300 text-sm font-bold">
    No speed test data yet
  </div>
{/if}
```

---

## Step 3 — Routine Diagnostics Panel

Ganti 3 item hardcoded dengan render dari `diagSessions` (maks 5 terbaru).

### Logika per sesi:
```typescript
// Untuk setiap DiagSession:
const avgLoss = session.hops.length > 0
  ? session.hops.reduce((s, h) => s + h.loss, 0) / session.hops.length
  : 0;
const isOk = avgLoss < 10 && !session.failed;

const lastHop = session.hops[session.hops.length - 1];
const latency = lastHop?.avg_ms > 0 ? `${lastHop.avg_ms} ms` : '--';
```

### Render markup per item:
```svelte
{#each diagSessions.slice(0, 5) as session}
  {@const avgLoss = session.hops.reduce((s, h) => s + h.loss, 0) / (session.hops.length || 1)}
  {@const isOk = avgLoss < 10}
  {@const lastHop = session.hops.at(-1)}

  <div class="flex items-center justify-between p-4 border {isOk ? 'border-slate-100' : 'border-red-100 bg-red-50/30'} rounded-2xl ...">
    <div class="flex items-center gap-4">
      <div class="w-3 h-3 {isOk ? 'bg-green-500 animate-pulse' : 'bg-red-500'} rounded-full ..."></div>
      <div class="flex flex-col">
        <span class="text-sm font-extrabold {isOk ? 'text-slate-800' : 'text-red-700'}">{session.host}</span>
        <span class="text-[0.65rem] font-bold {isOk ? 'text-slate-400' : 'text-red-400/80'}">
          {relativeTime(session.ended_at)} &bull; Loss: {fmtLoss(avgLoss)}
        </span>
      </div>
    </div>
    <div class="text-[0.7rem] font-bold {isOk ? 'text-slate-500 bg-slate-100/50' : 'text-red-600 bg-red-100'} px-3 py-1 rounded-lg">
      {isOk ? `~ ${lastHop?.avg_ms ?? '--'} ms` : 'Issues'}
    </div>
  </div>
{:else}
  <div class="text-center py-8 text-slate-400 text-sm font-medium">
    No diagnostics run yet
  </div>
{/each}
```

---

## Step 4 — System Logs Panel

Gabungkan speedtest + diag sessions menjadi unified timeline, urutkan newest-first, tampilkan maks 6 event.

```typescript
type LogEvent = {
  time: string;
  message: string;
  color: 'blue' | 'red' | 'green' | 'gray';
};

let systemLogs = $derived.by((): LogEvent[] => {
  const events: (LogEvent & { ts: string })[] = [];

  for (const s of speedtestSessions) {
    events.push({
      ts: s.started_at,
      time: relativeTime(s.started_at),
      message: s.failed
        ? `Speed test failed: ${s.fail_reason ?? 'unknown error'}`
        : `Speed test: ↓${fmtMbps(s.download_mbps)} ↑${fmtMbps(s.upload_mbps)} Mbps — ${s.ping_ms}ms ping`,
      color: s.failed ? 'red' : 'blue',
    });
  }

  for (const d of diagSessions) {
    const avgLoss = d.hops.reduce((sum, h) => sum + h.loss, 0) / (d.hops.length || 1);
    const hasIssues = avgLoss > 20;
    events.push({
      ts: d.ended_at,
      time: relativeTime(d.ended_at),
      message: hasIssues
        ? `Diagnostics to ${d.host} — high packet loss (${fmtLoss(avgLoss)})`
        : `Diagnostics to ${d.host} — completed`,
      color: hasIssues ? 'red' : 'green',
    });
  }

  return events
    .sort((a, b) => new Date(b.ts).getTime() - new Date(a.ts).getTime())
    .slice(0, 6);
});
```

**Color mapping untuk dot:**
```
blue  → bg-blue-500
red   → bg-red-500
green → bg-green-500
gray  → bg-slate-300
```

**Render:**
```svelte
{#each systemLogs as log}
  <div class="relative z-10 flex flex-col gap-1">
    <div class="absolute -left-[14.5px] top-1.5 w-2 h-2 rounded-full bg-{log.color}-500 ring-4 ring-white"></div>
    <span class="text-[0.65rem] font-bold text-slate-400">{log.time}</span>
    <span class="text-xs font-bold {log.color === 'red' ? 'text-red-600' : 'text-slate-800'}">{log.message}</span>
  </div>
{:else}
  <p class="text-slate-400 text-sm font-medium">No events yet</p>
{/each}
```

---

## Step 5 — onMount: Load Data

```typescript
onMount(async () => {
  const [st, diag, hosts] = await Promise.all([
    GetSpeedtestHistory(),
    GetSessions(),
    GetHistory(),
  ]);
  speedtestSessions = st ?? [];
  diagSessions = diag ?? [];
  historyHosts = hosts ?? [];
});
```

---

## Urutan Implementasi

1. Tambah `<script lang="ts">` block ke `dashboard/+page.svelte` dengan semua state + derived variables
2. Ganti 4 stat card hardcoded → gunakan derived values (tampilkan `--` jika null/no data)
3. Ganti chart bars `Array(40)` → `chartBars`, update Y-axis dan X-axis labels
4. Ganti 3 item Routine Diagnostics hardcoded → `{#each diagSessions.slice(0, 5)}`
5. Ganti 4 event System Logs hardcoded → `{#each systemLogs}`
6. `bun run check` — verifikasi 0 errors
7. `wails build` — verifikasi build sukses

---

## Verifikasi

| Test | Ekspektasi |
|------|-----------|
| Dashboard dibuka tanpa data | Semua stat card tampilkan `--`, chart kosong, "No diagnostics run yet", "No events yet" |
| Setelah 1x speedtest | Avg Latency dan Network Health terisi, 1 bar di chart, 1 log entry |
| Setelah 1x diagnostics | Routine Diagnostics tampil 1 item, System Logs tambah 1 entry |
| Session dengan outage/gagal | Total Outages bertambah, log entry berwarna merah |
| `bun run check` | 0 errors, 0 warnings |
| `wails build` | Build sukses |
