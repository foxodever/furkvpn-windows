//go:build windows

// FurkVPN.exe is the app's window and tray icon, run by the user. Everything that needs
// administrator rights (the VPN itself) happens in FurkVPNService.exe, which it talks to over a
// named pipe. "--tray" starts it hidden (at sign-in); a furkvpn:// link or a settings file in the
// arguments is passed to the running copy; "--connect", "--disconnect" and "--toggle" turn the
// VPN on or off without the window (shortcuts, the taskbar's tasks); "--restore-proxy" (for the
// installer) only puts back the system proxy the app has set, see sysproxy.go.
package main

import (
	"encoding/json"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"slices"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"

	"furkvpn/frontend"
	"furkvpn/internal/ipc"
)

func main() {
	if slices.Contains(os.Args[1:], "--restore-proxy") {
		restoreProxyAndExit()
		return
	}
	app := newApp(os.Args[1:])
	dist, err := fs.Sub(frontend.Dist, "dist")
	if err != nil {
		log.Fatal(err)
	}
	background := options.NewRGB(backgroundOf(app.ui.dark()))
	titleBar := windows.SystemDefault
	switch app.ui.Theme {
	case "dark":
		titleBar = windows.Dark
	case "light":
		titleBar = windows.Light
	}
	ru := app.lang == "ru"
	msg := func(r, e string) string {
		if ru {
			return r
		}
		return e
	}
	err = wails.Run(&options.App{
		Title:             "FurkVPN",
		Width:             app.ui.Width,
		Height:            app.ui.Height,
		MinWidth:          400,
		MinHeight:         560,
		StartHidden:       app.startHidden,
		HideWindowOnClose: false, // beforeClose hides it instead, see app.go
		BackgroundColour:  background,
		AssetServer:       &assetserver.Options{Assets: dist},
		OnStartup:         app.startup,
		OnDomReady:        app.domReady,
		OnBeforeClose:     app.beforeClose,
		OnShutdown:        app.shutdown,
		Bind:              []interface{}{app},
		// As JSON text: Wails hands the interface `new Error(value)`, and an object would turn
		// into "[object Object]" there, losing the code the interface picks its message by.
		ErrorFormatter: func(err error) any {
			value := map[string]string{"code": "failed", "message": err.Error()}
			if e, ok := err.(*ipc.Error); ok {
				value = map[string]string{"code": e.Code, "message": e.Message}
			}
			b, _ := json.Marshal(value)
			return string(b)
		},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId:               "furkvpn-ui-2f6a1c7e",
			OnSecondInstanceLaunch: app.secondInstance,
		},
		Windows: &windows.Options{
			Theme:               titleBar,
			WebviewUserDataPath: filepath.Join(localAppData(), "FurkVPN", "WebView2"),
			DisablePinchZoom:    true,
			Messages: &windows.Messages{
				InstallationRequired: msg("Для работы FurkVPN нужен компонент Microsoft Edge WebView2.", "FurkVPN needs Microsoft Edge WebView2."),
				UpdateRequired:       msg("Обновите Microsoft Edge WebView2.", "Please update Microsoft Edge WebView2."),
				MissingRequirements:  msg("Не хватает компонентов", "Missing requirements"),
				Webview2NotInstalled: msg("Microsoft Edge WebView2 не установлен", "Microsoft Edge WebView2 is not installed"),
				Error:                msg("Ошибка", "Error"),
				FailedToInstall:      msg("Не удалось установить WebView2. Переустановите FurkVPN.", "Couldn't install WebView2. Reinstall FurkVPN."),
				DownloadPage:         msg("Открыть страницу загрузки WebView2?", "Open the WebView2 download page?"),
				PressOKToInstall:     msg("Нажмите OK, чтобы установить.", "Press OK to install."),
				ContactAdmin:         msg("Обратитесь к администратору компьютера.", "Contact your administrator."),
				InvalidFixedWebview2: msg("Неверный путь к WebView2.", "Invalid WebView2 path."),
				WebView2ProcessCrash: msg("WebView2 аварийно завершился.", "WebView2 crashed."),
			},
		},
	})
	if err != nil {
		log.Print(err)
	}
}
