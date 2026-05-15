# Icon Quality — CatmullRom Scaler

**Goal:** Windows taskbar, systray ve alt-tab ikonlarını pikselleşmeden, keskin göstermek.

**Root cause:** `resizeLogo` özel box-filter kullanıyor. 1024→16px gibi büyük oranlar için box-filter kenarları yıkıyor ve blur üretiyor.

**Çözüm:** `golang.org/x/image/draw.CatmullRom.Scale` — bicubic interpolasyon, kenarleri korur, sharpen etkisi yaratır.

**Kapsam:**
- `icon.go`: `resizeLogo` fonksiyonu yeniden yazılır, özel box-filter kaldırılır
- `go.mod / go.sum`: `golang.org/x/image` bağımlılığı eklenir
- Başka dosya değişmez; tüm tüketiciler (`makeICOBytes`, `makeTrayICOBytes`, `makeIconPNG`) aynı imzayı kullanmaya devam eder

**Test:** Derleme başarılı, çalışma zamanında ikon görsel olarak keskin.
