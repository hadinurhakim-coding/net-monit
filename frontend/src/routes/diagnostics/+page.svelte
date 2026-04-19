<script lang="ts">
  import { onMount } from 'svelte';
  import { Input } from "$lib/components/ui/input/index.js";
  import { Button } from "$lib/components/ui/button/index.js";
  import * as DropdownMenu from "$lib/components/ui/dropdown-menu/index.js";
  import * as Table from "$lib/components/ui/table/index.js";
  import { type HopUpdate, type DiagnosticsUpdate, fmtMs, fmtLoss } from '$lib/types/diagnostics';
  import { StartDiagnostics, StopDiagnostics, GetHistory, ExportToFile, GetDiagnosticsStatus, DeleteHost } from '../../../wailsjs/go/main/App';
  import { EventsOn, ClipboardSetText } from '../../../wailsjs/runtime/runtime';

  let isRunning = $state(false);
  let host = $state('');
  let historyHosts = $state<string[]>([]);
  let hops = $state<HopUpdate[]>([]);
  let errorMsg = $state('');

  onMount(() => {
    GetHistory()
      .then((h: string[]) => { historyHosts = h ?? []; })
      .catch(() => {});

    // Sync running state with backend in case user navigated away mid-run
    GetDiagnosticsStatus().then(running => { isRunning = running; });

    const off = EventsOn('diagnostics:update', (update: DiagnosticsUpdate) => {
      hops = update.hops ?? [];
      if (update.done) isRunning = false;
      if (update.error) {
        errorMsg = update.error;
        isRunning = false;
      }
    });

    return () => off();
  });

  async function toggleRun() {
    if (isRunning) {
      await StopDiagnostics();
    } else {
      if (!host.trim()) return;
      errorMsg = '';
      hops = [];
      isRunning = true;
      try {
        await StartDiagnostics(host.trim());
      } catch (e) {
        errorMsg = String(e);
        isRunning = false;
      }
    }
  }

  function buildAnalysis(data: HopUpdate[]): string {
    if (data.length === 0) return '';

    const final = data[data.length - 1];
    const middle = data.slice(0, -1);

    // Hops in the middle path with significant ICMP loss
    const midLossHops = middle.filter(h => h.loss > 15);
    const finalLoss   = final.loss;
    const finalAvg    = final.avg; // -1 means no reply

    // ── Status ────────────────────────────────────────────────────────────────
    let status: string;
    if (finalAvg < 0) {
      status = 'Tujuan akhir tidak memberikan respons — host mungkin offline atau ICMP diblokir firewall.';
    } else if (finalLoss === 0 && finalAvg <= 20) {
      status = 'Koneksi internet Anda sangat lancar dan stabil.';
    } else if (finalLoss === 0 && finalAvg <= 50) {
      status = 'Koneksi internet Anda lancar dan stabil.';
    } else if (finalLoss === 0 && finalAvg <= 100) {
      status = 'Koneksi internet Anda stabil namun memiliki latensi yang cukup tinggi.';
    } else if (finalLoss === 0) {
      status = 'Koneksi internet Anda mencapai tujuan, namun responsnya sangat lambat.';
    } else if (finalLoss <= 5) {
      status = 'Koneksi internet Anda mengalami sedikit gangguan pada hop akhir.';
    } else if (finalLoss <= 20) {
      status = 'Koneksi internet Anda mengalami gangguan — terdapat packet loss di tujuan.';
    } else {
      status = 'Koneksi internet Anda mengalami gangguan serius — packet loss tinggi di tujuan akhir.';
    }

    // ── Keterangan ────────────────────────────────────────────────────────────
    let body: string;
    const icmpRateLimiting = midLossHops.length > 0 && finalLoss === 0 && finalAvg >= 0;

    if (icmpRateLimiting) {
      const maxMidLoss = Math.max(...midLossHops.map(h => h.loss));
      const nrs        = midLossHops.map(h => h.nr);
      const nrLabel    = nrs.length === 1
        ? `baris ke-${nrs[0]}`
        : `baris ${nrs.slice(0, -1).join(', ')} dan ${nrs[nrs.length - 1]}`;

      body =
        `Meskipun terlihat ada packet loss yang tinggi (hingga ${maxMidLoss.toFixed(0)}%) pada ${nrLabel}, ` +
        `Anda sama sekali tidak perlu khawatir. Itu hanyalah router penyedia internet di tengah jalur yang sengaja ` +
        `membatasi balasan terhadap paket ICMP (paket tes jaringan ini) untuk menghemat kinerja mesin mereka — ` +
        `perilaku ini sangat umum dan normal di jaringan ISP.\n\n` +
        `Buktinya ada pada baris ke-${final.nr} (tujuan akhir: ${final.host}). ` +
        `Di titik akhir tersebut, packet loss kembali menjadi ${fmtLoss(finalLoss)} ` +
        `dengan kecepatan respons rata-rata ${finalAvg}ms.\n\n` +
        `Artinya, lalu lintas data Anda melewati jalur tersebut dengan mulus sampai ke tujuan tanpa ada paket yang benar-benar hilang.`;

    } else if (finalAvg < 0) {
      body =
        `Tujuan akhir (${final.host}) tidak memberikan balasan sama sekali. ` +
        `Kemungkinan penyebabnya: host sedang offline, terdapat firewall yang memblokir paket ICMP, ` +
        `atau ada gangguan routing sebelum mencapai tujuan.` +
        (midLossHops.length > 0
          ? `\n\nTerdapat juga loss tinggi pada beberapa hop di tengah jalur (baris ${midLossHops.map(h => h.nr).join(', ')}), ` +
            `yang memperkuat kemungkinan adanya gangguan nyata pada rute ini.`
          : '');

    } else if (finalLoss > 0) {
      const firstBadHop = data.find(h => h.loss > 5 && h.nr < final.nr);
      body =
        `Terdapat packet loss nyata sebesar ${fmtLoss(finalLoss)} pada tujuan akhir (${final.host}). ` +
        `Ini berarti sebagian data yang dikirim tidak sampai ke tujuan.` +
        (firstBadHop
          ? `\n\nMasalah pertama kali muncul pada baris ke-${firstBadHop.nr} (${firstBadHop.host}) ` +
            `dengan loss ${fmtLoss(firstBadHop.loss)}, mengindikasikan gangguan pada segmen jaringan tersebut.`
          : '') +
        `\n\nLatensi rata-rata ke tujuan adalah ${finalAvg}ms. ` +
        (finalAvg <= 50
          ? 'Latency masih wajar, namun packet loss dapat menyebabkan koneksi terasa tidak stabil.'
          : 'Latency yang tinggi ditambah packet loss dapat membuat koneksi terasa sangat lambat dan terputus-putus.') +
        `\n\nDisarankan untuk menghubungi penyedia layanan internet (ISP) Anda jika masalah ini berlanjut.`;

    } else {
      // Clean path
      body =
        `Seluruh ${data.length} hop menuju ${final.host} dalam kondisi baik tanpa packet loss yang berarti. ` +
        `Latensi rata-rata ke tujuan akhir adalah ${finalAvg}ms` +
        (finalAvg <= 20
          ? ', yang tergolong sangat cepat dan ideal untuk semua jenis penggunaan internet.'
          : finalAvg <= 50
            ? ', yang tergolong baik dan memadai untuk aktivitas sehari-hari maupun gaming.'
            : finalAvg <= 100
              ? ', yang masih dalam batas wajar namun mungkin terasa sedikit lambat untuk aplikasi sensitif latensi seperti video call atau gaming.'
              : ', yang tergolong tinggi. Pertimbangkan untuk menghubungi ISP Anda jika ini terasa lambat.');
    }

    return `\nStatus      : ${status}\n\nKeterangan  : ${body}`;
  }

  async function copyText() {
    if (hops.length === 0) return;
    const header = `${'Nr'.padStart(3)}  ${'Hostname'.padEnd(36)}  ${'Loss%'.padStart(7)}  ${'Sent'.padStart(4)}  ${'Recv'.padStart(4)}  ${'Best'.padStart(7)}  ${'Avg'.padStart(7)}  ${'Worst'.padStart(7)}  ${'Last'.padStart(7)}`;
    const sep = '-'.repeat(header.length);
    const rows = hops.map(h =>
      `${String(h.nr).padStart(3)}  ${h.host.padEnd(36)}  ${fmtLoss(h.loss).padStart(7)}  ${String(h.sent).padStart(4)}  ${String(h.recv).padStart(4)}  ${fmtMs(h.best).padStart(7)}  ${fmtMs(h.avg).padStart(7)}  ${fmtMs(h.worst).padStart(7)}  ${fmtMs(h.last).padStart(7)}`
    );
    await ClipboardSetText([`Host: ${host}`, header, sep, ...rows, buildAnalysis(hops)].join('\n'));
  }

  async function exportFile() {
    try {
      await ExportToFile();
    } catch (e) {
      errorMsg = String(e);
    }
  }
</script>

<div class="flex flex-col h-full bg-background p-6 pt-5 gap-6">

  <!-- Header Controls -->
  <div class="flex flex-row items-end gap-4 rounded-xl border bg-card p-4 shadow-sm w-full">
    <div class="flex-1 flex flex-col gap-2">
      <label for="host-input" class="text-sm font-semibold tracking-tight text-foreground">Host</label>
      <!-- Combobox: free-text input + history dropdown trigger -->
      <div class="relative flex items-center">
        <Input
          id="host-input"
          bind:value={host}
          placeholder="google.com or IP address"
          class="h-10 text-base pr-10"
          disabled={isRunning}
          onkeydown={(e) => { if (e.key === 'Enter' && !isRunning) toggleRun(); }}
        />
        <DropdownMenu.Root>
          <DropdownMenu.Trigger
            class="absolute right-0 h-10 w-10 flex items-center justify-center text-muted-foreground hover:text-foreground hover:bg-muted/60 rounded-r-md border-l border-input transition-colors focus:outline-none"
            aria-label="Show host history"
          >
            <span class="material-symbols-outlined text-[18px]">expand_more</span>
          </DropdownMenu.Trigger>
          <DropdownMenu.Content align="end" class="w-64">
            {#if historyHosts.length === 0}
              <div class="px-3 py-2 text-sm text-muted-foreground">No history yet</div>
            {:else}
              {#each historyHosts as h}
                <DropdownMenu.Item
                  onclick={() => { host = h; }}
                  class="font-mono text-sm flex items-center justify-between gap-2 pr-1 group"
                >
                  <span class="flex-1 truncate">{h}</span>
                  <button
                    onclick={(e) => { e.stopPropagation(); DeleteHost(h).then(() => { historyHosts = historyHosts.filter(x => x !== h); }); }}
                    class="shrink-0 flex items-center justify-center w-5 h-5 rounded opacity-0 group-hover:opacity-100 hover:bg-destructive/15 hover:text-destructive text-muted-foreground transition-opacity"
                    aria-label="Remove {h}"
                  >
                    <span class="material-symbols-outlined text-[14px]">close</span>
                  </button>
                </DropdownMenu.Item>
              {/each}
            {/if}
          </DropdownMenu.Content>
        </DropdownMenu.Root>
      </div>
    </div>

    <div class="flex-none flex gap-3 h-10">
      <Button
        onclick={toggleRun}
        variant={isRunning ? "destructive" : "default"}
        class="min-w-25 gap-2 h-10 px-5 transition-all text-sm font-semibold"
      >
        <span class="material-symbols-outlined text-[18px]">
          {isRunning ? "stop" : "play_arrow"}
        </span>
        {isRunning ? 'Stop' : 'Start'}
      </Button>
      <Button variant="secondary" class="h-10 gap-2 px-4 border text-sm font-semibold hover:bg-muted">
        <span class="material-symbols-outlined text-[18px]">settings</span>
        Options
      </Button>
    </div>
  </div>

  <!-- Error Banner -->
  {#if errorMsg}
    <div class="flex items-center justify-between rounded-lg border border-destructive/30 bg-destructive/10 px-4 py-2.5 text-sm text-destructive">
      <span>{errorMsg}</span>
      <button onclick={() => { errorMsg = ''; }} class="ml-4 opacity-70 hover:opacity-100">
        <span class="material-symbols-outlined text-[16px]">close</span>
      </button>
    </div>
  {/if}

  <!-- Content Container (Toolbar + Table) -->
  <div class="flex flex-col flex-1 rounded-xl border bg-card shadow-sm overflow-hidden">

    <!-- Action Toolbar -->
    <div class="flex items-center justify-between p-3 border-b bg-muted/40">
      <DropdownMenu.Root>
        <DropdownMenu.Trigger class="inline-flex items-center justify-center whitespace-nowrap rounded-md text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:pointer-events-none disabled:opacity-50 border border-input bg-background shadow-sm hover:bg-accent hover:text-accent-foreground h-9 px-4 py-2 gap-2">
          Copy to Clipboard
          <span class="material-symbols-outlined text-[18px]">expand_more</span>
        </DropdownMenu.Trigger>
        <DropdownMenu.Content align="start">
          <DropdownMenu.Item onclick={copyText}>Text</DropdownMenu.Item>
        </DropdownMenu.Content>
      </DropdownMenu.Root>

      <DropdownMenu.Root>
        <DropdownMenu.Trigger class="inline-flex items-center justify-center whitespace-nowrap rounded-md text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:pointer-events-none disabled:opacity-50 border border-input bg-background shadow-sm hover:bg-accent hover:text-accent-foreground h-9 px-4 py-2 gap-2">
          Export
          <span class="material-symbols-outlined text-[18px]">expand_more</span>
        </DropdownMenu.Trigger>
        <DropdownMenu.Content align="end">
          <DropdownMenu.Item onclick={exportFile}>Text (.txt)</DropdownMenu.Item>
        </DropdownMenu.Content>
      </DropdownMenu.Root>
    </div>

    <!-- Data Table -->
    <div class="flex-1 overflow-auto bg-card">
      <Table.Root>
        <Table.Header class="bg-muted/10 sticky top-0 z-10 shadow-sm backdrop-blur">
          <Table.Row class="hover:bg-transparent">
            <Table.Head class="font-bold text-foreground text-xs py-3 pl-4">Hostname</Table.Head>
            <Table.Head class="font-bold text-foreground text-xs py-3">Nr</Table.Head>
            <Table.Head class="font-bold text-foreground text-xs py-3">Loss %</Table.Head>
            <Table.Head class="font-bold text-foreground text-xs py-3">Sent</Table.Head>
            <Table.Head class="font-bold text-foreground text-xs py-3">Recv</Table.Head>
            <Table.Head class="font-bold text-foreground text-xs py-3">Best</Table.Head>
            <Table.Head class="font-bold text-foreground text-xs py-3">Avrg</Table.Head>
            <Table.Head class="font-bold text-foreground text-xs py-3">Worst</Table.Head>
            <Table.Head class="font-bold text-foreground text-xs py-3 pr-4">Last</Table.Head>
          </Table.Row>
        </Table.Header>
        <Table.Body>
          {#each hops as row (row.nr)}
            <Table.Row class="transition-colors hover:bg-muted/50 data-[state=selected]:bg-muted border-b border-border/50">
              <Table.Cell class="font-medium text-sm py-3 pl-4">{row.host}</Table.Cell>
              <Table.Cell class="text-sm py-3">{row.nr}</Table.Cell>
              <Table.Cell class="text-sm py-3">{fmtLoss(row.loss)}</Table.Cell>
              <Table.Cell class="text-sm py-3">{row.sent}</Table.Cell>
              <Table.Cell class="text-sm py-3">{row.recv}</Table.Cell>
              <Table.Cell class="text-sm py-3">{fmtMs(row.best)}</Table.Cell>
              <Table.Cell class="text-sm py-3">{fmtMs(row.avg)}</Table.Cell>
              <Table.Cell class="text-sm py-3">{fmtMs(row.worst)}</Table.Cell>
              <Table.Cell class="text-sm py-3 pr-4">{fmtMs(row.last)}</Table.Cell>
            </Table.Row>
          {:else}
            <Table.Row class="hover:bg-transparent">
              <Table.Cell colspan={9} class="text-center text-muted-foreground py-12 text-sm">
                {#if isRunning}
                  <span class="material-symbols-outlined text-[20px] align-middle mr-2 animate-spin">progress_activity</span>
                  Discovering hops...
                {:else}
                  Enter a host above and click Start to begin diagnostics
                {/if}
              </Table.Cell>
            </Table.Row>
          {/each}
        </Table.Body>
      </Table.Root>
    </div>
  </div>
</div>
