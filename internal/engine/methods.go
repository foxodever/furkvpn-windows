package engine

import (
	"context"
	"encoding/json"

	"furkvpn/internal/ipc"
	"furkvpn/internal/routing"
	"furkvpn/internal/settings"
	"furkvpn/internal/transfer"
	"furkvpn/internal/version"
)

// Connected is called when a window connects: it gets the whole state at once.
func (e *Engine) Connected(c *ipc.Conn) {
	c.Send("state", e.fullState())
}

// Disconnected is called when a window goes away.
func (e *Engine) Disconnected(c *ipc.Conn) {}

func decode[T any](params json.RawMessage) (T, error) {
	var v T
	if len(params) == 0 {
		return v, nil
	}
	if err := json.Unmarshal(params, &v); err != nil {
		return v, ipc.Errorf("bad_request", err.Error())
	}
	return v, nil
}

// Handle serves the windows' requests.
func (e *Engine) Handle(ctx context.Context, c *ipc.Conn, method string, params json.RawMessage) (any, error) {
	switch method {
	case "hello":
		// The window is open: refresh what is outdated (like returning to the app on Android).
		go func() {
			e.refreshConfigIfOlder(configStaleOnOpen)
			e.refreshSubscriptionIfStale()
			e.refreshAccount(false)
		}()
		e.checkWhitelistIfStale()
		return map[string]any{"version": version.Name, "versionCode": version.CodeInt(), "protocol": ipc.Version}, nil

	case "connect":
		if e.subs.Info() == nil {
			return nil, ipc.Errorf("no_key", "")
		}
		go e.Connect()
		return nil, nil
	case "disconnect":
		go e.Disconnect()
		return nil, nil

	case "selectServer":
		p, err := decode[struct{ Key string }](params)
		if err != nil {
			return nil, err
		}
		_, _ = e.settings.Update(func(s *settings.Settings) { s.SelectedServer = p.Key })
		go e.Reconnect()
		return nil, nil

	case "submitKey":
		p, err := decode[struct{ Text string }](params)
		if err != nil {
			return nil, err
		}
		return e.SubmitKey(p.Text)
	case "importKey":
		p, err := decode[struct{ URL string }](params)
		if err != nil {
			return nil, err
		}
		return e.ImportKey(p.URL)
	case "refreshSubscription":
		return nil, e.RefreshSubscription()
	case "logout":
		e.Logout()
		return nil, nil

	case "testPings":
		e.TestPings()
		return nil, nil
	case "pingServer":
		p, err := decode[struct{ Key string }](params)
		if err != nil {
			return nil, err
		}
		e.PingServer(p.Key)
		return nil, nil
	case "openAutoCheck":
		p, err := decode[struct{ Key string }](params)
		if err != nil {
			return nil, err
		}
		e.OpenAutoCheck(p.Key)
		return nil, nil
	case "recheckAuto":
		e.RecheckAuto()
		return nil, nil
	case "closeAutoCheck":
		e.CloseAutoCheck()
		return nil, nil

	case "checkWhitelist":
		e.kickWhitelist(0)
		return nil, nil

	case "set":
		return nil, e.set(params)
	case "setSharePorts":
		p, err := decode[struct{ HTTP, Socks int }](params)
		if err != nil {
			return nil, err
		}
		if p.HTTP == p.Socks || p.HTTP < settings.MinPort || p.HTTP > settings.MaxPort || p.Socks < settings.MinPort || p.Socks > settings.MaxPort {
			return nil, ipc.Errorf("bad_ports", "")
		}
		_, err = e.settings.Update(func(s *settings.Settings) { s.Share.HTTPPort, s.Share.SocksPort = p.HTTP, p.Socks })
		return nil, err
	case "resetShareCredentials":
		_, err := e.settings.Update(func(s *settings.Settings) {
			s.Share.User, s.Share.Password = settings.NewShareUser(), settings.NewSharePassword()
		})
		return nil, err

	case "parseRules":
		p, err := decode[struct{ Text string }](params)
		if err != nil {
			return nil, err
		}
		valid, invalid := routing.ParseList(p.Text)
		return map[string]any{"valid": ruleViews(valid), "invalid": invalid}, nil
	case "addRules", "replaceRule":
		p, err := decode[struct {
			Action routing.Action
			Text   string
			Old    string
		}](params)
		if err != nil {
			return nil, err
		}
		if !validAction(p.Action) {
			return nil, ipc.Errorf("bad_request", "action")
		}
		valid, invalid := routing.ParseList(p.Text)
		if len(valid) > 0 || method == "replaceRule" && len(invalid) == 0 {
			_, err = e.settings.Update(func(s *settings.Settings) {
				if method == "replaceRule" {
					s.Routing.ReplaceRule(p.Action, p.Old, valid)
				} else {
					s.Routing.AddRules(p.Action, valid)
				}
			})
		}
		return map[string]any{"added": len(valid), "invalid": invalid}, err
	case "removeRule":
		p, err := decode[struct {
			Action routing.Action
			Entry  string
		}](params)
		if err != nil {
			return nil, err
		}
		_, err = e.settings.Update(func(s *settings.Settings) { s.Routing.RemoveRule(p.Action, p.Entry) })
		return nil, err
	case "clearRules":
		p, err := decode[struct{ Action routing.Action }](params)
		if err != nil {
			return nil, err
		}
		if !validAction(p.Action) {
			return nil, ipc.Errorf("bad_request", "action")
		}
		_, err = e.settings.Update(func(s *settings.Settings) { s.Routing.ClearRules(p.Action) })
		return nil, err

	case "setAppExcluded":
		p, err := decode[struct {
			Name     string
			Excluded bool
		}](params)
		if err != nil {
			return nil, err
		}
		name, ok := transfer.ProgramName(p.Name)
		if !ok {
			return nil, ipc.Errorf("bad_request", "name")
		}
		recommended := e.cfg().Windows.DefaultExcludedApps
		_, err = e.settings.Update(func(s *settings.Settings) { s.SetAppExcluded(name, p.Excluded, recommended) })
		return nil, err

	case "exportSettings":
		p, err := decode[struct{ Kind string }](params)
		if err != nil {
			return nil, err
		}
		s := e.settings.Get()
		switch p.Kind {
		case "routing":
			return map[string]any{"fileName": transfer.RoutingFileName, "content": string(transfer.ExportRouting(s.Routing))}, nil
		case "apps":
			x := settings.ExclusionsOf(s, e.cfg().Windows.DefaultExcludedApps)
			return map[string]any{"fileName": transfer.AppsFileName, "content": string(transfer.ExportApps(x))}, nil
		}
		return nil, ipc.Errorf("bad_request", "kind")
	case "inspectSettingsFile", "importSettingsFile":
		p, err := decode[struct {
			Content string
			Replace bool
		}](params)
		if err != nil {
			return nil, err
		}
		imp, err := transfer.Parse([]byte(p.Content))
		if err != nil {
			return nil, ipc.Errorf("not_settings_file", "")
		}
		if method == "inspectSettingsFile" {
			return imp, nil
		}
		_, err = e.settings.Update(func(s *settings.Settings) {
			if imp.Routing != nil {
				s.Routing.Import(*imp.Routing, p.Replace)
			}
			if imp.Apps != nil {
				s.ImportApps(*imp.Apps, p.Replace)
			}
		})
		return imp, err

	case "connectionLog":
		p, err := decode[struct{ Since int64 }](params)
		if err != nil {
			return nil, err
		}
		v := e.connlog.Version()
		if v == p.Since {
			return map[string]any{"version": v}, nil
		}
		return map[string]any{"version": v, "apps": e.connlog.Snapshot()}, nil
	case "clearConnectionLog":
		e.connlog.Clear()
		return nil, nil

	case "checkForUpdates":
		available, err := e.CheckForUpdates()
		return map[string]any{"available": available}, err
	case "skipUpdate":
		e.SkipUpdate()
		return nil, nil
	case "downloadUpdate":
		path, err := e.DownloadUpdate()
		if err != nil {
			return nil, err
		}
		return map[string]any{"path": path}, nil
	case "prepareInstall":
		e.PrepareInstall()
		return nil, nil
	}
	return nil, ipc.Errorf("unknown_method", method)
}

func validAction(a routing.Action) bool {
	return a == routing.Direct || a == routing.Proxy || a == routing.Block
}

// set changes one simple setting: {"key": "whitelistDetect", "value": true}.
func (e *Engine) set(params json.RawMessage) error {
	var p struct {
		Key   string
		Value json.RawMessage
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return ipc.Errorf("bad_request", err.Error())
	}
	var b bool
	var n int
	asBool := func() error { return json.Unmarshal(p.Value, &b) }
	asInt := func() error { return json.Unmarshal(p.Value, &n) }
	var change func(s *settings.Settings)
	switch p.Key {
	case "whitelistDetect", "connectionLog", "shareEnabled", "shareAuth", "serversOnLeft", "autoConnect",
		"routingEnabled", "routingLanDirect", "routingRuDirect", "useRecommendedExclusions":
		if err := asBool(); err != nil {
			return ipc.Errorf("bad_request", err.Error())
		}
		change = func(s *settings.Settings) {
			switch p.Key {
			case "whitelistDetect":
				s.WhitelistDetect = b
			case "connectionLog":
				s.ConnectionLog = b
			case "shareEnabled":
				s.Share.Enabled = b
			case "shareAuth":
				s.Share.Auth = b
				if b && s.Share.User == "" {
					s.Share.User, s.Share.Password = settings.NewShareUser(), settings.NewSharePassword()
				}
			case "serversOnLeft":
				s.ServersOnLeft = b
			case "autoConnect":
				s.AutoConnect = b
			case "routingEnabled":
				s.Routing.Enabled = b
			case "routingLanDirect":
				s.Routing.LANDirect = b
			case "routingRuDirect":
				s.Routing.RUDirect = b
			case "useRecommendedExclusions":
				s.UseRecommendedExclusions = b
			}
		}
	case "subscriptionUpdateHours":
		if err := asInt(); err != nil || n < settings.UpdateManual || n > 24 {
			return ipc.Errorf("bad_request", "hours")
		}
		change = func(s *settings.Settings) { s.SubscriptionUpdateHours = n }
	default:
		return ipc.Errorf("bad_request", "unknown setting "+p.Key)
	}
	_, err := e.settings.Update(change)
	return err
}
