# Smart Campaign Composer — Case Brief (source of truth)

> **What this file is.** A verbatim copy of the Insider One "Smart Campaign Composer" case
> (original artifact: https://claude.ai/artifact/Vdtq1yNTx4buTo9A1jTvmj), captured on 2026-10-19.
> The case text is kept in its original language (Turkish) on purpose so nothing is lost in
> translation; later sessions should read requirements from here, not from memory.
> Section 2 holds the delivery/process notes from the HR email (names removed).

---

## 1. Case text (verbatim, Turkish)

**Insider One · AI Developer Case Study · 2026**

# Smart Campaign Composer

Pazarlamacının doğal dille yazdığı bir kampanya isteğini Claude ile hedef kitleye ve SMS ve email
mesajlarına çeviren; onaya sunan ve gönderip durumunu canlı gösteren uçtan uca bir sistem kuracaksın.

| | |
|---|---|
| Frontend | Vue.js |
| Backend | Go veya PHP |
| LLM | Claude API |
| Teslim | 7 gün içinde |

### İş bağlamı

Insider One müşterileri her gün milyonlarca kişiye SMS ve email gönderiyor. Bir kampanya hazırlamak
için kitleyi tanımlamak, her kanal için metin yazmak ve karakter limitlerine uymak gerekiyor. Bu
case'te pazarlamacının yalnızca ne istediğini yazdığı, gerisini Claude'un önerdiği ve son kararı
insanın verdiği bir araç kurmanı istiyoruz.

### Kullanıcı akışı

**Örnek brief**

> "Son 30 günde sepetine ürün ekleyip alışveriş yapmayan İstanbul'daki kullanıcılara %30 indirim
> hatırlatması gönder. Samimi ama kısa olsun."

1. **Kitle çıkarılır** — Claude brief'ten bir segment kuralı üretir, arayüz kaç kişiye gideceğini gösterir.
2. **Mesajlar üretilir** — SMS ve email içeriği ekrana stream edilerek akar.
3. **İnsan onaylar** — Pazarlamacı önizler, gerekirse düzenler ve onaylar.
4. **Gönderilir** — Mesajlar kuyruk üzerinden mock provider'a gönderilir.
5. **Canlı izlenir** — Dashboard gönderim durumunu gösterir.

Gerçek kimseye mesaj gitmez. Alıcılar senin oluşturacağın sahte kullanıcılardır, gönderim de senin
yazacağın mock provider'a yapılır.

### Fonksiyonel gereksinimler

**Kitle**
- Claude'un ürettiği segment kuralı backend'de doğrulanır; LLM çıktısı doğrudan SQL olarak çalıştırılmaz.
- İzni olmayan kullanıcıya o kanaldan mesaj gönderilmez.

**İçerik**
- İçerik kanal limitlerine uymalı: SMS en fazla 160 karakter, email konusu en fazla 80 karakter.
- `{{first_name}}` gibi kişiselleştirme değişkenleri desteklenir.

**Gönderim**
- Asenkron kuyruk, kanal başına saniyede en fazla 100 mesaj.
- Aynı kişiye aynı mesaj iki kez gitmez. Retry stratejisini sen tasarla.

**Takip**
- Canlı gönderim durumu, metrics ve health check endpoint'leri.

### AI gereksinimleri

Case'in en önemli kısmı. Claude'u çağırmak yeterli değil; çıktısını güvenilir hale getirmeni bekliyoruz.

- **Doğrulama:** Claude'un çıktısı yapılandırılmış (JSON) olmalı ve backend'de kontrol edilmeli.
- **Test edilebilirlik:** testler gerçek API key olmadan, mock bir LLM ile çalışmalı.

### Kaynaklar

**Claude API key:** Uygulama key'i `.env` dosyasındaki `ANTHROPIC_API_KEY` değişkeninden okumalı;
repoya bir `.env.example` ekle. Geliştirme sırasında mock bir LLM ile çalışabilir, gerçek modelle
denemek istersen Claude Console'dan kendi API key'ini alabilirsin.

Aşağıdakileri de kendin hazırlayacaksın:

- **Sahte kullanıcı verisi:** birkaç bin kullanıcılık bir seed. Hangi alanlara ihtiyaç olduğuna (ad,
  email, telefon, şehir, alışveriş geçmişi, kanal izinleri gibi) sen karar ver.
- **Mock SMS/email provider:** gerçek bir servisi taklit eden basit bir servis. Retry ve rate limit
  mantığını gerçekten test edebilmek için zaman zaman hata, gecikme ve 429 döndürmeli.

### Teknoloji

Frontend Vue.js, backend Go veya PHP. Veritabanı, kuyruk ve kütüphaneler senin seçimin.

### Teslim edilecekler

- GitHub veya GitLab repository
- `docker compose up` ile tek komutla kurulum (seed verisi ve mock provider dahil)
- `make test` ile çalışan testler
- `README.md`: kurulum ve kısa mimari özeti
- `DECISIONS.md`: önemli kararların ve nedenleri
- `ai-sessions/`: Claude session export'ların (aşağıya bakın)

**Kapsam notu:** Her şeyi yapmak yerine çekirdek akışı sağlam yapmanı tercih ederiz. Yetiştiremediklerini
README'de belirtmen yeterli.

### Bonus (isteğe bağlı)

- Zamanlanmış gönderim
- A/B varyantları
- Kullanıcının diline göre içerik
- CI pipeline

### AI araçları kullanımı

Claude Code, Cursor gibi araçları kullanman serbest, hatta bekleniyor. Önemli olan üretilen kodu
anlaman ve sahiplenmen.

Case boyunca Claude ile yaptığın tüm çalışma session'larının export'larını repodaki `ai-sessions/`
klasörüne ekle (Claude Code'da `/export`, claude.ai'da paylaşım linki). Göndermeden önce API key ve
kişisel bilgileri temizle.

**Neden istiyoruz:** Kayıtları ne kadar AI kullandığına göre puanlamıyoruz. Problemi nasıl ele aldığını
ve AI çıktısını nasıl doğruladığını görmek istiyoruz.

### Değerlendirme

| Kriter | Ağırlık |
|---|---|
| LLM entegrasyonu | %25 |
| Mimari ve kararlar | %20 |
| Backend güvenilirliği | %20 |
| Frontend | %20 |
| Test ve kod kalitesi | %15 |

### Sonraki adım

Projen incelenip olumlu bulunduğu takdirde seninle 30 dakikalık bir review görüşmesi yapacağız.
Görüşmede çözümünü ve verdiğin kararları birlikte konuşacağız. Soruların olursa veya ek süreye
ihtiyacın olursa bize yazman yeterli.

---

## 2. Process notes from the HR email (paraphrased, names removed)

- **Deadline:** 7 days from receipt of the email → **2026-10-26 (Monday)**.
- **Submission:** reply-all to the HR email with the repository link.
- **Deliverables** (same as the case): repo, `docker compose up` (seed + mock provider included),
  `make test`, `README.md` (setup + short architecture summary), `DECISIONS.md`, `ai-sessions/`.
- **Scope:** not everything is expected; a solid core flow is preferred, and anything not finished
  must be listed in the README. Bonus items are fully optional.
- **AI tools:** encouraged. What matters is understanding and owning the generated code. All Claude
  session exports go into `ai-sessions/`; **strip API keys and personal data before submitting.**
  Sessions are not scored by amount of AI use but by problem approach and how AI output was verified.
- **Questions / extra time:** reply-all to the same email thread.
- **After submission:** evaluation within one week; if positive, a 30-minute review call about the
  solution and its decisions.
