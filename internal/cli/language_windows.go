package cli

import "syscall"

func systemLanguage() string {
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("GetUserDefaultUILanguage")
	if proc.Find() != nil {
		return ""
	}
	id, _, _ := proc.Call()
	if id&0x3ff == 0x19 {
		return "ru"
	}
	return "en"
}
