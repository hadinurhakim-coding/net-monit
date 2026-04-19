<script lang="ts">
  import { onMount } from 'svelte';
  import { main } from '../../../wailsjs/go/models';
  import { GetSpeedtestHistory, GetSessions } from '../../../wailsjs/go/main/App';
  import { fmtMbps, relativeTime } from '$lib/types/speedtest';
  import { fmtLoss } from '$lib/types/diagnostics';

  type HistoryEntry =
    | { kind: 'speedtest'; data: main.SpeedtestSession; ts: number }
    | { kind: 'diag';      data: main.DiagSession;      ts: number };

  let allEntries  = $state<HistoryEntry[]>([]);
  let loading     = $state(true);
  let query       = $state('');
  let typeFilter  = $state<'all' | 'speedtest' | 'diag'>('all');
  let page        = $state(1);
  const PER_PAGE  = 10;

  let filtered = $derived.by(() => {
    const q = query.toLowerCase();
    return allEntries.filter(e => {
      if (typeFilter !== 'all' && e.kind !== typeFilter) return false;
      if (q === '') return true;
      if (e.kind === 'speedtest') return e.data.server.toLowerCase().includes(q);
      return e.data.host.toLowerCase().includes(q);
    });
  });

  let totalPages = $derived(Math.max(1, Math.ceil(filtered.length / PER_PAGE)));
  let paginated  = $derived(filtered.slice((page - 1) * PER_PAGE, page * PER_PAGE));

  $effect(() => {
    // Reset to page 1 when filter changes
    filtered; page = 1;
  });

  function stStatus(s: main.SpeedtestSession): 'ok' | 'warn' | 'fail' {
    if (s.failed) return 'fail';
    if (s.jitter_ms > 20 || s.loss_pct > 5) return 'warn';
    return 'ok';
  }

  function diagStatus(d: main.DiagSession): 'ok' | 'fail' {
    const lastHop = d.hops.at(-1);
    return (lastHop && lastHop.loss > 10) ? 'fail' : 'ok';
  }

  onMount(async () => {
    const [st, diag] = await Promise.all([GetSpeedtestHistory(), GetSessions()]);
    const entries: HistoryEntry[] = [
      ...(st ?? []).map(d => ({ kind: 'speedtest' as const, data: d, ts: new Date(d.started_at).getTime() })),
      ...(diag ?? []).map(d => ({ kind: 'diag' as const,      data: d, ts: new Date(d.ended_at).getTime() })),
    ];
    allEntries = entries.sort((a, b) => b.ts - a.ts);
    loading = false;
  });
</script>

<svelte:head>
  <title>History - NetMonit</title>
</svelte:head>

<div class="h-full w-full max-w-350 mx-auto flex flex-col gap-6 pb-10 select-none mb-10">

  <!-- Top Controls -->
  <div class="bg-white rounded-3xl p-6 border border-slate-100 shadow-[0_2px_15px_rgba(0,0,0,0.015)] flex flex-col sm:flex-row sm:items-center justify-between gap-4">
    <div class="flex flex-col">
      <h2 class="text-xl font-bold text-slate-800">Diagnostic Logs</h2>
      <span class="text-xs text-slate-400 font-medium mt-0.5">
        {filtered.length} record{filtered.length !== 1 ? 's' : ''} found.
      </span>
    </div>

    <div class="flex items-center gap-4">
      <!-- Search -->
      <div class="relative w-64">
        <div class="absolute inset-y-0 left-0 flex items-center pl-4 pointer-events-none text-slate-400">
          <span class="material-symbols-outlined text-[18px]">search</span>
        </div>
        <input
          type="text"
          bind:value={query}
          class="bg-slate-50 border border-slate-200 text-slate-600 text-sm rounded-xl focus:ring-blue-500 focus:border-blue-500 block w-full pl-10 p-2.5 font-medium outline-none transition-all placeholder:text-slate-400"
          placeholder="Search host or server..."
        />
      </div>

      <!-- Type Filter -->
      <div class="relative">
        <select
          bind:value={typeFilter}
          class="appearance-none bg-slate-50 border border-slate-200 text-slate-600 text-sm rounded-xl focus:ring-blue-500 focus:border-blue-500 block w-40 p-2.5 pr-8 font-medium outline-none cursor-pointer transition-all"
        >
          <option value="all">All Types</option>
          <option value="diag">Diagnostics</option>
          <option value="speedtest">Speed Test</option>
        </select>
        <div class="absolute inset-y-0 right-0 flex items-center pr-3 pointer-events-none text-slate-400">
          <span class="material-symbols-outlined text-[16px]">expand_more</span>
        </div>
      </div>
    </div>
  </div>

  <!-- List -->
  <div class="bg-white/50 rounded-3xl p-1 border border-transparent flex-1 flex flex-col gap-3">

    <!-- Header -->
    <div class="px-6 py-3 grid grid-cols-[1fr_1.5fr_1fr_120px_40px] items-center text-[0.65rem] font-black tracking-[0.15em] text-slate-400 uppercase gap-4">
      <span>Entry Type</span>
      <span>Target / End Point</span>
      <span>Summary</span>
      <span class="text-center">Status</span>
      <span></span>
    </div>

    {#if loading}
      <div class="flex items-center justify-center py-16 text-slate-400 gap-2">
        <span class="material-symbols-outlined animate-spin text-[20px]">progress_activity</span>
        <span class="text-sm font-medium">Loading history...</span>
      </div>

    {:else if paginated.length === 0}
      <div class="flex flex-col items-center justify-center py-16 gap-2 text-slate-400">
        <span class="material-symbols-outlined text-[40px]">history</span>
        <span class="text-sm font-medium">No records found</span>
      </div>

    {:else}
      {#each paginated as entry (entry.ts + entry.kind)}

        {#if entry.kind === 'speedtest'}
          {@const s = entry.data}
          {@const status = stStatus(s)}
          <div class="bg-white rounded-2xl p-5 border {status === 'fail' ? 'border-red-50' : 'border-slate-100'} shadow-[0_2px_10px_rgba(0,0,0,0.015)] hover:shadow-md hover:border-slate-200 transition-all cursor-pointer group grid grid-cols-[1fr_1.5fr_1fr_120px_40px] items-center gap-4 relative overflow-hidden">
            {#if status === 'fail'}<div class="absolute top-0 right-0 bottom-0 w-1 bg-red-500"></div>{/if}

            <!-- Entry Type -->
            <div class="flex items-center gap-4">
              <div class="w-10 h-10 rounded-xl {status === 'fail' ? 'bg-red-50 text-red-500' : status === 'warn' ? 'bg-orange-50 text-orange-500' : 'bg-blue-50 text-blue-600'} flex items-center justify-center shrink-0">
                <span class="material-symbols-outlined text-[20px]">{status === 'fail' ? 'warning' : 'speed'}</span>
              </div>
              <div class="flex flex-col">
                <span class="text-sm font-extrabold text-slate-800">Speed Test</span>
                <span class="text-[0.65rem] font-bold text-slate-400 mt-0.5">{relativeTime(s.started_at)}</span>
              </div>
            </div>

            <!-- Target -->
            <div class="flex flex-col">
              <span class="text-sm font-extrabold text-slate-700 truncate">{s.server || '—'}</span>
            </div>

            <!-- Summary -->
            <div class="flex flex-col">
              {#if s.failed}
                <span class="text-xs font-bold text-red-600 border-l-2 border-red-500 pl-2">{s.fail_reason ?? 'Failed'}</span>
              {:else}
                <span class="text-xs font-bold text-slate-600 border-l-2 border-blue-500 pl-2">↓{fmtMbps(s.download_mbps)} ↑{fmtMbps(s.upload_mbps)} Mbps</span>
                <span class="text-[0.65rem] font-bold text-slate-400 pl-2.5 mt-0.5">Ping {s.ping_ms}ms · Jitter {s.jitter_ms}ms</span>
              {/if}
            </div>

            <!-- Status -->
            <div class="flex justify-center">
              {#if status === 'fail'}
                <div class="px-3 py-1 bg-red-50 text-red-600 text-[0.65rem] font-black tracking-widest rounded-lg flex items-center gap-1.5 uppercase border border-red-100">
                  <span class="w-1.5 h-1.5 bg-red-500 rounded-full"></span> Failed
                </div>
              {:else if status === 'warn'}
                <div class="px-3 py-1 bg-yellow-50 text-yellow-700 text-[0.65rem] font-black tracking-widest rounded-lg flex items-center gap-1.5 uppercase border border-yellow-100">
                  Suboptimal
                </div>
              {:else}
                <div class="px-3 py-1 bg-green-50 text-green-600 text-[0.65rem] font-black tracking-widest rounded-lg flex items-center gap-1.5 uppercase border border-green-100">
                  <span class="w-1.5 h-1.5 bg-green-500 rounded-full"></span> OK
                </div>
              {/if}
            </div>

            <div class="flex justify-end text-slate-300 group-hover:text-blue-500 transition-colors">
              <span class="material-symbols-outlined">chevron_right</span>
            </div>
          </div>

        {:else}
          {@const d = entry.data}
          {@const lastHop = d.hops.at(-1)}
          {@const status = diagStatus(d)}
          <div class="bg-white rounded-2xl p-5 border {status === 'fail' ? 'border-red-50' : 'border-slate-100'} shadow-[0_2px_10px_rgba(0,0,0,0.015)] hover:shadow-md transition-all cursor-pointer group grid grid-cols-[1fr_1.5fr_1fr_120px_40px] items-center gap-4 relative overflow-hidden">
            {#if status === 'fail'}<div class="absolute top-0 right-0 bottom-0 w-1 bg-red-500"></div>{/if}

            <!-- Entry Type -->
            <div class="flex items-center gap-4">
              <div class="w-10 h-10 rounded-xl {status === 'fail' ? 'bg-red-50 text-red-500' : 'bg-purple-50 text-purple-600'} flex items-center justify-center shrink-0">
                <span class="material-symbols-outlined text-[20px]">{status === 'fail' ? 'warning' : 'science'}</span>
              </div>
              <div class="flex flex-col">
                <span class="text-sm font-extrabold text-slate-800">Diagnostics</span>
                <span class="text-[0.65rem] font-bold text-slate-400 mt-0.5">{relativeTime(d.ended_at)}</span>
              </div>
            </div>

            <!-- Target -->
            <div class="flex flex-col">
              <span class="text-sm font-extrabold text-slate-700 truncate">{d.host}</span>
              <span class="text-[0.65rem] font-bold text-slate-400 mt-0.5">{d.hops.length} hops</span>
            </div>

            <!-- Summary -->
            <div class="flex flex-col">
              {#if lastHop}
                <span class="text-xs font-bold {status === 'fail' ? 'text-red-600 border-red-500' : 'text-slate-600 border-purple-500'} border-l-2 pl-2">
                  Loss: {fmtLoss(lastHop.loss)}
                </span>
                <span class="text-[0.65rem] font-bold text-slate-400 pl-2.5 mt-0.5">
                  Avg: {lastHop.avg_ms >= 0 ? lastHop.avg_ms + 'ms' : '—'}
                </span>
              {:else}
                <span class="text-xs font-bold text-slate-400 border-l-2 border-slate-300 pl-2">No hops</span>
              {/if}
            </div>

            <!-- Status -->
            <div class="flex justify-center">
              {#if status === 'fail'}
                <div class="px-3 py-1 bg-red-50 text-red-600 text-[0.65rem] font-black tracking-widest rounded-lg flex items-center gap-1.5 uppercase border border-red-100">
                  <span class="w-1.5 h-1.5 bg-red-500 rounded-full"></span> Issues
                </div>
              {:else}
                <div class="px-3 py-1 bg-green-50 text-green-600 text-[0.65rem] font-black tracking-widest rounded-lg flex items-center gap-1.5 uppercase border border-green-100">
                  <span class="w-1.5 h-1.5 bg-green-500 rounded-full"></span> OK
                </div>
              {/if}
            </div>

            <div class="flex justify-end text-slate-300 group-hover:text-blue-500 transition-colors">
              <span class="material-symbols-outlined">chevron_right</span>
            </div>
          </div>
        {/if}

      {/each}
    {/if}
  </div>

  <!-- Pagination -->
  {#if totalPages > 1}
    <div class="flex justify-center mt-4 mb-8">
      <div class="inline-flex items-center bg-white border border-slate-200 rounded-xl p-1 shadow-sm gap-1">
        <button
          onclick={() => { if (page > 1) page--; }}
          disabled={page === 1}
          class="px-3 py-1.5 flex items-center justify-center text-slate-400 hover:text-slate-700 rounded-lg hover:bg-slate-50 transition-colors disabled:opacity-30 disabled:cursor-not-allowed"
        >
          <span class="material-symbols-outlined text-[18px]">chevron_left</span>
        </button>

        {#each Array.from({ length: totalPages }, (_, i) => i + 1) as p}
          {#if p === 1 || p === totalPages || Math.abs(p - page) <= 1}
            <button
              onclick={() => { page = p; }}
              class="w-8 h-8 flex items-center justify-center text-sm font-extrabold rounded-lg transition-colors
                     {p === page ? 'text-white bg-blue-600 shadow-sm' : 'text-slate-600 hover:bg-slate-50'}"
            >{p}</button>
          {:else if Math.abs(p - page) === 2}
            <div class="w-8 h-8 flex items-center justify-center text-sm font-black text-slate-300">…</div>
          {/if}
        {/each}

        <button
          onclick={() => { if (page < totalPages) page++; }}
          disabled={page === totalPages}
          class="px-3 py-1.5 flex items-center justify-center text-slate-400 hover:text-slate-700 rounded-lg hover:bg-slate-50 transition-colors disabled:opacity-30 disabled:cursor-not-allowed"
        >
          <span class="material-symbols-outlined text-[18px]">chevron_right</span>
        </button>
      </div>
    </div>
  {/if}

</div>
