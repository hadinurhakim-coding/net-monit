<script lang="ts">
  import { geoNaturalEarth1, geoPath } from 'd3-geo';
  import { feature, mesh } from 'topojson-client';
  // @ts-ignore
  import topoData from 'world-atlas/countries-110m.json';

  let {
    lat = 0,
    lon = 0,
    city = '',
    country = '',
  }: {
    lat?: number;
    lon?: number;
    city?: string;
    country?: string;
  } = $props();

  const W = 960;
  const H = 500;

  const projection = geoNaturalEarth1()
    .scale(153)
    .translate([W / 2, H / 2]);

  const pathGen = geoPath(projection);

  // Pre-compute static paths (land fill + interior country borders)
  const topo = topoData as any;
  const landPath   = pathGen(feature(topo, topo.objects.land) as any)   ?? '';
  const bordersPath = pathGen(
    mesh(topo, topo.objects.countries, (a: any, b: any) => a !== b) as any
  ) ?? '';

  // Graticule lines (equator, tropics, polar circles, meridians)
  const hLines = [-66.5, -23.5, 0, 23.5, 66.5];
  const vLines = [-120, -60, 0, 60, 120];

  const hLineSvg = hLines.map(lat => {
    const y = projection([0, lat])?.[1] ?? 0;
    return { y, equator: lat === 0 };
  });
  const vLineSvg = vLines.map(lon => {
    const x = projection([lon, 0])?.[0] ?? 0;
    return { x, prime: lon === 0 };
  });

  // Reactive: marker SVG coords from real lat/lon
  let markerPos = $derived.by(() => {
    if (!city || city === '...' || city === 'Unknown') return null;
    return projection([lon, lat]) ?? null;
  });
</script>

<div class="relative h-40 w-full rounded-xl overflow-hidden border border-slate-200/60 bg-[#c8dff0]">
  <svg
    class="absolute inset-0 w-full h-full"
    viewBox="0 0 {W} {H}"
    preserveAspectRatio="xMidYMid slice"
    aria-hidden="true"
  >
    <!-- Ocean -->
    <rect width={W} height={H} fill="#c8dff0" />

    <!-- Land masses -->
    <path d={landPath} fill="#d6e6c3" />

    <!-- Country borders (interior only) -->
    <path d={bordersPath} fill="none" stroke="#b8ccaa" stroke-width="0.7" />

    <!-- Graticule: horizontal lat lines -->
    {#each hLineSvg as line}
      <line
        x1="0" y1={line.y} x2={W} y2={line.y}
        stroke="#94a3b8"
        stroke-width={line.equator ? 0.9 : 0.45}
        stroke-dasharray={line.equator ? 'none' : '5 5'}
        opacity={line.equator ? 0.45 : 0.25}
      />
    {/each}

    <!-- Graticule: vertical lon lines -->
    {#each vLineSvg as line}
      <line
        x1={line.x} y1="0" x2={line.x} y2={H}
        stroke="#94a3b8"
        stroke-width={line.prime ? 0.9 : 0.45}
        stroke-dasharray={line.prime ? 'none' : '5 5'}
        opacity={line.prime ? 0.45 : 0.25}
      />
    {/each}

    <!-- Location marker (only when we have real coordinates) -->
    {#if markerPos}
      <!-- Outer pulse ring -->
      <circle cx={markerPos[0]} cy={markerPos[1]} r="6" fill="#2563eb" opacity="0.2">
        <animate attributeName="r"       values="6;22;6"       dur="2.2s" repeatCount="indefinite" />
        <animate attributeName="opacity" values="0.35;0;0.35"  dur="2.2s" repeatCount="indefinite" />
      </circle>
      <!-- Inner pulse ring -->
      <circle cx={markerPos[0]} cy={markerPos[1]} r="4" fill="#2563eb" opacity="0.3">
        <animate attributeName="r"       values="4;13;4"       dur="2.2s" begin="0.35s" repeatCount="indefinite" />
        <animate attributeName="opacity" values="0.45;0;0.45"  dur="2.2s" begin="0.35s" repeatCount="indefinite" />
      </circle>
      <!-- Core dot with white stroke -->
      <circle cx={markerPos[0]} cy={markerPos[1]} r="4.5" fill="#1d4ed8" stroke="white" stroke-width="2" />
    {/if}
  </svg>

  <!-- Location badge -->
  <div class="absolute bottom-2.5 left-1/2 -translate-x-1/2 z-10 pointer-events-none">
    <div class="bg-white/80 backdrop-blur-sm px-3 py-1 rounded-full border border-white/80 shadow-sm flex items-center gap-1.5 whitespace-nowrap">
      <span class="w-1.5 h-1.5 bg-green-500 rounded-full animate-pulse shrink-0"></span>
      <span class="text-[0.58rem] font-bold tracking-wider text-slate-600">
        {city && city !== '...' && city !== 'Unknown'
          ? `${city}, ${country}`
          : 'ONLINE'}
      </span>
    </div>
  </div>
</div>
