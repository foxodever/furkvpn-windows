//go:build windows

package main

import (
	"strings"

	"golang.org/x/sys/windows"
)

// The few texts the window's process shows itself (the tray); the rest is in the interface.
var texts = map[string][2]string{
	"tray_open":         {"Открыть FurkVPN", "Open FurkVPN"},
	"tray_connect":      {"Подключиться", "Connect"},
	"tray_disconnect":   {"Отключиться", "Disconnect"},
	"tray_quit":         {"Выход", "Quit"},
	"tray_hint":         {"FurkVPN работает в трее — щёлкните по значку, чтобы открыть", "FurkVPN keeps running in the tray — click its icon to open it"},
	"status_connected":  {"Подключено", "Connected"},
	"status_connecting": {"Подключение…", "Connecting…"},
	"status_stopping":   {"Отключение…", "Disconnecting…"},
	"status_idle":       {"Не подключено", "Not connected"},
	"status_offline":    {"Служба не отвечает", "The service is not responding"},
	"settings_file":     {"Файл настроек FurkVPN", "FurkVPN settings file"},
	"program":           {"Программа", "Program"},
}

func (a *App) text(key string) string {
	t, ok := texts[key]
	if !ok {
		return key
	}
	if a.lang == "ru" {
		return t[0]
	}
	return t[1]
}

// uiLanguage is "ru" for Russian-speaking Windows, "en" otherwise; the full tag goes to the interface.
func uiLanguage() string {
	tag := uiLanguageTag()
	for _, p := range []string{"ru", "be", "uk", "kk", "ky", "uz", "tg", "hy", "az", "ka"} {
		if strings.HasPrefix(strings.ToLower(tag), p) {
			return "ru"
		}
	}
	return "en"
}

func uiLanguageTag() string {
	langs, err := windows.GetUserPreferredUILanguages(windows.MUI_LANGUAGE_NAME)
	if err != nil || len(langs) == 0 {
		return "en-US"
	}
	return langs[0]
}

// Language tells the interface the language of Windows.
func (a *App) Language() string { return uiLanguageTag() }
