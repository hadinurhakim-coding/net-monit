<script lang="ts">
	import './layout.css';
	import { page } from '$app/state';
	import { WindowMinimise, WindowToggleMaximise, Quit } from '../../wailsjs/runtime/runtime';
	import ChatDialog from '$lib/components/ChatDialog.svelte';
	let { children } = $props();
	let isCollapsed = $state(false);
	let isMaximised = $state(false);
	let chatOpen = $state(false);

	let pageTitle = $derived.by(() => {
		const path = page.url.pathname;
		if (path === '/dashboard') return 'Dashboard';
		if (path === '/diagnostics') return 'Diagnostics';
		if (path === '/speedtest') return 'Speed Test';
		if (path === '/history') return 'History';
		if (path === '/settings') return 'Settings';
		return 'Overview';
	});

	function toggleMaximise() {
		WindowToggleMaximise();
		isMaximised = !isMaximised;
	}
</script>

<div class="flex flex-col h-screen w-full bg-[#f8f9fc] overflow-hidden text-[#1e293b] antialiased font-sans selection:bg-blue-100">

	<!-- ── Custom Title Bar ── -->
	<div
		class="h-10 bg-white border-b border-slate-200/70 flex items-center px-3 gap-3 shrink-0 z-50 select-none"
		style="--wails-draggable: drag"
	>
		<!-- Left: Logo + App Name -->
		<div class="flex items-center gap-2 w-48 shrink-0" style="--wails-draggable: no-drag">
			<img src="/logo.png" alt="NetMonit" class="w-6 h-6 rounded-md object-contain shrink-0" />
			<span class="font-extrabold text-[0.82rem] text-slate-800 tracking-tight leading-none">NetMonit</span>
		</div>

		<!-- Center: Search -->
		<div class="flex-1 flex justify-center" style="--wails-draggable: no-drag">
			<div class="relative w-full max-w-sm">
				<span class="material-symbols-outlined absolute left-2.5 top-1/2 -translate-y-1/2 text-[15px] text-slate-400 pointer-events-none">search</span>
				<input
					type="text"
					placeholder="Search hosts, history..."
					class="w-full pl-8 pr-3 py-1 text-[0.78rem] rounded-lg border border-slate-200 bg-slate-50 focus:outline-none focus:ring-1 focus:ring-blue-400/60 focus:border-blue-400 placeholder:text-slate-400 transition-colors"
				/>
			</div>
		</div>

		<!-- Right: Window Controls -->
		<div class="flex items-center gap-0.5 ml-auto shrink-0" style="--wails-draggable: no-drag">
			<button
				onclick={() => WindowMinimise()}
				class="w-8 h-8 flex items-center justify-center rounded-lg text-slate-400 hover:bg-slate-100 hover:text-slate-700 transition-colors"
				aria-label="Minimize"
			>
				<span class="material-symbols-outlined text-[18px]">remove</span>
			</button>
			<button
				onclick={toggleMaximise}
				class="w-8 h-8 flex items-center justify-center rounded-lg text-slate-400 hover:bg-slate-100 hover:text-slate-700 transition-colors"
				aria-label="Maximise"
			>
				<span class="material-symbols-outlined text-[18px]">{isMaximised ? 'filter_none' : 'crop_square'}</span>
			</button>
			<button
				onclick={() => Quit()}
				class="w-8 h-8 flex items-center justify-center rounded-lg text-slate-400 hover:bg-red-500 hover:text-white transition-colors"
				aria-label="Close"
			>
				<span class="material-symbols-outlined text-[18px]">close</span>
			</button>
		</div>
	</div>

	<!-- ── Main Layout ── -->
	<div class="flex flex-1 overflow-hidden">

		<!-- Sidebar -->
		<aside class="border-r border-slate-100 bg-white flex flex-col h-full relative transition-all duration-300 ease-in-out {isCollapsed ? 'w-22' : 'w-65'} shadow-[2px_0_10px_rgba(0,0,0,0.02)] z-20">
			<!-- Collapse Toggle -->
			<button
				onclick={() => isCollapsed = !isCollapsed}
				class="absolute -right-3 top-9 z-10 flex h-6 w-6 items-center justify-center rounded-full border border-slate-200 bg-white shadow-sm text-slate-400 hover:text-slate-700 cursor-pointer outline-none hover:bg-slate-50 transition-colors focus-visible:ring-2 focus-visible:ring-blue-500"
			>
				<span class="material-symbols-outlined text-[16px] transition-transform duration-300 {isCollapsed ? 'rotate-180' : ''}">
					chevron_left
				</span>
			</button>

			<!-- Logo Area -->
			<div class="h-16 flex items-center {isCollapsed ? 'justify-center' : 'px-6'} transition-all overflow-hidden whitespace-nowrap">
				{#if !isCollapsed}
					<div class="flex flex-col">
						<span class="font-extrabold text-[1.05rem] leading-none text-slate-900 tracking-tight">NetMonit</span>
						<span class="text-[0.52rem] tracking-[0.2em] text-slate-500 font-bold mt-1">NETWORK INTELLIGENCE</span>
					</div>
				{/if}
			</div>

			<!-- Navigation -->
			<nav class="flex-1 py-2 px-4 space-y-1.5 overflow-hidden transition-all overflow-y-auto">
				<a href="/dashboard" class="flex items-center gap-3.5 rounded-xl py-3 px-3.5 text-[0.9rem] font-bold transition-all {page.url.pathname === '/dashboard' ? 'bg-blue-50 text-blue-700' : 'text-slate-500 hover:bg-slate-50 hover:text-slate-700'} {isCollapsed ? 'justify-center px-0!' : ''}">
					<span class="material-symbols-outlined text-[20px]">grid_view</span>
					{#if !isCollapsed}<span class="whitespace-nowrap">Dashboard</span>{/if}
				</a>
				<a href="/diagnostics" class="flex items-center gap-3.5 rounded-xl py-3 px-3.5 text-[0.9rem] font-bold transition-all {page.url.pathname === '/diagnostics' ? 'bg-blue-50 text-blue-700' : 'text-slate-500 hover:bg-slate-50 hover:text-slate-700'} {isCollapsed ? 'justify-center px-0!' : ''}">
					<span class="material-symbols-outlined text-[20px]">science</span>
					{#if !isCollapsed}<span class="whitespace-nowrap">Diagnostics</span>{/if}
				</a>
				<a href="/speedtest" class="flex items-center gap-3.5 rounded-xl py-3 px-3.5 text-[0.9rem] font-bold transition-all {page.url.pathname === '/speedtest' ? 'bg-blue-50 text-blue-700 shadow-sm' : 'text-slate-500 hover:bg-slate-50 hover:text-slate-700'} {isCollapsed ? 'justify-center px-0!' : ''}">
					<span class="material-symbols-outlined text-[20px]">speed</span>
					{#if !isCollapsed}<span class="whitespace-nowrap">Speed Test</span>{/if}
				</a>
				<a href="/history" class="flex items-center gap-3.5 rounded-xl py-3 px-3.5 text-[0.9rem] font-bold transition-all {page.url.pathname === '/history' ? 'bg-blue-50 text-blue-700' : 'text-slate-500 hover:bg-slate-50 hover:text-slate-700'} {isCollapsed ? 'justify-center px-0!' : ''}">
					<span class="material-symbols-outlined text-[20px]">history</span>
					{#if !isCollapsed}<span class="whitespace-nowrap">History</span>{/if}
				</a>
			</nav>

			<!-- Bottom Settings -->
			<div class="p-4 mt-auto border-t border-slate-100/60 bg-slate-50/30">
				<a href="/settings" class="flex items-center gap-3.5 rounded-xl py-3 px-3.5 text-[0.9rem] font-bold transition-all {page.url.pathname === '/settings' ? 'bg-blue-50 text-blue-700' : 'text-slate-500 hover:bg-slate-50 hover:text-slate-700'} {isCollapsed ? 'justify-center px-0!' : ''}">
					<span class="material-symbols-outlined text-[20px]">settings</span>
					{#if !isCollapsed}<span class="whitespace-nowrap">Settings</span>{/if}
				</a>
				{#if !isCollapsed}
					<p class="text-[0.55rem] text-slate-400 text-center mt-3 tracking-wide select-none">
						© Developed by <span class="font-bold text-slate-500">Prof Kim</span>
					</p>
				{/if}
			</div>
		</aside>

		<!-- Content Area -->
		<div class="flex-1 flex flex-col h-full overflow-hidden relative">
			<!-- Page Header -->
			<header class="h-12 flex items-center justify-between px-8 shrink-0 z-10 border-b border-slate-200/50 bg-white/40 backdrop-blur-md">
				<h1 class="text-xl font-bold text-slate-800 tracking-tight">{pageTitle}</h1>
				<div class="flex items-center gap-5">
					<button class="relative text-slate-500 hover:text-slate-700 transition-colors cursor-pointer outline-none w-10 h-10 flex items-center justify-center hover:bg-slate-100 rounded-full">
						<span class="material-symbols-outlined text-[22px]">notifications</span>
						<span class="absolute top-2 right-2 w-2.5 h-2.5 bg-blue-600 border-2 border-white rounded-full"></span>
					</button>
					<div class="flex items-center gap-3.5 pl-5 border-l border-slate-200 h-10 cursor-pointer group">
						<div class="flex flex-col items-end pt-0.5">
							<span class="text-[0.85rem] font-bold text-slate-800 leading-none group-hover:text-blue-600 transition-colors">Admin User</span>
							<span class="text-[0.70rem] text-slate-500 font-medium mt-1">Network Admin</span>
						</div>
						<div class="w-10 h-10 rounded-full bg-slate-200 border-2 border-white shadow-sm overflow-hidden flex items-center justify-center shrink-0">
							<img src="https://i.pravatar.cc/150?img=11" alt="Avatar" class="w-full h-full object-cover text-xs text-transparent">
						</div>
					</div>
				</div>
			</header>

			<!-- Page Content -->
			<main class="flex-1 overflow-auto relative">
				<div class="px-8 pt-2 pb-4 h-full max-w-7xl mx-auto">
					{@render children()}
				</div>
			</main>
		</div>
	</div>
</div>

<!-- Floating AI chat button — bottom-right corner -->
<button
	onclick={() => chatOpen = true}
	title="Open AI Assistant"
	class="fixed bottom-6 right-6 z-40 w-13 h-13 rounded-full shadow-lg hover:shadow-xl
	       transition-all duration-200 hover:scale-110 active:scale-95 outline-none
	       focus-visible:ring-2 focus-visible:ring-blue-500 focus-visible:ring-offset-2
	       overflow-hidden border-2 border-white"
	aria-label="Open AI Assistant"
>
	<img src="/logo.png" alt="AI Chat" class="w-full h-full object-cover" />
</button>

<ChatDialog bind:open={chatOpen} />
