<script lang="ts">
  import { onMount } from 'svelte';
  import { main } from '../../../wailsjs/go/models';
  import { GetSpeedtestHistory, GetSessions, GetHistory } from '../../../wailsjs/go/main/App';
  import { fmtMbps, relativeTime } from '$lib/types/speedtest';
  import { fmtLoss } from '$lib/types/diagnostics';

  let speedtestSessions = $state<main.SpeedtestSession[]>([]);
  let diagSessions      = $state<main.DiagSession[]>([]);
  let historyHosts      = $state<string[]>([]);

  // ── Stat Cards ──────────────────────────────────────────────────────────────

  let networkHealth = $derived.by(() => {
    if (speedtestSessions.length === 0) return null;
    const ok = speedtestSessions.filter(s => !s.failed).length;
    return ((ok / speedtestSessions.length) * 100).toFixed(1);
  });

  let avgLatency = $derived.by(() => {
    const valid = speedtestSessions.filter(s => !s.failed && s.ping_ms > 0).slice(0, 10);
    if (valid.length === 0) return null;
    return Math.round(valid.reduce((sum, s) => sum + s.ping_ms, 0) / valid.length);
  });

  let totalOutages = $derived.by(() => {
    const stFailed = speedtestSessions.filter(s => s.failed).length;
    const diagFailed = diagSessions.filter(s => {
      const lastHop = s.hops.at(-1);
      return lastHop ? lastHop.loss > 50 : false;
    }).length;
    return stFailed + diagFailed;
  });

  let activeEndpoints = $derived(historyHosts.length);

  // ── Chart ────────────────────────────────────────────────────────────────────

  type ChartRange = '1H' | '24H' | '7D';
  type ChartBar = { heightPct: number; ping: number; time: string };

  let chartRange = $state<ChartRange>('24H');

  let chartBars = $derived.by((): ChartBar[] => {
    const now = Date.now();
    const cutoffMs: Record<ChartRange, number> = {
      '1H':  1 * 60 * 60 * 1000,
      '24H': 24 * 60 * 60 * 1000,
      '7D':  7 * 24 * 60 * 60 * 1000,
    };
    const cutoff = now - cutoffMs[chartRange];
    const sessions = [...speedtestSessions]
      .filter(s => !s.failed && s.ping_ms > 0 && new Date(s.started_at).getTime() >= cutoff)
      .reverse()
      .slice(-40);
    if (sessions.length === 0) return [];
    const maxPing = Math.max(...sessions.map(s => s.ping_ms));
    return sessions.map(s => ({
      heightPct: Math.max(5, (s.ping_ms / maxPing) * 100),
      ping: s.ping_ms,
      time: relativeTime(s.started_at),
    }));
  });

  let chartMax  = $derived(chartBars.length > 0 ? Math.max(...chartBars.map(b => b.ping)) : 100);
  let xLabels   = $derived.by(() => {
    if (chartBars.length < 2) return ['', '', '', '', 'NOW'];
    const pts = [0, 0.25, 0.5, 0.75, 1].map(f =>
      chartBars[Math.min(Math.floor(f * (chartBars.length - 1)), chartBars.length - 1)]?.time ?? ''
    );
    pts[pts.length - 1] = 'NOW';
    return pts;
  });

  // ── System Logs ──────────────────────────────────────────────────────────────

  type LogColor = 'blue' | 'red' | 'green' | 'gray';
  type LogEvent = { ts: string; time: string; message: string; color: LogColor };

  let systemLogs = $derived.by((): LogEvent[] => {
    const events: LogEvent[] = [];

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
      const lastHop = d.hops.at(-1);
      const lastLoss = lastHop?.loss ?? 0;
      const hasIssues = lastLoss > 20;
      events.push({
        ts: d.ended_at,
        time: relativeTime(d.ended_at),
        message: hasIssues
          ? `Diagnostics to ${d.host} — high packet loss (${fmtLoss(lastLoss)})`
          : `Diagnostics to ${d.host} — completed`,
        color: hasIssues ? 'red' : 'green',
      });
    }

    return events
      .sort((a, b) => new Date(b.ts).getTime() - new Date(a.ts).getTime())
      .slice(0, 6);
  });

  const dotColor: Record<LogColor, string> = {
    blue:  'bg-blue-500',
    red:   'bg-red-500',
    green: 'bg-green-500',
    gray:  'bg-slate-300',
  };

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
</script>

<svelte:head>
	<title>Dashboard - NetMonit</title>
</svelte:head>

<div class="h-full w-full max-w-350 mx-auto flex flex-col gap-8 pb-10 select-none mb-8">

	<!-- Top Level: Quick Stats -->
	<div class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-4 gap-6">

		<!-- Network Health -->
		<div class="bg-white rounded-3xl p-6 border border-slate-100 shadow-[0_2px_15px_rgba(0,0,0,0.015)] flex flex-col relative overflow-hidden transition-transform hover:-translate-y-1">
			<div class="absolute top-0 right-0 p-6 opacity-20 text-blue-600">
				<span class="material-symbols-outlined text-[60px] -m-2">verified_user</span>
			</div>
			<div class="w-10 h-10 rounded-2xl bg-blue-50 text-blue-600 flex items-center justify-center mb-4">
				<span class="material-symbols-outlined text-[20px]">monitor_heart</span>
			</div>
			<span class="text-[0.65rem] font-extrabold tracking-widest text-slate-400 mb-1 z-10">NETWORK HEALTH</span>
			<div class="flex items-end gap-2 z-10">
				<span class="text-3xl font-black text-slate-800 leading-none">{networkHealth ?? '--'}</span>
				<span class="text-sm font-bold text-blue-600 pb-0.5">{networkHealth !== null ? '%' : ''}</span>
			</div>
		</div>

		<!-- Avg Latency -->
		<div class="bg-white rounded-3xl p-6 border border-slate-100 shadow-[0_2px_15px_rgba(0,0,0,0.015)] flex flex-col relative overflow-hidden transition-transform hover:-translate-y-1">
			<div class="w-10 h-10 rounded-2xl bg-orange-50 text-orange-500 flex items-center justify-center mb-4">
				<span class="material-symbols-outlined text-[20px]">speed</span>
			</div>
			<span class="text-[0.65rem] font-extrabold tracking-widest text-slate-400 mb-1 z-10">AVG LATENCY</span>
			<div class="flex items-end gap-2 z-10">
				<span class="text-3xl font-black text-slate-800 leading-none">{avgLatency ?? '--'}</span>
				<span class="text-sm font-bold text-slate-400 pb-0.5">{avgLatency !== null ? 'ms' : ''}</span>
			</div>
		</div>

		<!-- Total Outages -->
		<div class="bg-white rounded-3xl p-6 border border-slate-100 shadow-[0_2px_15px_rgba(0,0,0,0.015)] flex flex-col relative overflow-hidden transition-transform hover:-translate-y-1">
			<div class="w-10 h-10 rounded-2xl bg-red-50 text-red-500 flex items-center justify-center mb-4">
				<span class="material-symbols-outlined text-[20px] font-bold">warning</span>
			</div>
			<span class="text-[0.65rem] font-extrabold tracking-widest text-slate-400 mb-1 z-10">TOTAL OUTAGES</span>
			<div class="flex items-end gap-2 z-10">
				<span class="text-3xl font-black text-slate-800 leading-none">{totalOutages}</span>
				<span class="text-sm font-bold text-slate-400 pb-0.5">events</span>
			</div>
		</div>

		<!-- Active Endpoints -->
		<div class="bg-white rounded-3xl p-6 border border-slate-100 shadow-[0_2px_15px_rgba(0,0,0,0.015)] flex flex-col relative overflow-hidden transition-transform hover:-translate-y-1">
			<div class="w-10 h-10 rounded-2xl bg-teal-50 text-teal-600 flex items-center justify-center mb-4">
				<span class="material-symbols-outlined text-[20px] font-bold">route</span>
			</div>
			<span class="text-[0.65rem] font-extrabold tracking-widest text-slate-400 mb-1 z-10">ACTIVE ENDPOINTS</span>
			<div class="flex items-end gap-2 z-10">
				<span class="text-3xl font-black text-slate-800 leading-none">{activeEndpoints}</span>
				<span class="text-sm font-bold text-slate-400 pb-0.5">hosts</span>
			</div>
		</div>
	</div>

	<!-- Mid Level: Connection Stability Chart -->
	<div class="bg-white rounded-4xl border border-slate-100 shadow-[0_2px_15px_rgba(0,0,0,0.015)] p-8">
		<div class="flex items-center justify-between mb-8">
			<div class="flex flex-col">
				<h3 class="text-sm font-extrabold text-slate-800">Connection Stability</h3>
				<span class="text-[0.7rem] text-slate-400 font-medium">
					Ping latency across speed test sessions ({chartBars.length} data points).
				</span>
			</div>
			<!-- Filter Pills placeholder -->
			<div class="flex items-center gap-2">
				{#each (['1H', '24H', '7D'] as const) as r}
					<button
						onclick={() => { chartRange = r; }}
						class="px-4 py-1.5 rounded-full text-xs font-bold transition-colors
						       {chartRange === r ? 'bg-blue-600 text-white shadow-sm' : 'bg-slate-100 text-slate-500 hover:bg-slate-200'}"
					>{r}</button>
				{/each}
			</div>
		</div>

		<!-- Bar Chart -->
		<div class="w-full h-56 flex items-end justify-between gap-1 sm:gap-2 px-2 border-b-2 border-slate-100/60 pb-2 relative">

			<!-- Y-axis labels -->
			<div class="absolute left-0 top-0 bottom-0 w-8 flex flex-col justify-between text-[0.65rem] font-bold text-slate-300 pointer-events-none -ml-4">
				<span>{chartMax}</span>
				<span>{Math.round(chartMax * 0.5)}</span>
				<span>{Math.round(chartMax * 0.2)}</span>
				<span>0</span>
			</div>

			<!-- Empty state -->
			{#if chartBars.length === 0}
				<div class="absolute inset-0 flex items-center justify-center text-slate-300 text-sm font-bold">
					No speed test data yet — run a speed test to populate this chart
				</div>
			{/if}

			<!-- Dynamic bars from real sessions -->
			{#each chartBars as bar}
				<div class="w-full max-w-5 rounded-t-sm group relative flex items-end justify-center h-full">
					<div
						class="w-full bg-[#cbd5e1] group-hover:bg-blue-500 transition-colors duration-300 rounded-t-md"
						style="height: {bar.heightPct}%"
					></div>
					<div class="absolute -top-8 bg-slate-800 text-white text-[0.6rem] font-bold px-2 py-1 rounded opacity-0 group-hover:opacity-100 transition-opacity pointer-events-none whitespace-nowrap z-10">
						{bar.ping} ms
					</div>
				</div>
			{/each}
		</div>

		<!-- X-axis timestamps -->
		<div class="flex justify-between items-center text-[0.60rem] font-bold text-slate-400 uppercase tracking-widest mt-3 px-2">
			{#each xLabels as label}
				<span>{label}</span>
			{/each}
		</div>
	</div>

	<!-- Bottom Level: Diagnostics & Logs -->
	<div class="grid grid-cols-1 lg:grid-cols-[1fr_400px] gap-6 flex-1 mb-8">

		<!-- Recent Diagnostics -->
		<div class="bg-white rounded-4xl border border-slate-100 shadow-[0_2px_15px_rgba(0,0,0,0.015)] p-8 h-fit mb-8">
			<div class="flex items-center justify-between mb-6">
				<h3 class="text-[0.65rem] tracking-[0.15em] font-black text-slate-500">RECENT DIAGNOSTICS</h3>
				<a href="/diagnostics" class="text-[0.7rem] font-bold text-blue-600 cursor-pointer hover:underline">Run New</a>
			</div>

			<div class="space-y-4">
				{#if diagSessions.length === 0}
					<div class="text-center py-8 text-slate-400 text-sm font-medium">
						No diagnostics run yet
					</div>
				{:else}
					{#each diagSessions.slice(0, 5) as session (session.id)}
						{@const lastHop = session.hops.at(-1)}
						{@const lastLoss = lastHop?.loss ?? 0}
						{@const isOk = lastLoss < 10}
						<div class="flex items-center justify-between p-4 border {isOk ? 'border-slate-100' : 'border-red-100 bg-red-50/30'} rounded-2xl hover:shadow-sm transition-all cursor-pointer">
							<div class="flex items-center gap-4">
								<div class="w-3 h-3 {isOk ? 'bg-green-500 animate-pulse' : 'bg-red-500'} rounded-full border-2 border-white shadow-[0_0_0_2px_{isOk ? 'rgba(34,197,94,0.2)' : 'rgba(239,68,68,0.2)'}]"></div>
								<div class="flex flex-col">
									<span class="text-sm font-extrabold {isOk ? 'text-slate-800' : 'text-red-700'}">{session.host}</span>
									<span class="text-[0.65rem] font-bold {isOk ? 'text-slate-400' : 'text-red-400/80'}">
										{relativeTime(session.ended_at)} &bull; Loss: {fmtLoss(lastLoss)}
									</span>
								</div>
							</div>
							<div class="text-[0.7rem] font-bold {isOk ? 'text-slate-500 bg-slate-100/50' : 'text-red-600 bg-red-100'} px-3 py-1 rounded-lg shrink-0">
								{#if isOk}
									{lastHop && lastHop.avg_ms > 0 ? `~ ${lastHop.avg_ms} ms` : '-- ms'}
								{:else}
									Issues
								{/if}
							</div>
						</div>
					{/each}
				{/if}
			</div>
		</div>

		<!-- System Logs -->
		<div class="bg-linear-to-b from-[#f8fafc] to-white rounded-4xl border border-slate-200/60 shadow-sm p-8 relative overflow-hidden mb-8">
			<h3 class="text-[0.65rem] tracking-[0.15em] font-black text-slate-500 mb-6">SYSTEM LOGS</h3>

			<div class="relative pl-4 space-y-6">
				<!-- Timeline line -->
				<div class="absolute left-1.25 top-2 bottom-2 w-0.5 bg-slate-200 rounded-full"></div>

				{#if systemLogs.length === 0}
					<p class="text-slate-400 text-sm font-medium">No events yet</p>
				{:else}
					{#each systemLogs as log (log.ts + log.message)}
						<div class="relative z-10 flex flex-col gap-1">
							<div class="absolute -left-[14.5px] top-1.5 w-2 h-2 rounded-full {dotColor[log.color]} ring-4 ring-white"></div>
							<span class="text-[0.65rem] font-bold text-slate-400">{log.time}</span>
							<span class="text-xs font-bold {log.color === 'red' ? 'text-red-600' : 'text-slate-800'}">{log.message}</span>
						</div>
					{/each}
				{/if}
			</div>

			<div class="mt-6 text-center">
				<a href="/history" class="text-[0.7rem] font-extrabold text-blue-600 hover:underline cursor-pointer">
					View full log &rarr;
				</a>
			</div>
		</div>
	</div>
</div>
