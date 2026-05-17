#define AppName      "LunaProxy"
#ifndef AppVersion
  #define AppVersion "1.0.20"
#endif
#define AppPublisher "SpAC3"
#define AppURL       "https://github.com/Anilyldrmm/LunaProxy"
#define AppExe       "LunaProxy.exe"

[Setup]
AppId={{A1B2C3D4-E5F6-7890-ABCD-EF1234567890}
AppName={#AppName}
AppVersion={#AppVersion}
AppPublisherURL={#AppURL}
AppSupportURL={#AppURL}
AppUpdatesURL={#AppURL}
DefaultDirName={autopf}\{#AppName}
DefaultGroupName={#AppName}
AllowNoIcons=yes
PrivilegesRequired=admin
OutputDir=Output
OutputBaseFilename=LunaProxy_Setup_v{#AppVersion}
SetupIconFile=..\icon.ico
Compression=lzma2/ultra64
SolidCompression=yes
WizardStyle=modern
WizardSmallImageFile=..\icon.png
UninstallDisplayIcon={app}\{#AppExe}
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
MinVersion=10.0.17763
CloseApplications=yes

[Languages]
Name: "turkish";  MessagesFile: "compiler:Languages\Turkish.isl"
Name: "english";  MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon";    Description: "Masaüstü kısayolu oluştur";     GroupDescription: "Ek görevler:"; Flags: unchecked
Name: "startupicon";    Description: "Başlangıçta otomatik başlat";   GroupDescription: "Ek görevler:"; Flags: unchecked

[Files]
Source: "..\{#AppExe}"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{group}\{#AppName}";                     Filename: "{app}\{#AppExe}"
Name: "{group}\{#AppName}'i Kaldır";            Filename: "{uninstallexe}"
Name: "{autodesktop}\{#AppName}";               Filename: "{app}\{#AppExe}"; Tasks: desktopicon

[Registry]
; Başlangıç kaydı (görev seçildiyse)
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; \
  ValueType: string; ValueName: "{#AppName}"; ValueData: """{app}\{#AppExe}"""; \
  Flags: uninsdeletevalue; Tasks: startupicon

[Run]
; Normal kurulum: son sayfada checkbox olarak göster
Filename: "{app}\{#AppExe}"; Description: "{#AppName}'i şimdi başlat"; \
  Flags: nowait postinstall skipifsilent runascurrentuser
; Sessiz kurulum (auto-update): her zaman çalıştır, postinstall atlansın
Filename: "{app}\{#AppExe}"; Flags: nowait runascurrentuser; Check: WizardSilent

[UninstallRun]
Filename: "taskkill"; Parameters: "/f /im {#AppExe}"; Flags: runhidden; RunOnceId: "KillApp"

[Code]
// WebView2 Runtime kurulu mu kontrol et
function WebView2Installed: Boolean;
var
  sVersion: string;
begin
  Result := RegQueryStringValue(HKLM,
    'SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}',
    'pv', sVersion) and (sVersion <> '') and (sVersion <> '0.0.0.0');
  if not Result then
    Result := RegQueryStringValue(HKCU,
      'SOFTWARE\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}',
      'pv', sVersion) and (sVersion <> '') and (sVersion <> '0.0.0.0');
end;

procedure InitializeWizard;
begin
  if not WebView2Installed then
    MsgBox(
      'Microsoft Edge WebView2 Runtime bulunamadı.' + #13#10 +
      'LunaProxy çalışmak için WebView2 gerektirir.' + #13#10#13#10 +
      'Kurulumdan sonra https://go.microsoft.com/fwlink/p/?LinkId=2124703' + #13#10 +
      'adresinden WebView2 Runtime''ı indirip kurunuz.',
      mbInformation, MB_OK);
end;
