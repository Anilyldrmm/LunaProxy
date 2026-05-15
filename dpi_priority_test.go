package main

import "testing"

func TestParseServiceRunning(t *testing.T) {
	running := `SERVICE_NAME: GoodbyeDPI
        TYPE               : 10  WIN32_OWN_PROCESS
        STATE              : 4  RUNNING
        WIN32_EXIT_CODE    : 0  (0x0)`

	stopped := `SERVICE_NAME: GoodbyeDPI
        TYPE               : 10  WIN32_OWN_PROCESS
        STATE              : 1  STOPPED
        WIN32_EXIT_CODE    : 0  (0x0)`

	notFound := `[SC] EnumQueryServicesStatus:OpenService FAILED 1060`

	if !parseServiceRunning(running) {
		t.Error("RUNNING durumu tespit edilemedi")
	}
	if parseServiceRunning(stopped) {
		t.Error("STOPPED yanlışlıkla RUNNING döndü")
	}
	if parseServiceRunning(notFound) {
		t.Error("hata çıktısı yanlışlıkla RUNNING döndü")
	}
}

func TestParseProcessFound(t *testing.T) {
	found := `goodbyedpi.exe            1234 Console                    1     4,512 K`
	empty := `INFO: No tasks are currently running which match the specified criteria.`

	if !parseProcessFound(found) {
		t.Error("proses tespit edilemedi")
	}
	if parseProcessFound(empty) {
		t.Error("boş çıktı yanlışlıkla proses döndü")
	}
}

func TestResolveDPIDisabled(t *testing.T) {
	c := Config{DPISource: "disabled"}
	r, err := ResolveDPI(c)
	if err != nil {
		t.Fatal(err)
	}
	if r.Source != "disabled" || r.ExePath != "" {
		t.Errorf("disabled: source=%s exePath=%s", r.Source, r.ExePath)
	}
}

func TestResolveDPIManualNoPath(t *testing.T) {
	c := Config{DPISource: "manual", GDPIPath: ""}
	_, err := ResolveDPI(c)
	if err == nil {
		t.Error("yol yokken hata bekleniyor")
	}
}

func TestResolveDPIManualWithPath(t *testing.T) {
	c := Config{DPISource: "manual", GDPIPath: `C:\gdpi\goodbyedpi.exe`}
	r, err := ResolveDPI(c)
	if err != nil {
		t.Fatal(err)
	}
	if r.Source != "manual" || r.ExePath != `C:\gdpi\goodbyedpi.exe` {
		t.Errorf("manual: source=%s exePath=%s", r.Source, r.ExePath)
	}
}
