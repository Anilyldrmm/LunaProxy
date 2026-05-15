GoodbyeDPI Bundle Dosyaları
============================
Bu klasöre şu dosyaları yerleştir:
  goodbyedpi.exe   (~1.3 MB)
  WinDivert.dll    (~70 KB)
  WinDivert64.sys  (~90 KB)

Ardından bundle ile derle:
  go build -tags withbundle -o SpAC3DPI.exe

Bundle olmadan normal derleme:
  go build -o SpAC3DPI.exe
