# Plan: LibreSpeed Dynamic Server Selection

## Alur
1. User buka dialog → frontend panggil `GetLibreSpeedServers()`
2. Backend fetch `https://librespeed.org/servers-cli.json`
3. Ping semua server **paralel** (timeout 3s), filter yang respond
4. Return daftar ke frontend → tampil dengan search
5. User pilih server → `StartLibreSpeedTest(name, country, baseURL, dlURL, ulURL)`
6. Runner gunakan LibreSpeed protocol (garbage.php / empty.php)

## Files

| File | Aksi |
|------|------|
| `librespeed.go` | Baru — struct + fetch + parallel ping |
| `speedtest.go` | Tambah LibreSpeed runner support |
| `app.go` | Tambah `GetLibreSpeedServers()` + `StartLibreSpeedTest()` |
| `wails generate module` | Regenerate bindings |
| `ServerDialog.svelte` | Search input + loading state + dynamic fetch |
| `speedtest/+page.svelte` | Handle LibreSpeed server selection |

---

## Step 1 — librespeed.go

```go
type LibreSpeedServer struct {
    ID      string `json:"id"`
    Name    string `json:"name"`
    Server  string `json:"server"`   // base URL
    DlURL   string `json:"dlURL"`
    UlURL   string `json:"ulURL"`
    PingURL string `json:"pingURL"`
    Country string `json:"country"`  // ISO 3166-1 alpha-2
}

func FetchAndFilterLibreSpeedServers() ([]LibreSpeedServer, error) {
    // 1. GET https://librespeed.org/servers-cli.json (10s timeout)
    // 2. Parse JSON array
    // 3. Normalize URLs: "//" prefix → "https://"
    // 4. Ping setiap server paralel (goroutine per server, timeout 3s)
    //    → GET {server}/{pingURL}
    // 5. Collect yang respond, return sorted by name
}
```

URL normalization:
```go
func normalizeURL(u string) string {
    if strings.HasPrefix(u, "//") {
        return "https:" + u
    }
    return u
}
```

---

## Step 2 — speedtest.go: LibreSpeed Runner

Tambah method baru di `SpeedtestRunner` — gunakan `server.isLibreSpeed` flag:

```go
// runDownload() — LibreSpeed path:
// Loop GET {baseURL}/{dlURL}?ckSize=25 sampai context expired

// runUpload() — LibreSpeed path:
// Loop POST {baseURL}/{ulURL} dengan body random 25MB sampai context expired
```

Tambah field di `speedServerConfig`:
```go
type speedServerConfig struct {
    // ... existing fields ...
    isLibreSpeed bool
    lsBaseURL    string
    lsDlURL      string
    lsUlURL      string
}
```

---

## Step 3 — app.go

```go
func (a *App) GetLibreSpeedServers() ([]LibreSpeedServer, error) {
    return FetchAndFilterLibreSpeedServers()
}

func (a *App) StartLibreSpeedTest(name, country, baseURL, dlURL, ulURL string) error {
    // Guard: cek stRunner tidak nil
    // Build speedServerConfig dengan isLibreSpeed=true
    // Jalankan runner seperti StartSpeedtest biasa
}
```

---

## Step 4 — ServerDialog.svelte

```
┌─────────────────────────────────┐
│ Select Server Location          │
│ ┌─────────────────────────────┐ │
│ │ 🔍 Search server...         │ │
│ └─────────────────────────────┘ │
│                                  │
│ MANUAL SERVERS                   │
│ [🌐 Cloudflare - Nearest]       │  ← static list (existing)
│ [🇸🇬 Cloudflare - Singapore]   │
│ ...                              │
│                                  │
│ LIBRESPEED SERVERS               │
│ [loading spinner...]             │  ← saat fetch
│ [🇮🇩 Server Name - ID]         │  ← muncul satu per satu
│ ...                              │
└─────────────────────────────────┘
```

- Search filter kedua grup sekaligus
- Flag dari country code: `countryToFlag("ID")` → 🇮🇩
- Loading state saat `GetLibreSpeedServers()` berjalan
- Jika tidak ada hasil: "No servers found"

---

## Step 5 — speedtest/+page.svelte

Tambah state:
```typescript
let lsServers = $state<LibreSpeedServer[]>([]);
let lsLoading = $state(false);

// Saat user pilih server LibreSpeed:
await StartLibreSpeedTest(s.name, s.country, s.server, s.dlURL, s.ulURL);
```

Deteksi jenis server dari `selectedServer` — tambah field `source: 'static' | 'librespeed'`.

---

## Urutan Implementasi

1. `librespeed.go` — struct + fetch + ping
2. `speedtest.go` — LibreSpeed download/upload path
3. `app.go` — 2 method baru
4. `go build ./...`
5. `wails generate module`
6. `ServerDialog.svelte` — search + sections + loading
7. `speedtest/+page.svelte` — integrasi
8. `bun run check`
9. `wails build`
