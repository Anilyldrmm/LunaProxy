@echo off
chcp 65001 >nul
title LunaProxy — Build

echo.
echo  ╔════════════════════════════════════════════════╗
echo  ║         LunaProxy  —  Build Script              ║
echo  ╚════════════════════════════════════════════════╝
echo.

:: ── Go kontrolü ──────────────────────────────────────────────────────────────
where go >nul 2>&1
if %ERRORLEVEL% NEQ 0 (
    echo  [!] Go kurulu degil.  https://go.dev/dl/
    pause & exit /b 1
)
for /f "tokens=3" %%v in ('go version') do set GOVERSION=%%v
echo  Go: %GOVERSION%

:: ── GCC kontrolü (walk CGO gerektiriyor) ────────────────────────────────────
where gcc >nul 2>&1
if %ERRORLEVEL% NEQ 0 (
    echo.
    echo  [!] GCC/MinGW-w64 bulunamadi.
    echo.
    echo      Kurulum secenekleri:
    echo      1. winget install -e --id GnuWin32.Gcc
    echo      2. https://www.mingw-w64.org/downloads/  ^(LLVM-MinGW onerilir^)
    echo      3. MSYS2: msys2.org  →  pacman -S mingw-w64-x86_64-gcc
    echo.
    echo      Kurduktan sonra PATH'e ekleyip tekrar calistirin.
    pause & exit /b 1
)
for /f "tokens=*" %%v in ('gcc --version 2^>^&1 ^| findstr /r "gcc"') do (
    echo  GCC: %%v
    goto :gcc_done
)
:gcc_done

:: ── rsrc: manifest + ikon embed (varsa kullan) ───────────────────────────────
where rsrc >nul 2>&1
if %ERRORLEVEL% EQU 0 (
    echo  Manifest gomuleyor...
    rsrc -manifest app.manifest -o rsrc.syso 2>nul
    if %ERRORLEVEL% NEQ 0 (
        echo  [uyari] rsrc basarisiz, manifest gomulmeden devam ediliyor
    )
) else (
    echo  [bilgi] rsrc bulunamadi, manifest gomulmeyecek
    echo         Kurulum: go install github.com/akavel/rsrc@latest
)

:: ── Modüller ──────────────────────────────────────────────────────────────────
echo  Bagimliliklar guncelleniyor...
set CGO_ENABLED=1
set GOOS=windows
set GOARCH=amd64
go mod tidy
if %ERRORLEVEL% NEQ 0 (
    echo  [!] go mod tidy basarisiz.
    pause & exit /b 1
)

:: ── Derleme ───────────────────────────────────────────────────────────────────
echo  Derleniyor...
go build -ldflags "-H windowsgui -s -w" -o LunaProxy.exe .

if %ERRORLEVEL% NEQ 0 (
    echo.
    echo  [!] Derleme basarisiz.
    pause & exit /b 1
)

echo.
for %%F in (LunaProxy.exe) do echo  [OK]  LunaProxy.exe  —  %%~zF bytes
echo.
pause
