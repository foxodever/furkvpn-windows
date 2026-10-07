package subs

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"furkvpn/internal/keys"
)

const body = `[{"remarks":"🇳🇱 Нидерланды","outbounds":[{"tag":"proxy","protocol":"vless","settings":{"vnext":[{"address":"nl.net","port":443,"users":[]}]}}]},
 {"remarks":"⬇️ Внизу ⬇️","outbounds":[{"tag":"proxy","protocol":"vless","settings":{"vnext":[{"address":"0.0.0.0","port":1,"users":[]}]}}]}]`

func repo(t *testing.T, mirrors ...string) *Repo {
	return Open(Options{
		Dir: t.TempDir(),
		Do: func(ctx context.Context, req *http.Request) (*http.Response, error) {
			return http.DefaultClient.Do(req)
		},
		Headers: func() http.Header { return http.Header{"X-Hwid": {"test"}} },
		Allowed: func() []string { return []string{"127.0.0.1", "localhost"} },
		Mirrors: func() []string { return mirrors },
	})
}

func keyFor(t *testing.T, server *httptest.Server, path string) keys.Key {
	u := server.URL + path
	return keys.Key{URL: u, Host: "127.0.0.1", Path: path}
}

func TestImportStoresServersAndHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/abc/json" || r.Header.Get("x-hwid") != "test" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("profile-title", "base64:RnVya1ZQTg==")
		w.Header().Set("subscription-userinfo", "upload=1; download=2; total=0; expire=1790000000")
		w.Header().Set("profile-update-interval", "12")
		w.Write([]byte(body))
	}))
	defer server.Close()
	r := repo(t)
	info, err := r.Import(context.Background(), keyFor(t, server, "/abc"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Title != "FurkVPN" || *info.Download != 2 || info.Total != nil || *info.Expire != 1790000000 || info.UpdateIntervalHours != 12 {
		t.Fatalf("%+v", info)
	}
	if len(info.Servers) != 2 || r.ConfigFor(info.Servers[0].Key) == nil || r.Selected("").Key != info.Servers[0].Key {
		t.Fatal("servers")
	}
	// It survives a restart.
	again := Open(Options{Dir: r.dir})
	if again.Info() == nil || again.Info().URL != info.URL || again.ConfigFor(info.Servers[0].Key) == nil {
		t.Fatal("not stored")
	}
	if !again.IsStale(1, time.Now().Add(2*time.Hour)) || again.IsStale(0, time.Now().Add(2*time.Hour)) || again.IsStale(-1, time.Now().Add(100*time.Hour)) {
		t.Fatal("staleness")
	}
}

func TestSlowHostFallsBackToMirror(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(10 * time.Second):
		}
	}))
	defer slow.Close()
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/abc/json" {
			w.Write([]byte(body))
		}
	}))
	defer mirror.Close()
	r := repo(t, strings.Replace(mirror.URL, "127.0.0.1", "localhost", 1))
	start := time.Now()
	if _, err := r.Import(context.Background(), keyFor(t, slow, "/abc")); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took < 3*time.Second || took > 6*time.Second {
		t.Fatalf("mirror after the stagger, took %v", took)
	}
}

func TestMeaningfulErrors(t *testing.T) {
	notFound := httptest.NewServer(http.NotFoundHandler())
	defer notFound.Close()
	if _, err := repo(t).Import(context.Background(), keyFor(t, notFound, "/abc")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("%v", err)
	}
	garbage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("vless://x")) }))
	defer garbage.Close()
	if _, err := repo(t).Import(context.Background(), keyFor(t, garbage, "/abc")); !errors.Is(err, ErrBadFormat) {
		t.Fatalf("%v", err)
	}
	onlyHeaders := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"remarks":"⬇️ x","outbounds":[{"protocol":"vless","settings":{"vnext":[{"address":"0.0.0.0"}]}}]}]`))
	}))
	defer onlyHeaders.Close()
	if _, err := repo(t).Import(context.Background(), keyFor(t, onlyHeaders, "/abc")); !errors.Is(err, ErrNoServers) {
		t.Fatalf("%v", err)
	}
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://127.0.0.2:1/elsewhere", http.StatusFound)
	}))
	defer redirect.Close()
	if _, err := repo(t).Import(context.Background(), keyFor(t, redirect, "/abc")); err == nil {
		t.Fatal("a redirect outside our domains must fail")
	}
}
