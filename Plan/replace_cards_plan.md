# Plan: Modernize Speed Test Stat Cards

## 1. State Variables Modification (`+page.svelte` script block)
We need to capture max download, max upload, and best ping values during the test.

**Add/modify state variables:**
```svelte
let dlSpeed = $state(0);
let ulSpeed = $state(0);
let maxDlSpeed = $state(0);
let maxUlSpeed = $state(0);
let bestPing = $state(0);
```

**Reset variables in `toggleRun()`:**
```js
dlSpeed = 0;
ulSpeed = 0;
maxDlSpeed = 0;
maxUlSpeed = 0;
bestPing = 0;
// Reset existing values...
```

**Update values in `EventsOn('speedtest:update')`:**
```js
if (update.phase === 'download') {
    dlSpeed = update.speed; // Current/Average
    if (update.speed > maxDlSpeed) maxDlSpeed = update.speed;
} else if (update.phase === 'upload') {
    ulSpeed = update.speed; // Current/Average
    if (update.speed > maxUlSpeed) maxUlSpeed = update.speed;
}

if (update.ping > 0) {
    pingVal = update.ping;
    if (bestPing === 0 || update.ping < bestPing) bestPing = update.ping;
}
if (update.jitter > 0) jitterVal = update.jitter;
```

## 2. Card UI Replacement (`+page.svelte` markup)
Replace the 3 existing cards (Latency, Jitter, Loss) with the 3 new requested cards (Download, Upload, Latency), while maintaining the exact same premium UI structure (icons, gradients, shadows, Layout).

### Card 1: Download
* Icon: `download` or `arrow_downward`
* Title: `Download`
* Right-Aligned Small Text: `Average {dlSpeed} Mbps`
* Center Big Text: `{maxDlSpeed} Mbps`
* Subtitle (Bottom): `(Nilai terbesar)`

### Card 2: Upload
* Icon: `upload` or `arrow_upward`
* Title: `Upload`
* Right-Aligned Small Text: `Average {ulSpeed} Mbps`
* Center Big Text: `{maxUlSpeed} Mbps`
* Subtitle (Bottom): `(Nilai terbesar)`

### Card 3: Latency
* Icon: `speed` or `network_ping`
* Title: `Latency`
* Line 1 Small Text: `Average {pingVal} ms`
* Line 2 Small Text: `Jitter {jitterVal} ms`
* Center Big Text: `{bestPing} ms`
* Subtitle (Bottom): `(Hasil terbaik)`

## 3. Functioning and Accuracy Check
- Values will update in real-time as the Wails backend emits `speedtest:update` events.
- Once the phase is 'done', `dlSpeed` and `ulSpeed` will snap to the final official computed averages from the backend, effectively making the display highly accurate at test conclusion.

This ensures all requested data parameters fit cleanly into the same premium visual footprint while giving more comprehensive details (Peak vs Average).
