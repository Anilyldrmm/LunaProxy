package main

import "testing"

func TestUnderDir(t *testing.T) {
	pf := `C:\Program Files`
	for path, want := range map[string]bool{
		`C:\Program Files\LunaProxy\LunaProxy.exe`: true,
		`c:\program files\LunaProxy\LunaProxy.exe`: true,
		`C:\Program FilesX\LunaProxy.exe`:          false,
		`C:\Users\x\Downloads\LunaProxy.exe`:       false,
		`C:\Program Files\..\Users\x\a.exe`:        false,
	} {
		if got := underDir(path, pf); got != want {
			t.Errorf("underDir(%q)=%v, beklenen %v", path, got, want)
		}
	}
	if underDir(`C:\a.exe`, "") {
		t.Error("boş dizin eşleşmemeli")
	}
}
