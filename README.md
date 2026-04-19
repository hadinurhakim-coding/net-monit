<img width="1919" height="1018" alt="image" src="https://github.com/user-attachments/assets/aeedead6-91c6-4ec9-85e2-da117cc76dce" /><img width="1919" height="1018" alt="image" src="https://github.com/user-attachments/assets/45090967-5054-4bd9-b2a9-5addfd3b2bf4" /># NetMonit — Network Intelligence

> Aplikasi desktop monitoring jaringan berbasis Go + Svelte, dibangun dengan Wails.

<img width="1919" height="1018" alt="image" src="https://github.com/user-attachments/assets/66f18d19-01eb-42ee-97f5-074fcedea0f2" />


![Speed Test Screenshot](docs/speedtest-screenshot.png)

---

## Fitur Utama

NetMonit menyediakan alat diagnostik jaringan yang biasanya hanya tersedia via CLI, dikemas dalam antarmuka desktop yang modern dan intuitif.

---

## Halaman & Fitur

### Dashboard
Tampilan ringkasan kondisi jaringan secara real-time.

- **Network Health** — persentase keberhasilan tes secara keseluruhan
- **Average Latency** — rata-rata latensi dari tes terbaru
- **Total Outages** — jumlah gangguan terdeteksi (packet loss tinggi / tes gagal)
- **Active Endpoints** — jumlah host unik yang pernah diuji
- **Connection Stability Chart** — grafik batang latensi ping (rentang 1H / 24H / 7D)
- **Recent Diagnostics** — daftar diagnostik terbaru beserta status dan packet loss
- **System Event Log** — timeline aktivitas speed test dan diagnostik

---

### Diagnostics
Jalankan MTR (My Traceroute) ke host tujuan secara real-time.

- Input host dengan autocomplete dari riwayat
- Tampilan tabel hop-by-hop: hostname, packet loss, sent/received, best/avg/worst/last latency
- **Analisis otomatis** — interpretasi hasil dalam Bahasa Indonesia (deteksi ICMP rate limiting, evaluasi kualitas koneksi, wawasan ISP)
- Salin hasil ke clipboard dalam format teks
- Ekspor hasil ke file `.txt`

---

### Speed Test
Uji kecepatan internet terhadap server pilihan.

- **Gauge interaktif** — menampilkan kecepatan download/upload secara real-time
- **Pemilihan server** — dialog dengan daftar server berdasarkan lokasi
- Indikator fase: PING → DOWNLOAD → UPLOAD → DONE
- Kartu detail: Download (avg + maks), Upload (avg + maks), Latency (avg ping, jitter, best)
- **Network Info Panel** — ISP provider, IP publik, lokasi server
- **Peta dunia interaktif** — menandai lokasi server yang digunakan
- Daftar 3 tes terakhir di sidebar

---

### History
Riwayat gabungan semua speed test dan diagnostik.

- Pencarian berdasarkan nama host / server
- Filter tipe: Semua / Diagnostics / Speed Test
- Paginasi (10 item per halaman)
- Indikator status berwarna: OK (hijau), Suboptimal (kuning), Failed (merah)
- Ringkasan per entri: kecepatan, ping, jitter, packet loss, hop count

---

### Settings
*(Coming soon)*

---

## Teknologi

| Layer | Stack |
|-------|-------|
| Backend | Go, [Wails v2](https://wails.io) |
| Frontend | Svelte 5, TypeScript, Tailwind CSS 4 |
| UI Components | Bits UI, Material Symbols |
| Visualisasi | D3.js, TopoJSON |
| Storage | SQLite |

---

## Menjalankan Aplikasi

### Development

```bash
wails dev
```

Frontend hot-reload tersedia di `http://localhost:34115`.

### Build

```bash
wails build
```

Menghasilkan executable redistributable di folder `build/bin/`.

---

## Lisensi

© Developed by **Prof Kim**
