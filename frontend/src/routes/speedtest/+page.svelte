<script lang="ts">
  import { onMount } from 'svelte';
  import MapBlob from '$lib/components/MapBlob.svelte';
  import ServerDialog from '$lib/components/ServerDialog.svelte';
  import type { SpeedtestUpdate } from '$lib/types/speedtest';
  import { fmtMbps, relativeTime } from '$lib/types/speedtest';
  import { main } from '../../../wailsjs/go/models';
  import { StartSpeedtest, StopSpeedtest, GetSpeedtestHistory, GetNetworkInfo, GetServers } from '../../../wailsjs/go/main/App';
  import { EventsOn } from '../../../wailsjs/runtime/runtime';

  type Phase = SpeedtestUpdate['phase'];

  let phase = $state<Phase>('idle');
  let displaySpeed = $state(0);
  let dlSpeed = $state(0);
  let ulSpeed = $state(0);
  let maxDlSpeed = $state(0);
  let maxUlSpeed = $state(0);
  let bestPing = $state(0);
  let pingVal = $state(0);
  let jitterVal = $state(0);
  let lossVal = $state(0);
  let recentTests = $state<main.SpeedtestSession[]>([]);
  let networkInfo = $state<main.NetworkInfo>({ provider: '...', ip: '...', city: '...', country: '', lat: 0, lon: 0 });
  let lastTestedAt = $state('');
  let errorMsg = $state('');
  let serverList     = $state<main.SpeedServer[]>([]);
  let selectedServer = $state<main.SpeedServer>({ id: 'cloudflare-auto', name: 'Cloudflare', location: 'Nearest (Auto)', flag: '🌐' });
  let showServerDialog = $state(false);

  const MAX_SPEED = 1000;
  let gaugeOffset = $derived(283 - (Math.min(displaySpeed, MAX_SPEED) / MAX_SPEED * 283));
  let phaseLabel = $derived(
    phase === 'upload' ? 'UPLOAD' :
    phase === 'ping'   ? 'PING'   : 'DOWNLOAD'
  );
  let isRunning = $derived(phase === 'ping' || phase === 'download' || phase === 'upload');


  async function toggleRun() {
    if (isRunning) {
      await StopSpeedtest();
      phase = 'idle';
    } else {
      errorMsg = '';
      displaySpeed = 0;
      dlSpeed = 0;
      ulSpeed = 0;
      maxDlSpeed = 0;
      maxUlSpeed = 0;
      bestPing = 0;
      pingVal = 0;
      jitterVal = 0;
      lossVal = 0;
      phase = 'ping';
      try {
        await StartSpeedtest(selectedServer.id);
      } catch (e) {
        errorMsg = String(e);
        phase = 'idle';
      }
    }
  }

  async function refreshHistory() {
    const sessions = await GetSpeedtestHistory();
    recentTests = (sessions ?? []).slice(0, 3);
    if (recentTests.length > 0) {
      lastTestedAt = relativeTime(recentTests[0].started_at);
    }
  }

  onMount(() => {
    GetNetworkInfo().then(info => { networkInfo = info; });
    GetServers().then(s => {
      serverList = s ?? [];
    });
    refreshHistory();

    const off = EventsOn('speedtest:update', (update: SpeedtestUpdate) => {
      phase = update.phase;

      if (update.phase === 'download') {
        displaySpeed = update.speed;
        dlSpeed = update.speed;
        if (update.speed > maxDlSpeed) maxDlSpeed = update.speed;
      } else if (update.phase === 'upload') {
        displaySpeed = update.speed;
        ulSpeed = update.speed;
        if (update.speed > maxUlSpeed) maxUlSpeed = update.speed;
      }

      if (update.ping > 0) {
        pingVal = update.ping;
        if (bestPing === 0 || update.ping < bestPing) bestPing = update.ping;
      }
      if (update.jitter > 0) jitterVal = update.jitter;
      lossVal = update.loss;

      if (update.phase === 'done') {
        displaySpeed = update.download;
        dlSpeed = update.download;
        ulSpeed = update.upload;
        refreshHistory();
      }
      if (update.phase === 'failed') {
        phase = 'idle';
        errorMsg = update.error ?? 'Speed test failed';
        refreshHistory();
      }
    });

    return () => off();
  });
</script>

<svelte:head>
  <title>Speed Test - NetMonit</title>
</svelte:head>

<div class="h-full w-full max-w-350 mx-auto border-box">
  <div class="grid grid-cols-1 xl:grid-cols-[1fr_360px] gap-6 h-full pb-2">

    <!-- Left Main Column -->
    <div class="flex flex-col gap-5">

      <!-- Speedometer Card -->
      <div class="bg-linear-to-b from-white to-[#f8faff] rounded-[2.5rem] shadow-sm border border-slate-100/60 p-8 flex flex-col items-center justify-center relative overflow-hidden flex-1 select-none">

        <!-- Circular Gauge Component using pure SVG -->
        <div class="relative w-64 h-64 sm:w-68 sm:h-68 my-2 flex items-center justify-center">
          <!-- Perfectly Circular Glow -->
          <div class="absolute inset-4 rounded-full shadow-[0_0_40px_rgba(37,99,235,0.15)] pointer-events-none"></div>

          <!-- Background Track -->
          <svg class="w-full h-full -rotate-90 transform relative z-10" viewBox="0 0 100 100">
            <circle cx="50" cy="50" r="45" fill="none" stroke="#f1f5f9" stroke-width="6" />
            <!-- Progress Arc -->
            <circle cx="50" cy="50" r="45" fill="none" stroke="#2563eb" stroke-width="6" stroke-linecap="round"
                    stroke-dasharray="283" stroke-dashoffset={gaugeOffset} class="transition-all duration-1000 ease-out" />
          </svg>

          <!-- Gauge Central Data -->
          <div class="absolute inset-0 flex flex-col items-center justify-center pt-2">
            <span class="text-[0.65rem] tracking-[0.15em] font-bold text-slate-500 mb-1">{phaseLabel}</span>
            <span class="text-6xl sm:text-[4.5rem] font-black text-slate-800 leading-none tracking-tighter">{fmtMbps(displaySpeed)}</span>
            <span class="text-lg font-bold text-blue-600 mt-2">Mbps</span>
          </div>
        </div>

        <!-- Error Banner -->
        {#if errorMsg}
          <div class="w-full mb-4 flex items-center justify-between rounded-xl border border-red-200 bg-red-50 px-4 py-2.5 text-sm text-red-600">
            <span>{errorMsg}</span>
            <button onclick={() => { errorMsg = ''; }} class="ml-4 opacity-60 hover:opacity-100">
              <span class="material-symbols-outlined text-[16px]">close</span>
            </button>
          </div>
        {/if}

        <div class="mt-6 flex flex-col items-center gap-3">
          <button
            onclick={toggleRun}
            class="bg-[#0f4ed8] hover:bg-blue-700 shadow-[0_8px_20px_-6px_rgba(15,78,216,0.5)] text-white w-56 py-3 rounded-xl font-bold text-sm transition-all hover:-translate-y-0.5 active:translate-y-0 focus:outline-none focus:ring-4 focus:ring-blue-500/30 {isRunning ? 'bg-red-600! hover:bg-red-700! shadow-[0_8px_20px_-6px_rgba(220,38,38,0.5)]!' : ''}"
          >
            {isRunning ? 'Stop' : (phase === 'done' ? 'Test Again' : 'Start Speedtest')}
          </button>

          <!-- Server Location Button -->
          <button
            onclick={() => { showServerDialog = true; }}
            disabled={isRunning}
            class="flex items-center gap-2 px-3 py-1.5 rounded-lg border border-slate-200 bg-white text-slate-600 text-[0.75rem] font-semibold hover:bg-slate-50 transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
          >
            <span class="text-base leading-none">{selectedServer.flag}</span>
            <span>{selectedServer.location}</span>
            <span class="material-symbols-outlined text-[14px]">expand_more</span>
          </button>
        </div>

        <p class="text-[0.7rem] font-medium text-slate-400 mt-2">
          {lastTestedAt ? `Last tested: ${lastTestedAt}` : 'Never tested'}
        </p>
      </div>

      <ServerDialog
        bind:open={showServerDialog}
        servers={serverList}
        selectedId={selectedServer.id}
        onSelect={(s) => { selectedServer = s; }}
      />

      <!-- Three Stat Cards (Latency, Jitter, Loss) -->
      <div class="grid grid-cols-1 sm:grid-cols-3 gap-x-5 gap-y-4 shrink-0 mb-2">

        <!-- Download -->
        <div class="relative pt-7">
          <div class="absolute top-0 left-4 z-10 w-14 h-14 rounded-full bg-blue-600 border-4 border-white shadow-[0_4px_14px_rgba(37,99,235,0.4)] flex items-center justify-center">
            <div class="w-9 h-9 rounded-full border-2 border-white/50 flex items-center justify-center">
              <span class="material-symbols-outlined text-white text-[22px]">download</span>
            </div>
          </div>
          <div class="rounded-2xl overflow-hidden shadow-[0_4px_20px_rgba(0,0,0,0.07)] border border-slate-100 h-full flex flex-col">
            <div class="bg-blue-600 h-11 shrink-0 flex items-center pl-20 pr-4">
              <span class="text-white font-extrabold text-[0.9rem] tracking-tight">Download</span>
            </div>
            <div class="bg-white px-5 pt-3 pb-5 flex-1 flex flex-col items-center justify-center text-center">
              <div class="w-full flex items-center justify-between text-[0.72rem] text-slate-400 font-medium">
                <span>Average</span>
                <span>{dlSpeed > 0 ? `${fmtMbps(dlSpeed)} Mbps` : '-- Mbps'}</span>
              </div>
              <div class="flex items-baseline justify-center gap-1.5 mt-3">
                <span class="text-4xl font-black text-blue-600">{maxDlSpeed > 0 ? fmtMbps(maxDlSpeed) : '--'}</span>
                <span class="text-lg font-bold text-blue-600">Mbps</span>
              </div>
              <div class="text-[0.6rem] text-slate-400 font-medium mt-1 tracking-wider uppercase">(Nilai terbesar)</div>
            </div>
          </div>
        </div>

        <!-- Upload -->
        <div class="relative pt-7">
          <div class="absolute top-0 left-4 z-10 w-14 h-14 rounded-full bg-blue-600 border-4 border-white shadow-[0_4px_14px_rgba(37,99,235,0.4)] flex items-center justify-center">
            <div class="w-9 h-9 rounded-full border-2 border-white/50 flex items-center justify-center">
              <span class="material-symbols-outlined text-white text-[22px]">upload</span>
            </div>
          </div>
          <div class="rounded-2xl overflow-hidden shadow-[0_4px_20px_rgba(0,0,0,0.07)] border border-slate-100 h-full flex flex-col">
            <div class="bg-blue-600 h-11 shrink-0 flex items-center pl-20 pr-4">
              <span class="text-white font-extrabold text-[0.9rem] tracking-tight">Upload</span>
            </div>
            <div class="bg-white px-5 pt-3 pb-5 flex-1 flex flex-col items-center justify-center text-center">
              <div class="w-full flex items-center justify-between text-[0.72rem] text-slate-400 font-medium">
                <span>Average</span>
                <span>{ulSpeed > 0 ? `${fmtMbps(ulSpeed)} Mbps` : '-- Mbps'}</span>
              </div>
              <div class="flex items-baseline justify-center gap-1.5 mt-3">
                <span class="text-4xl font-black text-blue-600">{maxUlSpeed > 0 ? fmtMbps(maxUlSpeed) : '--'}</span>
                <span class="text-lg font-bold text-blue-600">Mbps</span>
              </div>
              <div class="text-[0.6rem] text-slate-400 font-medium mt-1 tracking-wider uppercase">(Nilai terbesar)</div>
            </div>
          </div>
        </div>

        <!-- Latency -->
        <div class="relative pt-7">
          <div class="absolute top-0 left-4 z-10 w-14 h-14 rounded-full bg-blue-600 border-4 border-white shadow-[0_4px_14px_rgba(37,99,235,0.4)] flex items-center justify-center">
            <div class="w-9 h-9 rounded-full border-2 border-white/50 flex items-center justify-center">
              <span class="material-symbols-outlined text-white text-[22px]">speed</span>
            </div>
          </div>
          <div class="rounded-2xl overflow-hidden shadow-[0_4px_20px_rgba(0,0,0,0.07)] border border-slate-100 h-full flex flex-col">
            <div class="bg-blue-600 h-11 shrink-0 flex items-center pl-20 pr-4">
              <span class="text-white font-extrabold text-[0.9rem] tracking-tight">Latency</span>
            </div>
            <div class="bg-white px-5 pt-3 pb-5 flex-1 flex flex-col items-center justify-center text-center">
              <div class="w-full flex items-center justify-between text-[0.72rem] text-slate-400 font-medium mb-1">
                <span>Average</span>
                <span>{pingVal > 0 ? `${pingVal} ms` : '-- ms'}</span>
              </div>
              <div class="w-full flex items-center justify-between text-[0.72rem] text-slate-400 font-medium">
                <span>Jitter</span>
                <span>{jitterVal > 0 ? `${jitterVal.toFixed(1)} ms` : '-- ms'}</span>
              </div>
              <div class="flex items-baseline justify-center gap-1.5 mt-2">
                <span class="text-4xl font-black text-blue-600">{bestPing > 0 ? bestPing : '--'}</span>
                <span class="text-lg font-bold text-blue-600">ms</span>
              </div>
              <div class="text-[0.6rem] text-slate-400 font-medium mt-1 tracking-wider uppercase">(Hasil terbaik)</div>
            </div>
          </div>
        </div>

      </div>
    </div>

    <!-- Right Side Column -->
    <div class="flex flex-col gap-5">

      <!-- Network Info -->
      <div class="bg-[#f8fafc] rounded-3xl p-6 border border-slate-200/60 shadow-sm flex flex-col relative overflow-hidden">
        <h3 class="text-[0.65rem] tracking-[0.15em] font-black text-slate-500 mb-4">NETWORK INFO</h3>

        <div class="space-y-4 relative z-10">
          <!-- Provider -->
          <div class="flex items-center gap-4">
            <div class="w-11 h-11 bg-white shadow-sm border border-slate-100 rounded-xl flex items-center justify-center text-blue-600 shrink-0">
              <span class="material-symbols-outlined text-[20px]">router</span>
            </div>
            <div class="flex flex-col">
              <span class="text-[0.6rem] font-black tracking-wider text-slate-400">PROVIDER</span>
              <span class="text-[0.85rem] font-bold text-slate-800">{networkInfo.provider}</span>
            </div>
          </div>

          <!-- IP -->
          <div class="flex items-center gap-4">
            <div class="w-11 h-11 bg-white shadow-sm border border-slate-100 rounded-xl flex items-center justify-center text-blue-600 shrink-0">
              <span class="material-symbols-outlined text-[20px]">public</span>
            </div>
            <div class="flex flex-col">
              <span class="text-[0.6rem] font-black tracking-wider text-slate-400">IP ADDRESS</span>
              <span class="text-[0.85rem] font-bold text-slate-800">{networkInfo.ip}</span>
            </div>
          </div>

          <!-- Server -->
          <div class="flex items-center gap-4">
            <div class="w-11 h-11 bg-white shadow-sm border border-slate-100 rounded-xl flex items-center justify-center text-blue-600 shrink-0">
              <span class="material-symbols-outlined text-[20px]">dns</span>
            </div>
            <div class="flex flex-col">
              <span class="text-[0.6rem] font-black tracking-wider text-slate-400">SERVER</span>
              <span class="text-[0.85rem] font-bold text-slate-800">{networkInfo.city}{networkInfo.country ? `, ${networkInfo.country}` : ''}</span>
            </div>
          </div>
        </div>

        <!-- Dynamic Map Blob -->
        <div class="mt-6">
          <MapBlob lat={networkInfo.lat} lon={networkInfo.lon} city={networkInfo.city} country={networkInfo.country} />
        </div>
      </div>

      <!-- Recent Tests -->
      <div class="bg-white rounded-3xl p-6 border border-slate-100 shadow-[0_2px_15px_rgba(0,0,0,0.02)] flex-1 mb-2">
        <div class="flex items-center justify-between mb-4">
          <h3 class="text-[0.65rem] tracking-[0.15em] font-black text-slate-500">RECENT TESTS</h3>
          <a href="/history" class="text-[0.70rem] font-bold text-blue-600 hover:text-blue-800 hover:underline">View All</a>
        </div>

        <div class="space-y-1">
          {#if recentTests.length === 0}
            <p class="text-[0.75rem] text-slate-400 text-center py-6">No tests yet</p>
          {:else}
            {#each recentTests as t (t.id)}
              <div class="flex items-center justify-between hover:bg-slate-50 p-2.5 -mx-2.5 rounded-2xl transition-colors cursor-pointer group">
                <div class="flex items-center gap-3.5">
                  <div class="w-10 h-10 rounded-full {t.failed ? 'bg-red-50 text-red-500' : 'bg-blue-50 text-blue-600'} flex items-center justify-center shrink-0">
                    <span class="material-symbols-outlined text-[18px]">{t.failed ? 'priority_high' : 'download'}</span>
                  </div>
                  <div class="flex flex-col">
                    <span class="text-[0.85rem] font-extrabold text-slate-800">{t.failed ? 'Failed' : `${fmtMbps(t.download_mbps)} Mbps`}</span>
                    <span class="text-[0.65rem] font-medium text-slate-400">{relativeTime(t.started_at)}</span>
                  </div>
                </div>
                <span class="material-symbols-outlined text-[18px] text-slate-300 group-hover:text-blue-400 transition-colors">chevron_right</span>
              </div>
            {/each}
          {/if}
        </div>
      </div>

    </div>
  </div>
</div>
