<script lang="ts">
  import { onMount, tick } from 'svelte';
  import { Dialog } from 'bits-ui';
  import SvelteMarkdown from 'svelte-markdown';
  import { EventsOn } from '../../../wailsjs/runtime/runtime';
  import {
    SendChatMessage,
    GetChatSessions,
    GetChatSession,
    DeleteChatSession,
    StopChatStream,
    CheckOllamaStatus,
    StartOllama,
    PullDeepSeekModel,
  } from '../../../wailsjs/go/main/App';
  import type { ChatChunk, OllamaStatus, PullProgress } from '$lib/types/chat';
  import { chatSessionTitle, relativeTime } from '$lib/types/chat';

  // Local shape interfaces — compatible with Wails-generated classes but usable with plain objects
  interface ChatMessage { id: string; role: string; content: string; timestamp: string; }
  interface ChatSession { id: string; messages: ChatMessage[]; created_at: string; updated_at: string; }
  interface ToolActivity { id: string; tool_name: string; args: string; result?: string; done: boolean; }

  let { open = $bindable(false) }: { open: boolean } = $props();

  // ── State ────────────────────────────────────────────────────────────────────
  let sessions        = $state<ChatSession[]>([]);
  let activeSession   = $state<ChatSession | null>(null);
  let inputText       = $state('');
  let isStreaming     = $state(false);
  let streamingDelta  = $state('');   // accumulates current assistant tokens
  let ollamaStatus    = $state<OllamaStatus | null>(null);
  let pullProgress    = $state<PullProgress | null>(null);
  let errorMsg        = $state('');
  let showSidebar     = $state(false);
  let messagesEl      = $state<HTMLElement | null>(null);
  let toolActivities  = $state<ToolActivity[]>([]);

  // ── Derived ──────────────────────────────────────────────────────────────────
  let messages = $derived(activeSession?.messages ?? []);
  let canSend  = $derived(
    inputText.trim().length > 0 &&
    !isStreaming &&
    ollamaStatus?.available === true &&
    ollamaStatus?.model_ready === true
  );

  // ── Lifecycle ────────────────────────────────────────────────────────────────
  onMount(() => {
    CheckOllamaStatus().then(s => { ollamaStatus = s; });
    loadSessions();

    const offChunk = EventsOn('chat:chunk', (chunk: ChatChunk) => {
      if (chunk.error && chunk.error !== 'cancelled') {
        errorMsg = chunk.error;
        isStreaming = false;
        streamingDelta = '';
        toolActivities = [];
        return;
      }
      if (!chunk.done) {
        streamingDelta += chunk.delta;
        tick().then(scrollToBottom);
        return;
      }
      // done — finalise message, reload session from storage
      isStreaming = false;
      streamingDelta = '';
      toolActivities = [];
      if (activeSession) {
        GetChatSession(activeSession.id).then(s => {
          if (s) activeSession = s;
        });
      }
      loadSessions();
      tick().then(scrollToBottom);
    });

    const offTool = EventsOn('chat:tool_call', (evt: { session_id: string; tool_name: string; args: string; result?: string; is_result: boolean }) => {
      if (!evt.is_result) {
        streamingDelta = ''; // clear any pre-tool thinking text
        toolActivities = [...toolActivities, { id: crypto.randomUUID(), tool_name: evt.tool_name, args: evt.args, done: false }];
      } else {
        toolActivities = toolActivities.map(a =>
          a.tool_name === evt.tool_name && !a.done ? { ...a, result: evt.result, done: true } : a
        );
      }
      tick().then(scrollToBottom);
    });

    const offStatus = EventsOn('chat:ollama_status', (s: OllamaStatus) => {
      ollamaStatus = s;
    });

    const offPull = EventsOn('chat:pull_progress', (p: PullProgress) => {
      pullProgress = p;
      if (p.status === 'success') pullProgress = null;
    });

    return () => { offChunk(); offStatus(); offPull(); offTool(); };
  });

  // Re-check Ollama every time the panel opens
  $effect(() => {
    if (open) {
      CheckOllamaStatus().then(s => { ollamaStatus = s; });
      loadSessions();
      tick().then(scrollToBottom);
    }
  });

  // ── Actions ──────────────────────────────────────────────────────────────────
  async function loadSessions() {
    const list = await GetChatSessions();
    sessions = list ?? [];
  }

  function newChat() {
    activeSession = {
      id: crypto.randomUUID(),
      messages: [],
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
    };
    showSidebar = false;
    errorMsg = '';
  }

  async function openSession(s: ChatSession) {
    const full = await GetChatSession(s.id);
    activeSession = full ?? s;
    showSidebar = false;
    await tick();
    scrollToBottom();
  }

  async function removeSession(id: string, e: MouseEvent) {
    e.stopPropagation();
    await DeleteChatSession(id);
    if (activeSession?.id === id) activeSession = null;
    loadSessions();
  }

  async function sendMessage() {
    const text = inputText.trim();
    if (!text || !canSend) return;

    if (!activeSession) newChat();

    // Optimistic user bubble
    const userMsg: ChatMessage = {
      id: crypto.randomUUID(),
      role: 'user',
      content: text,
      timestamp: new Date().toISOString(),
    };
    activeSession = {
      ...activeSession!,
      messages: [...(activeSession?.messages ?? []), userMsg],
      updated_at: new Date().toISOString(),
    };

    inputText = '';
    isStreaming = true;
    errorMsg = '';
    streamingDelta = '';

    await tick();
    scrollToBottom();

    try {
      await SendChatMessage(activeSession!.id, text);
    } catch (e) {
      errorMsg = String(e);
      isStreaming = false;
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      sendMessage();
    }
  }

  function stopStream() {
    StopChatStream();
    isStreaming = false;
    streamingDelta = '';
  }

  async function retryOllama() {
    ollamaStatus = null;
    const s = await CheckOllamaStatus();
    ollamaStatus = s;
  }

  let startingOllama = $state(false);
  async function startOllama() {
    startingOllama = true;
    try {
      await StartOllama();
      // Give ollama a moment to start, then check status
      await new Promise(r => setTimeout(r, 2500));
      await retryOllama();
    } catch (e) {
      errorMsg = String(e);
    } finally {
      startingOllama = false;
    }
  }

  async function pullModel() {
    pullProgress = { status: 'Starting download…', completed: 0, total: 0 };
    await PullDeepSeekModel();
    await retryOllama();
  }

  function scrollToBottom() {
    if (messagesEl) messagesEl.scrollTop = messagesEl.scrollHeight;
  }

  // Strip DeepSeek R1 <think>…</think> blocks into a separate field
  function parseAssistant(content: string): { thinking: string; answer: string } {
    const match = content.match(/^<think>([\s\S]*?)<\/think>\s*([\s\S]*)$/);
    if (match) return { thinking: match[1].trim(), answer: match[2].trim() };
    return { thinking: '', answer: content };
  }

  function toolLabel(name: string): string {
    if (name === 'run_diagnostics') return 'Running diagnostics';
    if (name === 'run_speedtest')   return 'Running speed test';
    if (name === 'get_network_info') return 'Getting network info';
    return name;
  }

  function toolArgsHint(name: string, argsStr: string): string {
    try {
      const a = JSON.parse(argsStr);
      if (name === 'run_diagnostics' && a.host) return `→ ${a.host}`;
    } catch { /* ignore */ }
    return '';
  }

  // Pull progress percentage
  let pullPct = $derived(
    pullProgress && pullProgress.total > 0
      ? Math.round((pullProgress.completed / pullProgress.total) * 100)
      : 0
  );
</script>

<Dialog.Root bind:open>
  <Dialog.Portal>
    <!-- Overlay -->
    <Dialog.Overlay
      class="fixed inset-0 z-40 bg-black/40 backdrop-blur-sm
             data-[state=open]:animate-in data-[state=closed]:animate-out
             data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 duration-200"
    />

    <!-- Centered modal -->
    <Dialog.Content
      class="fixed inset-0 z-50 m-auto w-240 max-w-[94vw] h-[84vh]
             bg-white rounded-2xl shadow-2xl border border-slate-100/80
             flex flex-col outline-none overflow-hidden
             data-[state=open]:animate-in data-[state=closed]:animate-out
             data-[state=open]:fade-in-0 data-[state=closed]:fade-out-0
             data-[state=open]:zoom-in-95 data-[state=closed]:zoom-out-95
             duration-200"
    >

      <!-- ── Header ─────────────────────────────────────────────────────────── -->
      <div class="flex items-center gap-2 px-5 py-3.5 border-b border-slate-100 shrink-0 bg-white/80 backdrop-blur-sm">
        <!-- History toggle -->
        <button
          onclick={() => showSidebar = !showSidebar}
          title="Chat history"
          class="flex h-8 w-8 items-center justify-center rounded-lg transition-colors
                 {showSidebar ? 'bg-blue-50 text-blue-600' : 'text-slate-400 hover:bg-slate-100 hover:text-slate-600'}"
        >
          <span class="material-symbols-outlined text-[20px]">history</span>
        </button>

        <!-- Logo + title -->
        <div class="flex items-center gap-2.5 flex-1 min-w-0">
          <img src="/logo.png" alt="NetMonit" class="w-6 h-6 rounded-lg object-contain shrink-0" />
          <div class="min-w-0">
            <p class="text-[0.85rem] font-extrabold text-slate-800 tracking-tight leading-none truncate">
              {activeSession ? chatSessionTitle(activeSession) : 'NetMonit Assistant'}
            </p>
            <p class="text-[0.68rem] text-slate-400 mt-0.5">DeepSeek R1 · DeBERTa · Agent Tools</p>
          </div>
        </div>

        <!-- New chat -->
        <button
          onclick={newChat}
          title="New chat"
          class="flex h-8 w-8 items-center justify-center rounded-lg text-slate-400 hover:bg-slate-100 hover:text-slate-600 transition-colors"
        >
          <span class="material-symbols-outlined text-[20px]">edit_square</span>
        </button>

        <!-- Close -->
        <Dialog.Close
          class="flex h-8 w-8 items-center justify-center rounded-lg text-slate-400 hover:bg-red-50 hover:text-red-500 transition-colors"
          aria-label="Close"
        >
          <span class="material-symbols-outlined text-[20px]">close</span>
        </Dialog.Close>
      </div>

      <!-- ── Body: history sidebar (left) + chat (right) ───────────────────── -->
      <div class="flex flex-1 min-h-0">

        <!-- History sidebar — left column, collapsible -->
        {#if showSidebar}
          <div class="w-64 shrink-0 border-r border-slate-100 flex flex-col bg-slate-50/40">
            <div class="px-4 py-2.5 border-b border-slate-100">
              <p class="text-[0.72rem] font-bold text-slate-400 uppercase tracking-widest">Recent Chats</p>
            </div>
            <!-- New chat shortcut -->
            <button
              onclick={newChat}
              class="flex items-center gap-2 mx-3 mt-2 mb-1 px-3 py-2 rounded-xl border border-dashed border-slate-200
                     text-[0.78rem] text-slate-400 hover:bg-white hover:border-blue-300 hover:text-blue-600 transition-colors"
            >
              <span class="material-symbols-outlined text-[16px]">add</span>
              New chat
            </button>
            <div class="flex-1 overflow-y-auto py-1">
              {#if sessions.length === 0}
                <p class="px-4 py-6 text-[0.78rem] text-slate-400 text-center">No chats yet.</p>
              {:else}
                {#each sessions as s (s.id)}
                  <div class="flex items-start group hover:bg-white transition-colors mx-1 rounded-xl">
                    <button
                      onclick={() => openSession(s)}
                      class="flex-1 flex items-start gap-2 px-3 py-2.5 text-left min-w-0"
                    >
                      <span class="material-symbols-outlined text-[15px] text-slate-400 mt-0.5 shrink-0">chat_bubble</span>
                      <div class="flex-1 min-w-0">
                        <p class="text-[0.8rem] font-medium text-slate-700 truncate">{chatSessionTitle(s)}</p>
                        <p class="text-[0.68rem] text-slate-400">{relativeTime(s.updated_at)}</p>
                      </div>
                    </button>
                    <button
                      onclick={(e) => removeSession(s.id, e)}
                      class="opacity-0 group-hover:opacity-100 flex h-6 w-6 items-center justify-center rounded-md
                             text-slate-400 hover:bg-red-50 hover:text-red-500 transition-all shrink-0 my-auto mr-2"
                      title="Delete"
                    >
                      <span class="material-symbols-outlined text-[13px]">delete</span>
                    </button>
                  </div>
                {/each}
              {/if}
            </div>
          </div>
        {/if}

        <!-- ── Main chat area ─────────────────────────────────────────────── -->
        <div class="flex flex-col flex-1 min-h-0 min-w-0">

          <!-- Ollama status banners -->
          {#if ollamaStatus === null}
            <div class="mx-5 mt-3 px-3 py-2 rounded-xl bg-slate-50 border border-slate-200 flex items-center gap-2">
              <span class="material-symbols-outlined text-[16px] text-slate-400 animate-spin">progress_activity</span>
              <span class="text-[0.78rem] text-slate-500">Checking Ollama status…</span>
            </div>

          {:else if !ollamaStatus.available}
            <div class="mx-5 mt-3 px-3 py-2.5 rounded-xl bg-amber-50 border border-amber-200 flex items-start gap-2">
              <span class="material-symbols-outlined text-[17px] text-amber-500 mt-0.5 shrink-0">warning</span>
              <div class="flex-1 min-w-0">
                <p class="text-[0.78rem] font-semibold text-amber-800">Ollama is not running</p>
                <p class="text-[0.72rem] text-amber-600 mt-0.5">
                  {startingOllama ? 'Starting Ollama…' : 'Click Start or run: '}
                  {#if !startingOllama}<code class="bg-amber-100 px-1 rounded">ollama serve</code>{/if}
                </p>
              </div>
              <div class="flex gap-2 shrink-0 items-center">
                <button
                  onclick={startOllama}
                  disabled={startingOllama}
                  class="text-[0.72rem] font-semibold px-2.5 py-1 rounded-lg bg-amber-500 text-white hover:bg-amber-600 disabled:opacity-50 transition-colors"
                >{startingOllama ? '…' : 'Start'}</button>
                <button onclick={retryOllama} class="text-[0.72rem] font-semibold text-amber-700 hover:text-amber-900 underline">Retry</button>
              </div>
            </div>

          {:else if ollamaStatus.available && !ollamaStatus.model_ready}
            <div class="mx-5 mt-3 px-3 py-2.5 rounded-xl bg-blue-50 border border-blue-200 flex items-start gap-2">
              <span class="material-symbols-outlined text-[17px] text-blue-500 mt-0.5 shrink-0">download</span>
              <div class="flex-1 min-w-0">
                <p class="text-[0.78rem] font-semibold text-blue-800">DeepSeek R1 not downloaded</p>
                {#if pullProgress}
                  <p class="text-[0.72rem] text-blue-600 mt-0.5">{pullProgress.status}</p>
                  {#if pullProgress.total > 0}
                    <div class="mt-1.5 h-1.5 rounded-full bg-blue-100 overflow-hidden">
                      <div class="h-full bg-blue-500 rounded-full transition-all duration-300" style="width: {pullPct}%"></div>
                    </div>
                    <p class="text-[0.68rem] text-blue-500 mt-0.5">{pullPct}%</p>
                  {/if}
                {:else}
                  <p class="text-[0.72rem] text-blue-600 mt-0.5">~4.7 GB required</p>
                {/if}
              </div>
              {#if !pullProgress}
                <button onclick={pullModel} class="shrink-0 text-[0.72rem] font-semibold text-blue-700 hover:text-blue-900 underline">Download</button>
              {/if}
            </div>
          {/if}

          <!-- Messages -->
          <div
            bind:this={messagesEl}
            class="flex-1 overflow-y-auto px-6 py-5 space-y-4 scroll-smooth"
          >
            {#if messages.length === 0 && !isStreaming}
              <!-- Empty state -->
              <div class="flex flex-col items-center justify-center h-full text-center gap-4 pb-10">
                <div class="w-14 h-14 rounded-2xl bg-blue-50 flex items-center justify-center shadow-sm">
                  <span class="material-symbols-outlined text-[30px] text-blue-500">neurology</span>
                </div>
                <div>
                  <p class="text-[0.95rem] font-bold text-slate-700">NetMonit Assistant</p>
                  <p class="text-[0.8rem] text-slate-400 mt-1.5 max-w-sm">
                    Ask me anything — I can run diagnostics and speed tests on your behalf.
                  </p>
                </div>
                <div class="grid grid-cols-2 gap-2 w-full max-w-lg mt-1">
                  {#each [
                    { icon: 'network_check', label: 'Diagnose 8.8.8.8',        prompt: 'Diagnose connectivity to 8.8.8.8' },
                    { icon: 'speed',         label: 'Test internet speed',      prompt: 'Test my internet speed' },
                    { icon: 'analytics',     label: 'Analyze diagnostics',      prompt: 'Analyze my recent diagnostics' },
                    { icon: 'help',          label: 'Why is ping high?',        prompt: 'Why is my ping high?' },
                  ] as s}
                    <button
                      onclick={() => { inputText = s.prompt; }}
                      class="flex items-center gap-2.5 px-4 py-3 rounded-xl border border-slate-200 text-left
                             text-slate-600 hover:bg-blue-50 hover:border-blue-200 hover:text-blue-700 transition-colors"
                    >
                      <span class="material-symbols-outlined text-[18px] shrink-0 text-slate-400">{s.icon}</span>
                      <span class="text-[0.78rem] font-medium">{s.label}</span>
                    </button>
                  {/each}
                </div>
              </div>
            {:else}
              {#each messages as msg (msg.id)}
                {#if msg.role === 'user'}
                  <!-- User bubble -->
                  <div class="flex justify-end">
                    <div class="max-w-[72%] px-4 py-2.5 rounded-2xl rounded-tr-sm bg-blue-600 text-white text-[0.85rem] leading-relaxed shadow-sm">
                      {msg.content}
                    </div>
                  </div>
                {:else if msg.role === 'assistant'}
                  <!-- Assistant bubble -->
                  {@const parsed = parseAssistant(msg.content)}
                  <div class="flex justify-start gap-2.5">
                    <div class="w-7 h-7 rounded-lg bg-blue-50 flex items-center justify-center shrink-0 mt-0.5">
                      <span class="material-symbols-outlined text-[15px] text-blue-500">neurology</span>
                    </div>
                    <div class="flex-1 min-w-0 space-y-1.5">
                      {#if parsed.thinking}
                        <details class="group">
                          <summary class="text-[0.7rem] text-slate-400 cursor-pointer hover:text-slate-600 select-none list-none flex items-center gap-1">
                            <span class="material-symbols-outlined text-[13px] group-open:rotate-90 transition-transform">chevron_right</span>
                            Reasoning
                          </summary>
                          <div class="mt-1 pl-3 border-l-2 border-slate-200 text-[0.72rem] text-slate-400 italic leading-relaxed">
                            {parsed.thinking}
                          </div>
                        </details>
                      {/if}
                      <div class="px-4 py-3 rounded-2xl rounded-tl-sm bg-slate-100 text-slate-800 text-[0.85rem] leading-relaxed
                                  prose prose-sm prose-slate max-w-none
                                  prose-p:my-1 prose-ul:my-1 prose-li:my-0 prose-table:text-[0.8rem]
                                  prose-code:bg-slate-200 prose-code:px-1 prose-code:rounded prose-code:text-[0.78rem]">
                        <SvelteMarkdown source={parsed.answer} />
                      </div>
                    </div>
                  </div>
                {/if}
              {/each}

              <!-- Agent tool activity bubbles -->
              {#each toolActivities as activity (activity.id)}
                <div class="flex justify-start gap-2.5">
                  <div class="w-7 h-7 rounded-lg bg-violet-50 flex items-center justify-center shrink-0 mt-0.5">
                    <span class="material-symbols-outlined text-[14px] text-violet-500">build</span>
                  </div>
                  <div class="flex-1 min-w-0 px-4 py-2.5 rounded-2xl rounded-tl-sm bg-violet-50 border border-violet-100 text-[0.8rem] text-violet-700">
                    <div class="flex items-center gap-1.5">
                      {#if !activity.done}
                        <span class="material-symbols-outlined text-[14px] animate-spin shrink-0">progress_activity</span>
                      {:else}
                        <span class="material-symbols-outlined text-[14px] text-violet-500 shrink-0">check_circle</span>
                      {/if}
                      <span class="font-semibold">{toolLabel(activity.tool_name)}</span>
                      <span class="opacity-60">{toolArgsHint(activity.tool_name, activity.args)}</span>
                    </div>
                    {#if activity.done && activity.result}
                      <details class="mt-1.5">
                        <summary class="cursor-pointer select-none list-none flex items-center gap-0.5 text-[0.72rem] text-violet-500 hover:text-violet-800">
                          <span class="material-symbols-outlined text-[12px]">expand_more</span>
                          View result
                        </summary>
                        <pre class="mt-1.5 text-[0.68rem] bg-white/60 rounded-lg p-3 overflow-x-auto whitespace-pre-wrap leading-relaxed border border-violet-100">{activity.result}</pre>
                      </details>
                    {/if}
                  </div>
                </div>
              {/each}

              <!-- Streaming bubble -->
              {#if isStreaming}
                <div class="flex justify-start gap-2.5">
                  <div class="w-7 h-7 rounded-lg bg-blue-50 flex items-center justify-center shrink-0 mt-0.5">
                    <span class="material-symbols-outlined text-[15px] text-blue-500">neurology</span>
                  </div>
                  <div class="flex-1 min-w-0">
                    {#if streamingDelta}
                      <div class="px-4 py-3 rounded-2xl rounded-tl-sm bg-slate-100 text-slate-800 text-[0.85rem] leading-relaxed">
                        {streamingDelta}<span class="inline-block w-0.5 h-4 bg-slate-400 ml-0.5 animate-pulse align-middle"></span>
                      </div>
                    {:else if toolActivities.length === 0}
                      <div class="px-4 py-3 rounded-2xl rounded-tl-sm bg-slate-100 flex items-center gap-1.5 w-fit">
                        <span class="w-1.5 h-1.5 rounded-full bg-slate-400 animate-bounce [animation-delay:0ms]"></span>
                        <span class="w-1.5 h-1.5 rounded-full bg-slate-400 animate-bounce [animation-delay:150ms]"></span>
                        <span class="w-1.5 h-1.5 rounded-full bg-slate-400 animate-bounce [animation-delay:300ms]"></span>
                      </div>
                    {/if}
                  </div>
                </div>
              {/if}
            {/if}

            <!-- Error -->
            {#if errorMsg}
              <div class="flex justify-center">
                <div class="px-3 py-2 rounded-xl bg-red-50 border border-red-200 text-[0.75rem] text-red-600 flex items-center gap-1.5">
                  <span class="material-symbols-outlined text-[14px]">error</span>
                  {errorMsg}
                </div>
              </div>
            {/if}
          </div>

          <!-- ── Input bar ───────────────────────────────────────────────────── -->
          <div class="px-5 py-4 border-t border-slate-100 shrink-0 bg-white/60 backdrop-blur-sm">
            <div class="flex items-end gap-2.5 rounded-2xl border border-slate-200 bg-slate-50/80 px-4 py-2.5
                        focus-within:border-blue-400 focus-within:ring-2 focus-within:ring-blue-500/20 transition-all">
              <textarea
                bind:value={inputText}
                onkeydown={handleKeydown}
                disabled={isStreaming}
                placeholder={!ollamaStatus?.available ? 'Ollama is not running — click Start above…' : 'Ask about your network… (e.g. "cek koneksi ke 8.8.8.8")'}
                rows={1}
                class="flex-1 resize-none bg-transparent text-[0.85rem] text-slate-800 placeholder:text-slate-400
                       outline-none leading-relaxed max-h-36 overflow-y-auto disabled:opacity-50
                       field-sizing-content"
              ></textarea>

              {#if isStreaming}
                <button
                  onclick={stopStream}
                  class="shrink-0 flex h-9 w-9 items-center justify-center rounded-xl bg-red-100 text-red-500 hover:bg-red-200 transition-colors"
                  title="Stop"
                >
                  <span class="material-symbols-outlined text-[18px]">stop</span>
                </button>
              {:else}
                <button
                  onclick={sendMessage}
                  disabled={!canSend}
                  class="shrink-0 flex h-9 w-9 items-center justify-center rounded-xl transition-colors
                         {canSend ? 'bg-blue-600 text-white hover:bg-blue-700 shadow-sm' : 'bg-slate-200 text-slate-400 cursor-not-allowed'}"
                  title="Send (Enter)"
                >
                  <span class="material-symbols-outlined text-[18px]">arrow_upward</span>
                </button>
              {/if}
            </div>
            <p class="text-[0.65rem] text-slate-400 mt-1.5 text-center">
              Enter to send · Shift+Enter for new line
            </p>
          </div>

        </div>
      </div>

    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>
