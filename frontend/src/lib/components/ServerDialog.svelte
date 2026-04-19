<script lang="ts">
  import { Dialog } from 'bits-ui';
  import type { main } from '../../../wailsjs/go/models';

  let {
    open = $bindable(false),
    servers = [],
    selectedId = '',
    onSelect,
  }: {
    open: boolean;
    servers: main.SpeedServer[];
    selectedId: string;
    onSelect: (server: main.SpeedServer) => void;
  } = $props();

  let query = $state('');

  let filtered = $derived(
    servers.filter(s =>
      query === '' ||
      s.location.toLowerCase().includes(query.toLowerCase()) ||
      s.name.toLowerCase().includes(query.toLowerCase())
    )
  );

  function handleSelect(server: main.SpeedServer) {
    onSelect(server);
    open = false;
    query = '';
  }
</script>

<Dialog.Root bind:open onOpenChange={(v) => { if (!v) query = ''; }}>
  <Dialog.Portal>
    <Dialog.Overlay
      class="fixed inset-0 z-50 bg-black/40 backdrop-blur-sm data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0"
    />
    <Dialog.Content
      class="fixed left-1/2 top-1/2 z-50 -translate-x-1/2 -translate-y-1/2 w-full max-w-md rounded-2xl bg-white shadow-xl border border-slate-100 p-6 focus:outline-none data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95 duration-150"
    >
      <!-- Header -->
      <Dialog.Title class="text-[1rem] font-extrabold text-slate-800 tracking-tight">
        Select Server Location
      </Dialog.Title>
      <Dialog.Description class="text-[0.75rem] text-slate-400 mt-0.5 mb-3">
        Choose the server to test your connection speed.
      </Dialog.Description>

      <!-- Search -->
      <div class="relative mb-3">
        <span class="material-symbols-outlined absolute left-3 top-1/2 -translate-y-1/2 text-[18px] text-slate-400 pointer-events-none">search</span>
        <input
          type="text"
          bind:value={query}
          placeholder="Search server or location..."
          class="w-full pl-9 pr-3 py-2 text-[0.85rem] rounded-xl border border-slate-200 bg-slate-50 focus:outline-none focus:ring-2 focus:ring-blue-500/30 focus:border-blue-400 placeholder:text-slate-400"
        />
      </div>

      <!-- Server List -->
      <div class="max-h-80 overflow-y-auto pr-1">
        {#if filtered.length === 0}
          <div class="px-4 py-3 text-slate-400 text-[0.8rem]">No results for "{query}"</div>
        {:else}
          {#each filtered as s (s.id)}
            <button
              onclick={() => handleSelect(s)}
              class="w-full flex items-center gap-3 px-4 py-2.5 rounded-xl text-left transition-colors
                     {s.id === selectedId
                       ? 'bg-blue-50 border border-blue-200 text-blue-700'
                       : 'hover:bg-slate-50 border border-transparent text-slate-700'}"
            >
              <span class="text-xl shrink-0 leading-none">{s.flag}</span>
              <div class="flex-1 min-w-0">
                <p class="text-[0.83rem] font-bold leading-none">{s.location}</p>
                <p class="text-[0.70rem] text-slate-400 mt-0.5">{s.name}</p>
              </div>
              {#if s.id === selectedId}
                <span class="material-symbols-outlined text-blue-600 text-[18px] shrink-0">check_circle</span>
              {/if}
            </button>
          {/each}
        {/if}
      </div>

      <!-- Close button -->
      <Dialog.Close
        class="absolute right-4 top-4 flex h-7 w-7 items-center justify-center rounded-full text-slate-400 hover:bg-slate-100 hover:text-slate-600 transition-colors focus:outline-none"
        aria-label="Close"
      >
        <span class="material-symbols-outlined text-[18px]">close</span>
      </Dialog.Close>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>
