package settings

import (
	"path/filepath"
	"reflect"
	"testing"

	"furkvpn/internal/routing"
)

func TestDefaultsSurviveAMissingField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	st := Open(path)
	if !st.Get().WhitelistDetect || !st.Get().Routing.Enabled || !st.Get().UseRecommendedExclusions {
		t.Fatal("defaults")
	}
	if _, err := st.Update(func(s *Settings) { s.SelectedServer = "a#1"; s.Share.HTTPPort = 80 }); err != nil {
		t.Fatal(err)
	}
	again := Open(path).Get()
	if again.SelectedServer != "a#1" || !again.WhitelistDetect || again.Share.HTTPPort != DefaultHTTPPort {
		t.Fatalf("%+v", again)
	}
}

func TestRoutingListsMoveEntries(t *testing.T) {
	r := Routing{Direct: []string{"domain:a.ru"}, Proxy: []string{"domain:b.ru"}, Block: []string{}}
	r.AddRules(routing.Block, []string{"domain:a.ru", "domain:c.ru"})
	if len(r.Direct) != 0 || !reflect.DeepEqual(r.Block, []string{"domain:a.ru", "domain:c.ru"}) {
		t.Fatalf("%+v", r)
	}
	r.ReplaceRule(routing.Block, "domain:c.ru", []string{"domain:b.ru"})
	if len(r.Proxy) != 0 || !reflect.DeepEqual(r.Block, []string{"domain:a.ru", "domain:b.ru"}) {
		t.Fatalf("%+v", r)
	}
	r.Import(Routing{Direct: []string{"domain:a.ru"}, Proxy: []string{"1.1.1.1"}}, false)
	if !reflect.DeepEqual(r.Block, []string{"domain:b.ru"}) || !reflect.DeepEqual(r.Direct, []string{"domain:a.ru"}) || !reflect.DeepEqual(r.Proxy, []string{"1.1.1.1"}) {
		t.Fatalf("%+v", r)
	}
}

func TestRecommendedAppsAreExcludedUnlessOverridden(t *testing.T) {
	s := Defaults()
	s.ExcludedApps = []string{"bank.exe"}
	s.RecommendedOverrides = []string{"MAX.exe"}
	e := ExclusionsOf(s, []string{"gosuslugi.exe", "max.exe"})
	if !reflect.DeepEqual(e.Effective(), []string{"bank.exe", "gosuslugi.exe"}) || !e.IsRecommended("Max.EXE") {
		t.Fatalf("%v", e.Effective())
	}
	s.UseRecommendedExclusions = false
	if got := ExclusionsOf(s, []string{"gosuslugi.exe"}).Effective(); !reflect.DeepEqual(got, []string{"bank.exe"}) {
		t.Fatalf("switch turns off the whole list: %v", got)
	}
}

func TestSetAppExcluded(t *testing.T) {
	s := Defaults()
	recommended := []string{"max.exe"}
	s.SetAppExcluded("max.exe", false, recommended)
	if !reflect.DeepEqual(s.RecommendedOverrides, []string{"max.exe"}) || len(s.ExcludedApps) != 0 {
		t.Fatalf("%+v", s)
	}
	s.SetAppExcluded("Max.exe", true, recommended)
	s.SetAppExcluded("Telegram.exe", true, recommended)
	if len(s.RecommendedOverrides) != 0 || !reflect.DeepEqual(s.ExcludedApps, []string{"Telegram.exe"}) {
		t.Fatalf("%+v", s)
	}
}

func TestTokens(t *testing.T) {
	if u := NewShareUser(); len(u) != 13 || u[:5] != "furk_" {
		t.Fatal(u)
	}
	if p := NewSharePassword(); len(p) != 14 || p == NewSharePassword() {
		t.Fatal(p)
	}
}
