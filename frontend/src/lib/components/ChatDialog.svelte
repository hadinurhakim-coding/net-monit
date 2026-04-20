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
      if (activeSession) {
        GetChatSession(activeSession.id).then(s => {
          if (s) activeSession = s;
        });
      }
      loadSessions();
      tick().then(scrollToBottom);
    });

    const offStatus = EventsOn('chat:ollama_status', (s: OllamaStatus) => {
      ollamaStatus = s;
    });

    const offPull = EventsOn('chat:pull_progress', (p: PullProgress) => {
      pullProgress = p;
      if (p.status === 'success') pullProgress = null;
    });

    return () => { offChunk(); offStatus(); offPull(); };
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

  // Pull progress percentage
  let pullPct = $derived(
    pullProgress && pullProgress.total > 0
      ? Math.round((pullProgress.completed / pullProgress.total) * 100)
      : 0
  );
</script>

<Dialog.Root bind:open>
  <Dialog.Portal>
    <!-- Dim overlay (only on mobile-ish; desktop keeps sidebar visible) -->
    <Dialog.Overlay
      class="fixed inset-0 z-40 bg-black/20 backdrop-blur-[2px]
             data-[state=open]:animate-in data-[state=closed]:animate-out
             data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 duration-200"
    />

    <!-- Side panel -->
    <Dialog.Content
      class="fixed right-0 top-0 z-50 h-full w-110 bg-white border-l border-slate-100
             shadow-2xl flex flex-col outline-none
             data-[state=open]:animate-in data-[state=closed]:animate-out
             data-[state=open]:slide-in-from-right data-[state=closed]:slide-out-to-right
             duration-250"
    >

      <!-- ── Header ─────────────────────────────────────────────────────────── -->
      <div class="flex items-center gap-2 px-4 py-3 border-b border-slate-100 shrink-0">
        <!-- History toggle -->
        <button
          onclick={() => showSidebar = !showSidebar}
          class="flex h-8 w-8 items-center justify-center rounded-lg text-slate-400 hover:bg-slate-100 hover:text-slate-600 transition-colors"
          title="Chat history"
        >
          <span class="material-symbols-outlined text-[20px]">history</span>
        </button>

        <div class="flex-1 min-w-0">
          <p class="text-[0.82rem] font-extrabold text-slate-800 tracking-tight leading-none truncate">
            {activeSession ? chatSessionTitle(activeSession) : 'NetMonit Assistant'}
          </p>
          <p class="text-[0.68rem] text-slate-400 mt-0.5">Powered by DeepSeek R1 · DeBERTa</p>
        </div>

        <!-- New chat -->
        <button
          onclick={newChat}
          class="flex h-8 w-8 items-center justify-center rounded-lg text-slate-400 hover:bg-slate-100 hover:text-slate-600 transition-colors"
          title="New chat"
        >
          <span class="material-symbols-outlined text-[20px]">edit_square</span>
        </button>

        <!-- Close -->
        <Dialog.Close
          class="flex h-8 w-8 items-center justify-center rounded-lg text-slate-400 hover:bg-slate-100 hover:text-slate-600 transition-colors"
          aria-label="Close"
        >
          <span class="material-symbols-outlined text-[20px]">close</span>
        </Dialog.Close>
      </div>

      <!-- ── Body (history sidebar OR chat) ────────────────────────────────── -->
      <div class="flex flex-1 min-h-0">

        <!-- History sidebar (slide over chat) -->
        {#if showSidebar}
        <div class="absolute inset-0 top-13.25 z-10 bg-white flex flex-col">
          <div class="px-4 py-2.5 border-b border-slate-100">
            <p class="text-[0.75rem] font-bold text-slate-500 uppercase tracking-widest">Recent Chats</p>
          </div>
          <div class="flex-1 overflow-y-auto py-1">
            {#if sessions.length === 0}
              <p class="px-4 py-6 text-[0.8rem] text-slate-400 text-center">No chats yet.</p>
            {:else}
              {#each sessions as s (s.id)}
                <div class="flex items-start group hover:bg-slate-50 transition-colors">
                  <button
                    onclick={() => openSession(s)}
                    class="flex-1 flex items-start gap-2 px-4 py-2.5 text-left min-w-0"
                  >
                    <span class="material-symbols-outlined text-[16px] text-slate-400 mt-0.5 shrink-0">chat_bubble</span>
                    <div class="flex-1 min-w-0">
                      <p class="text-[0.82rem] font-medium text-slate-700 truncate">{chatSessionTitle(s)}</p>
                      <p class="text-[0.7rem] text-slate-400">{relativeTime(s.updated_at)}</p>
                    </div>
                  </button>
                  <button
                    onclick={(e) => removeSession(s.id, e)}
                    class="opacity-0 group-hover:opacity-100 flex h-6 w-6 items-center justify-center rounded-md text-slate-400 hover:bg-red-50 hover:text-red-500 transition-all shrink-0 my-auto mr-3"
                    title="Delete"
                  >
                    <span class="material-symbols-outlined text-[14px]">delete</span>
                  </button>
                </div>
              {/each}
            {/if}
          </div>
        </div>
        {/if}

        <!-- Main chat area -->
        <div class="flex flex-col flex-1 min-h-0">

          <!-- Ollama status banners -->
          {#if ollamaStatus === null}
            <div class="mx-4 mt-3 px-3 py-2 rounded-xl bg-slate-50 border border-slate-200 flex items-center gap-2">
              <span class="material-symbols-outlined text-[16px] text-slate-400 animate-spin">progress_activity</span>
              <span class="text-[0.78rem] text-slate-500">Checking Ollama status…</span>
            </div>

          {:else if !ollamaStatus.available}
            <div class="mx-4 mt-3 px-3 py-2.5 rounded-xl bg-amber-50 border border-amber-200 flex items-start gap-2">
              <span class="material-symbols-outlined text-[17px] text-amber-500 mt-0.5 shrink-0">warning</span>
              <div class="flex-1 min-w-0">
                <p class="text-[0.78rem] font-semibold text-amber-800">Ollama is not running</p>
                <p class="text-[0.72rem] text-amber-600 mt-0.5">
                  {startingOllama ? 'Starting Ollama…' : 'Click Start or run: '}
                  {#if !startingOllama}
                    <code class="bg-amber-100 px-1 rounded">ollama serve</code>
                  {/if}
                </p>
              </div>
              <div class="flex flex-col gap-1 shrink-0">
                <button
                  onclick={startOllama}
                  disabled={startingOllama}
                  class="text-[0.72rem] font-semibold px-2 py-0.5 rounded-lg bg-amber-500 text-white hover:bg-amber-600 disabled:opacity-50 transition-colors"
                >
                  {startingOllama ? '…' : 'Start'}
                </button>
                <button
                  onclick={retryOllama}
                  class="text-[0.72rem] font-semibold text-amber-700 hover:text-amber-900 underline"
                >Retry</button>
              </div>
            </div>

          {:else if ollamaStatus.available && !ollamaStatus.model_ready}
            <div class="mx-4 mt-3 px-3 py-2.5 rounded-xl bg-blue-50 border border-blue-200 flex items-start gap-2">
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
                <button
                  onclick={pullModel}
                  class="shrink-0 text-[0.72rem] font-semibold text-blue-700 hover:text-blue-900 underline"
                >Download</button>
              {/if}
            </div>
          {/if}

          <!-- Messages -->
          <div
            bind:this={messagesEl}
            class="flex-1 overflow-y-auto px-4 py-4 space-y-4 scroll-smooth"
          >
            {#if messages.length === 0 && !isStreaming}
              <!-- Empty state -->
              <div class="flex flex-col items-center justify-center h-full text-center gap-3 pb-8">
                <div class="w-12 h-12 rounded-2xl bg-blue-50 flex items-center justify-center">
                  <span class="material-symbols-outlined text-[26px] text-blue-500">neurology</span>
                </div>
                <div>
                  <p class="text-[0.88rem] font-bold text-slate-700">Ask about your network</p>
                  <p class="text-[0.75rem] text-slate-400 mt-1 max-w-60">
                    Run a speed test or diagnostics first, then ask me to analyze the results.
                  </p>
                </div>
                <div class="flex flex-col gap-1.5 w-full max-w-65">
                  {#each ['Analyze my recent diagnostics', 'Why is my ping high?', 'Is my internet connection stable?'] as prompt}
                    <button
                      onclick={() => { inputText = prompt; }}
                      class="text-[0.75rem] text-left px-3 py-2 rounded-xl border border-slate-200 text-slate-600 hover:bg-blue-50 hover:border-blue-200 hover:text-blue-700 transition-colors"
                    >
                      {prompt}
                    </button>
                  {/each}
                </div>
              </div>
            {:else}
              {#each messages as msg (msg.id)}
                {#if msg.role === 'user'}
                  <!-- User bubble -->
                  <div class="flex justify-end">
                    <div class="max-w-[78%] px-3.5 py-2.5 rounded-2xl rounded-tr-sm bg-blue-600 text-white text-[0.83rem] leading-relaxed shadow-sm">
                      {msg.content}
                    </div>
                  </div>
                {:else if msg.role === 'assistant'}
                  <!-- Assistant bubble -->
                  {@const parsed = parseAssistant(msg.content)}
                  <div class="flex justify-start">
                    <div class="max-w-[92%] space-y-1.5">
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
                      <div class="px-3.5 py-2.5 rounded-2xl rounded-tl-sm bg-slate-100 text-slate-800 text-[0.83rem] leading-relaxed
                                  prose prose-sm prose-slate max-w-none
                                  prose-p:my-1 prose-ul:my-1 prose-li:my-0 prose-table:text-[0.78rem]
                                  prose-code:bg-slate-200 prose-code:px-1 prose-code:rounded prose-code:text-[0.78rem]">
                        <SvelteMarkdown source={parsed.answer} />
                      </div>
                    </div>
                  </div>
                {/if}
              {/each}

              <!-- Streaming bubble -->
              {#if isStreaming}
                <div class="flex justify-start">
                  <div class="max-w-[92%]">
                    {#if streamingDelta}
                      <div class="px-3.5 py-2.5 rounded-2xl rounded-tl-sm bg-slate-100 text-slate-800 text-[0.83rem] leading-relaxed">
                        {streamingDelta}<span class="inline-block w-0.5 h-4 bg-slate-400 ml-0.5 animate-pulse align-middle"></span>
                      </div>
                    {:else}
                      <div class="px-3.5 py-2.5 rounded-2xl rounded-tl-sm bg-slate-100 flex items-center gap-1.5">
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

          <!-- ── Input bar ──────────────────────────────────────────────────── -->
          <div class="px-4 py-3 border-t border-slate-100 shrink-0">
            <div class="flex items-end gap-2 rounded-2xl border border-slate-200 bg-slate-50 px-3 py-2
                        focus-within:border-blue-400 focus-within:ring-2 focus-within:ring-blue-500/20 transition-all">
              <textarea
                bind:value={inputText}
                onkeydown={handleKeydown}
                disabled={isStreaming}
                placeholder={!ollamaStatus?.available ? 'Ollama is not running — click Start above…' : 'Ask about your network…'}
                rows={1}
                class="flex-1 resize-none bg-transparent text-[0.83rem] text-slate-800 placeholder:text-slate-400
                       outline-none leading-relaxed max-h-32 overflow-y-auto disabled:opacity-50
                       field-sizing-content"
              ></textarea>

              {#if isStreaming}
                <button
                  onclick={stopStream}
                  class="shrink-0 flex h-8 w-8 items-center justify-center rounded-xl bg-red-100 text-red-500 hover:bg-red-200 transition-colors"
                  title="Stop"
                >
                  <span class="material-symbols-outlined text-[18px]">stop</span>
                </button>
              {:else}
                <button
                  onclick={sendMessage}
                  disabled={!canSend}
                  class="shrink-0 flex h-8 w-8 items-center justify-center rounded-xl transition-colors
                         {canSend ? 'bg-blue-600 text-white hover:bg-blue-700' : 'bg-slate-200 text-slate-400 cursor-not-allowed'}"
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
