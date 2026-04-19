# UI/UX Planning: Network Monitoring Dashboard (Diagnostics Page)

## Goal
Membuat UI/UX untuk aplikasi desktop Network Monitoring (fokus pada halaman "Diagnostics") menggunakan Wails, Svelte, TypeScript, Shadcn-Svelte, Tailwind CSS 4, dan icon Material UI Google. Aplikasi akan memiliki desain khas shadcn-ui yang bersih, minimalis, dan sangat responsif untuk pengguna desktop.

## User Review Required
> [!IMPORTANT]
> - Framework pilihan: Shadcn-svelte sangat direkomendasikan berjalan di atas **SvelteKit**. Saya berencana menginisialisasi Wails dengan frontend berbasis Svelte (SPA/SvelteKit). Mohon konfirmasi jika Svelte Vite bawan Wails (SPA biasa) sudah cukup, atau sebaiknya menggunakan SvelteKit agar integrasi Shadcn-Svelte lebih mulus.
> - Mengingat Tailwind CSS v4 masih cukup baru/alpha, kita mungkin perlu melakukan penyesuaian sedikit pada bundler (Vite) atau kita bisa menggunakan standar v3 jika Anda mengutamakan stabilitas dengan Shadcn. 

## Struktur & Komponen UI (Berdasarkan Referensi Gambar)

Aplikasi akan menggunakan layout *Two-Column*:
### 1. Sidebar Khusus
- Link Navigasi Tepi Kiri: `Dashboard`, `Diagnostics` (aktif, *highlighted*), `History`, `Settings`.
- Icon untuk setiap menu akan menggunakan Google Material Icons.

### 2. Main Content (Diagnostics Page)
Halaman ini akan dipecah menjadi 3 sub-bagian utama (atas ke bawah):
- **Header Controls (Input Bar)**
  - `Input Model` (Host): Placeholder "google.com or IP address".
  - `Select Model` (History): Dropdown dengan label "Select".
  - `Primary Button` (Start): Dilengkapi icon "play", warna utama (primary), jika di-klik maka background berubah jadi merah/destruktif dan text menjadi "Stop".
  - `Outline Button` (Options): Dilengkapi gear/settings icon.
- **Action Toolbar**
  - Dropdown `Copy to Clipboard` dengan `DropdownMenu` ("Text", "HTML").
  - Spacer / Flex-grow.
  - Dropdown `Export` dengan `DropdownMenu` ("Text", "HTML").
- **Tabel Diagnostik (Data Table)**
  - Memanfaatkan komponen Shadcn-Svelte `Table` untuk menampilkan data grid dengan batas yang tipis (`border-b`).
  - **Kolom Header**: `Hostname`, `Nr`, `Loss %`, `Sent`, `Recv`, `Best`, `Avrg`, `Worst`, `Last`.
  - Tabel dirancang agar memenuhi sisa jendela dan ukurannya dinamis.

## Implementation Steps

1. **Inisialisasi Proyek Wails**: Menjalankan perintah wails untuk membangkitkan proyek Svelte TypeScript di direktori `net-monit`.
2. **Setup Frontend**: 
   - Konfigurasi Tailwind CSS (v4 jika memungkinkan, atau iterasi v3 yang stabil di Shadcn).
   - Inisialisasi dependensi `shadcn-svelte`.
3. **Instalasi Komponen Shadcn**: Menambahkan komponen seperti `button`, `input`, `select`, `dropdown-menu`, `table`, dan `badge`.
4. **Membangun Layout & Sidebar**: Penyusunan grid CSS untuk membingkai Sidebar dan Body Content.
5. **Konstruksi Halaman Diagnostics**: 
   - Menyusun Form section di atas.
   - Menyusun menu Dropdown Action copy & export.
   - Menyusun Tabel data dan menambahkan *dummy state* data.
6. **Interaktif Form / Mock State**: Membuat variabel *state* (misal: `isPinging`) menggunakan reactivity Svelte agar tombol Start berubah menjadi Stop.

## Open Questions
- Apakah implementasi ini saat ini hanya fokus pada Frontend UI/UX saja tanpa menghubungkan logika golang backend (wails bindings)?
- Apakah Anda ingin saya melanjutkan untuk men-generate proyek template-nya (menjalankan command Wails and Vite) setelah draft ini Anda setujui?

## Verification Plan
1. Menjalankan `bun dev` pada direktori frontend untuk memastikan view memuat halaman Svelte murni tanpa error.
2. Memverifikasi layout, ketepatan rendering semua komponen Shadcn, dan fungsionalitas button (termasuk mock toggle *Start/Stop*).
3. Mengekspor *WebP view* (Browser view / screenshot tool) agar Anda bisa melihat prototipe di akhir tahap jika diperlukan.
