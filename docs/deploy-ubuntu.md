# Deploy Renoxium di VPS Ubuntu kecil

Panduan ini untuk satu VPS Ubuntu 22.04 atau 24.04 berukuran kecil (sekitar 1 vCPU dan 1–2 GB RAM),
dengan HTTPS dari Cloudflare Tunnel. VPS tidak perlu membuka port apa pun selain SSH.

```
Pengunjung / ChatGPT / Claude
        │ HTTPS
        ▼
   Cloudflare  (sertifikat untuk renoxium.com dan *.renoxium.app)
        │ tunnel keluar dari VPS
        ▼
   cloudflared ──► Renoxium di 127.0.0.1:8080 ──► /var/lib/renoxium (SQLite)
```

## Yang perlu disiapkan

- VPS Ubuntu dengan akses SSH dan `sudo`.
- Domain `renoxium.com` dan `renoxium.app` sudah menjadi zone di akun Cloudflare yang sama.
- Komputer dengan Go 1.26 atau lebih baru untuk build. Go versi lama akan mengunduh 1.26 sendiri.

## Perkiraan memori

| Bagian | Normal | Paling buruk |
| --- | --- | --- |
| Proses utama | 30–60 MB | tumbuh dengan jumlah toko yang aktif |
| Tiap worker skrip | sekitar 24 MB | 256 MB, lalu dibunuh dan diganti |
| cloudflared | 30–50 MB | |

Dengan `-workers 2` (bawaan di `deploy/renoxium.service`), pemakaian normal sekitar 150 MB dan paling buruk
sekitar 650 MB. Untuk VPS 1 GB itu cukup, ditambah swap 1 GB sebagai cadangan. Untuk 2 GB atau lebih,
ubah menjadi `-workers 4`.

## 1. Siapkan server

Login ke VPS, lalu perbarui paket dan pasang `sqlite3` (dipakai untuk backup):

```bash
sudo apt-get update && sudo apt-get upgrade -y && sudo apt-get install -y sqlite3
```

Tambahkan swap 1 GB kalau VPS hanya punya 1 GB RAM:

```bash
sudo fallocate -l 1G /swapfile && sudo chmod 600 /swapfile && sudo mkswap /swapfile && sudo swapon /swapfile
```

```bash
echo '/swapfile none swap sw 0 0' | sudo tee -a /etc/fstab
```

Tutup semua port masuk kecuali SSH. Tunnel hanya membuat koneksi keluar, jadi port 80 dan 443 tidak perlu dibuka:

```bash
sudo ufw default deny incoming && sudo ufw default allow outgoing && sudo ufw allow OpenSSH && sudo ufw --force enable
```

Buat user sistem dan folder data:

```bash
sudo useradd --system --home /var/lib/renoxium --shell /usr/sbin/nologin renoxium
```

```bash
sudo install -d -o renoxium -g renoxium -m 700 /var/lib/renoxium
```

## 2. Build dan kirim aplikasi

Di komputermu, dari folder repo. Pakai `GOARCH=arm64` kalau VPS-mu ARM (cek dengan `uname -m` di VPS:
`x86_64` berarti amd64, `aarch64` berarti arm64).

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o renoxium ./cmd/renoxium
```

```bash
scp renoxium deploy/renoxium.service deploy/backup.sh deploy/cloudflared-config.yml USER@IP-VPS:~
```

## 3. Jalankan Renoxium sebagai service

Di VPS:

```bash
sudo install -D -m 755 ~/renoxium /opt/renoxium/renoxium && sudo install -m 755 ~/backup.sh /opt/renoxium/backup.sh
```

```bash
sudo cp ~/renoxium.service /etc/systemd/system/renoxium.service && sudo systemctl daemon-reload && sudo systemctl enable --now renoxium
```

Cek bahwa server hidup. Perintah ini harus mencetak `ok`:

```bash
curl -s 127.0.0.1:8080/healthz
```

Kalau tidak, lihat log-nya:

```bash
sudo journalctl -u renoxium -n 50 --no-pager
```

## 4. Pasang Cloudflare Tunnel

Pasang `cloudflared` dari repository resmi Cloudflare:

```bash
sudo mkdir -p --mode=0755 /usr/share/keyrings && curl -fsSL https://pkg.cloudflare.com/cloudflare-main.gpg | sudo tee /usr/share/keyrings/cloudflare-main.gpg >/dev/null
```

```bash
echo 'deb [signed-by=/usr/share/keyrings/cloudflare-main.gpg] https://pkg.cloudflare.com/cloudflared any main' | sudo tee /etc/apt/sources.list.d/cloudflared.list
```

```bash
sudo apt-get update && sudo apt-get install -y cloudflared
```

Login ke Cloudflare. Perintah ini mencetak sebuah link; buka di browser dan pilih zone `renoxium.com`:

```bash
cloudflared tunnel login
```

Buat tunnel. Catat ID yang dicetak (bentuknya seperti `6ff42ae2-765d-4adf-8112-31c55c1551ef`):

```bash
cloudflared tunnel create renoxium
```

Pindahkan konfigurasi dan kredensial ke `/etc/cloudflared`. Ganti `TUNNEL_ID` dengan ID tadi:

```bash
sudo mkdir -p /etc/cloudflared && sudo cp ~/.cloudflared/TUNNEL_ID.json /etc/cloudflared/
```

```bash
sed 's/TUNNEL_ID/ID-TUNNEL-KAMU/g' ~/cloudflared-config.yml | sudo tee /etc/cloudflared/config.yml
```

Arahkan DNS kedua domain ke tunnel:

```bash
cloudflared tunnel route dns renoxium renoxium.com
```

```bash
cloudflared tunnel route dns renoxium '*.renoxium.app'
```

Kalau perintah kedua menolak karena `renoxium.app` zone yang berbeda, tambahkan record DNS secara manual
di dashboard Cloudflare untuk `renoxium.app`: tipe `CNAME`, nama `*`, target `TUNNEL_ID.cfargotunnel.com`,
dengan proxy menyala (awan oranye).

Jalankan tunnel sebagai service:

```bash
sudo cloudflared service install && sudo systemctl enable --now cloudflared
```

Di dashboard Cloudflare, untuk kedua zone, buka **SSL/TLS → Edge Certificates** dan nyalakan
**Always Use HTTPS**.

## 5. Cek dari luar

Dari komputer mana saja:

```bash
curl -s https://renoxium.com/healthz
```

```bash
curl -s https://renoxium.com/.well-known/oauth-authorization-server
```

Yang pertama mencetak `ok`. Yang kedua mengembalikan JSON yang berisi `"issuer":"https://renoxium.com"`.

Lalu coba alur lengkapnya:

1. Buka `https://renoxium.com`, buat akun.
2. Di ChatGPT (aktifkan developer mode) atau Claude, tambahkan connector kustom dengan alamat
   `https://renoxium.com/mcp`. Agen akan membuka layar login dan persetujuan Renoxium.
3. Minta agen: "Buatkan toko online parfum dengan tiga produk, lalu publish."
4. Buka toko di `https://NAMA-TOKO.renoxium.app`, dan kelola datanya di `https://renoxium.com/apps/NAMA-TOKO`.

## 6. Backup otomatis

`backup.sh` menyalin semua database dengan aman walaupun server sedang berjalan, lalu membuat satu arsip
`.tar.gz` di `/var/backups/renoxium` dan menghapus arsip yang lebih tua dari 14 hari. Coba sekali:

```bash
sudo /opt/renoxium/backup.sh
```

Jadwalkan setiap hari pukul 03:17 waktu server:

```bash
echo '17 3 * * * root /opt/renoxium/backup.sh >/dev/null' | sudo tee /etc/cron.d/renoxium-backup
```

Backup di VPS yang sama tidak menolong kalau VPS-nya hilang. Salin arsipnya secara rutin ke tempat lain,
misalnya Cloudflare R2 atau object storage lain dengan `rclone`.

### Memulihkan dari backup

```bash
sudo systemctl stop renoxium
```

```bash
sudo mkdir -p /tmp/restore && sudo tar -C /tmp/restore -xzf /var/backups/renoxium/renoxium-TANGGAL.tar.gz
```

```bash
sudo rsync -a --delete /tmp/restore/TANGGAL/ /var/lib/renoxium/ && sudo chown -R renoxium:renoxium /var/lib/renoxium
```

```bash
sudo systemctl start renoxium
```

Database preview tidak ikut di-backup karena isinya hanya salinan data live. Preview akan dibuat ulang
pada `apply_change` berikutnya.

## 7. Memperbarui ke versi baru

Build dan kirim binary baru seperti langkah 2, lalu di VPS:

```bash
sudo /opt/renoxium/backup.sh && sudo install -m 755 ~/renoxium /opt/renoxium/renoxium && sudo systemctl restart renoxium
```

Restart hanya beberapa detik. Request yang sedang berjalan saat itu bisa gagal, jadi lakukan di jam sepi.

## Perintah sehari-hari

| Untuk | Perintah |
| --- | --- |
| Log Renoxium langsung | `sudo journalctl -u renoxium -f` |
| Log tunnel | `sudo journalctl -u cloudflared -f` |
| Status | `systemctl status renoxium cloudflared` |
| Pemakaian memori | `ps -o rss,args -u renoxium` (angka RSS dalam KB) |
| Ukuran data | `sudo du -sh /var/lib/renoxium` |

## Kalau ada masalah

| Gejala | Penyebab yang mungkin | Yang dicek |
| --- | --- | --- |
| Halaman error Cloudflare 1033 atau 502 | `cloudflared` atau Renoxium mati | `systemctl status cloudflared renoxium` |
| `https://renoxium.com` jalan, toko tidak | DNS `*.renoxium.app` belum ada | record `*` di zone `renoxium.app` |
| Toko menampilkan "not published yet" | Aplikasi belum di-publish | minta agen menjalankan `publish` |
| Login terus kembali ke form login | `-base-url` tidak sama dengan alamat yang dibuka | harus persis `https://renoxium.com` |
| Agen gagal connect, `/mcp` memberi 401 | Normal sebelum login; masalah ada di langkah OAuth | buka `/.well-known/oauth-authorization-server`, cek isinya memakai `https://renoxium.com` |
| Semua orang terkena "too many attempts" | `-trusted-ip-header` hilang dari service | `systemctl cat renoxium` |
| Memori penuh | Terlalu banyak worker untuk ukuran VPS | turunkan `-workers`, pastikan swap aktif dengan `free -h` |
