# MC SekaiLink

**MC SekaiLink**, uzaktan VDS kiralamadan kendi cihazında (Windows, Linux veya Android) çalışan, dış ağdan bağlantı alabilen, modern arayüzlü, açık kaynaklı (MIT License) **Minecraft Java + Bedrock** yerel sunucu yöneticisidir.

---

## Özellikler / Features

- **Cross-Play (Java + Bedrock Aynı Dünyada):** Paper, Purpur, Fabric veya NeoForge sunucularına tek tıkla **GeyserMC + Floodgate (+ ViaVersion)** kurulumu ve otomatik yapılandırma.
- **Çoklu Motor Desteği:**
  - **Java:** Paper, Purpur, Fabric, NeoForge, Vanilla
  - **Bedrock:** Resmi Bedrock Dedicated Server (`BDS` — Windows & Linux x64) ve **PowerNukkitX** (Windows, Linux, Android ARM64 uyumlu)
- **VDS'siz Dış Bağlantı (CGNAT Aşan):**
  - Otomatik **CGNAT / Public IP** tanılama
  - Tek tıkla **playit.gg** (Java TCP + Bedrock UDP) tünel kurulumu ve canlı adres yakalama
  - CGNAT olmayan ağlar için tek tıkla **UPnP** otomatik port açma
- **Mod, Eklenti, Shader & Kaynak Paketi Merkezi:**
  - Dahili **Modrinth** araması, otomatik bağımlılık çözme, tek tıkla kurulum, aç/kapa ve sürükle-bırak `.jar` / `.mcpack` / `.mcaddon` yükleme
  - Dahili **HTTP SHA-1 Resource Pack & Core Shader** sunucusu (`resource-pack` ve `resource-pack-sha1` otomatik ayarlanır) + arkadaşlar için `.mrpack` istemci paketi dışa aktarma
- **Dünya & Yedekleme Yöneticisi:** `.zip` / `.mcworld` içe aktarma, çoklu dünya geçişi, dışa aktarma ve tek tıkla yedek alma / geri yükleme
- **Çapraz Platform & Uzak Kontrol:**
  - **Windows & Linux Masaüstü:** Hafif Wails v2 uygulaması + `--headless` sunucu modu
  - **Android Mobil Uygulama:** Hem telefonda yerel sunucu çalıştırma (`Foreground Service` + gömülü Go çekirdeği) hem de **Remote Companion** modu ile PC/Linux sunucusunu uzaktan yönetme

---

## Mimari

```text
MC_SekaiLink/
├── cmd/sekailink-daemon/   # Pure-Go headless daemon (Windows / Linux / Android ARM64)
├── internal/
│   ├── addons/             # Modrinth v2, .mcpack/.mcaddon, HTTP SHA-1 Pack Server, .mrpack
│   ├── api/                # REST + WebSocket canlı konsol + Token kimlik doğrulama
│   ├── config/ & storage/  # Atomik JSON yapılandırma ve sunucu listesi
│   ├── engine/             # Paper, Purpur, Fabric, NeoForge, Vanilla, BDS, PowerNukkitX, Geyser
│   ├── network/            # CGNAT testi, UPnP port açıcı, playit.gg kontrolcü, Android DNS
│   └── worlds/             # Güvenli dünya içe/dışa aktarma ve yedekleme
├── frontend/               # React + TypeScript + Tailwind + Framer Motion + Recharts UI
├── web/                    # Gömülü (embed.FS) frontend derleme çıktısı
├── android/                # Kotlin Android uygulaması (WebView + ForegroundService + JreManager)
└── .github/workflows/      # Otomatik Windows, Linux, Android APK GitHub Actions derlemesi
```

---

## GitHub (`MC_SekaiLink`) Deposuna Gönderme Adımları

Bu proje bilgisayarında hazırlandıktan sonra GitHub'daki **`MC_SekaiLink`** depona göndermek için:

```bash
git remote add origin https://github.com/<KULLANICI_ADIN>/MC_SekaiLink.git
git pull origin main --allow-unrelated-histories
git push -u origin main
```

> **Not:** GitHub'da depo açarken seçtiğin `LICENSE` dosyasıyla yerel `LICENSE` dosyası aynı (GPL-3.0) olduğu için olası çakışmalarda yerel dosyaları koruyabilirsin. Kodu gönderdikten birkaç dakika sonra GitHub **Actions** sekmesinde Windows `.exe`, Linux ikilileri ve Android `.apk` otomatik olarak oluşturulacaktır.

---

## Yerel Geliştirme / Local Build

```bash
# 1. Frontend'i derle
cd frontend
npm install
npm run build
cd ..

# 2. Go testlerini calistir
go test -v ./...

# 3. Masaustu uygulamasini derle (Wails v2)
wails build

# Veya sadece headless daemon calistir:
go run ./cmd/sekailink-daemon --port 25580
```

## Lisans

Bu proje [GNU General Public License v3.0](LICENSE) altında açık kaynaklıdır.
