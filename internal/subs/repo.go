package subs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"furkvpn/internal/jsonx"
	"furkvpn/internal/keys"
	"furkvpn/internal/settings"
)

// Errors of a download, from the most to the least telling.
var (
	ErrNotFound  = errors.New("subscription not found")
	ErrForeign   = errors.New("foreign subscription host")
	ErrBadFormat = errors.New("unsupported subscription format")
	ErrNoServers = errors.New("subscription has no servers")
)

// NetworkError is a download that failed for network reasons.
type NetworkError struct{ Err error }

func (e *NetworkError) Error() string { return "network: " + e.Err.Error() }
func (e *NetworkError) Unwrap() error { return e.Err }

// Doer sends a request: through the VPN when it is on, falling back to a direct connection.
type Doer func(ctx context.Context, req *http.Request) (*http.Response, error)

type allowedHostsKey struct{}

// WithAllowedHosts marks requests whose redirects may only lead to these hosts; the HTTP client
// checks it before following one, so device headers never reach another site.
func WithAllowedHosts(ctx context.Context, hosts []string) context.Context {
	return context.WithValue(ctx, allowedHostsKey{}, hosts)
}

// AllowedHosts returns what WithAllowedHosts set, or nil.
func AllowedHosts(ctx context.Context) []string {
	hosts, _ := ctx.Value(allowedHostsKey{}).([]string)
	return hosts
}

// Repo downloads and keeps the subscription.
type Repo struct {
	dir     string
	do      Doer
	headers func() http.Header
	allowed func() []string
	mirrors func() []string

	op       sync.Mutex // one download at a time
	mu       sync.RWMutex
	info     *Info
	configs  []json.RawMessage
	onChange func()
}

// Options of a repository.
type Options struct {
	Dir string
	Do  Doer
	// Device headers for the panel (x-hwid, x-device-os, ...).
	Headers func() http.Header
	Allowed func() []string
	Mirrors func() []string
	// Called after the subscription changes.
	OnChange func()
}

// Open loads the stored subscription.
func Open(o Options) *Repo {
	r := &Repo{dir: o.Dir, do: o.Do, headers: o.Headers, allowed: o.Allowed, mirrors: o.Mirrors, onChange: o.OnChange}
	if data, err := os.ReadFile(r.infoPath()); err == nil {
		var info Info
		if json.Unmarshal(data, &info) == nil && info.URL != "" {
			r.info = &info
		}
	}
	if data, err := os.ReadFile(r.configsPath()); err == nil {
		var list []json.RawMessage
		if json.Unmarshal(data, &list) == nil {
			r.configs = list
		}
	}
	if r.info != nil && len(r.configs) != len(r.info.Servers) {
		r.info, r.configs = nil, nil
	}
	return r
}

func (r *Repo) infoPath() string    { return filepath.Join(r.dir, "subscription.json") }
func (r *Repo) configsPath() string { return filepath.Join(r.dir, "subscription_configs.json") }

// Info is the current subscription, or nil.
func (r *Repo) Info() *Info {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.info
}

// ConfigFor is the raw Xray config of a server, as served by the panel.
func (r *Repo) ConfigFor(serverKey string) []byte {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.info == nil {
		return nil
	}
	for i, e := range r.info.Servers {
		if e.Key == serverKey && i < len(r.configs) {
			return r.configs[i]
		}
	}
	return nil
}

// Selected is the selected server, or the first one that can be selected.
func (r *Repo) Selected(saved string) *Entry {
	info := r.Info()
	if info == nil {
		return nil
	}
	list := info.Selectable()
	for i := range list {
		if list[i].Key == saved {
			return &list[i]
		}
	}
	if len(list) > 0 {
		return &list[0]
	}
	return nil
}

// IsStale reports whether the subscription is due for its automatic update; updateHours is the
// setting (hours, or settings.UpdateFromSubscription / settings.UpdateManual).
func (r *Repo) IsStale(updateHours int, now time.Time) bool {
	if updateHours == settings.UpdateManual {
		return false
	}
	info := r.Info()
	if info == nil {
		return false
	}
	hours := updateHours
	if updateHours == settings.UpdateFromSubscription {
		hours = info.UpdateIntervalHours
		if hours <= 0 {
			hours = 6
		}
	}
	hours = min(max(hours, 1), 48)
	return now.UnixMilli()-info.FetchedAt > int64(hours)*3_600_000
}

// Import downloads and stores the subscription of a new key.
func (r *Repo) Import(ctx context.Context, key keys.Key) (*Info, error) {
	r.op.Lock()
	defer r.op.Unlock()
	d, err := r.download(ctx, key)
	if err != nil {
		return nil, err
	}
	return r.save(key, d)
}

// Refresh downloads the current subscription again; the old copy stays on failure.
func (r *Repo) Refresh(ctx context.Context) (*Info, error) {
	info := r.Info()
	if info == nil {
		return nil, nil
	}
	res := keys.Parse(info.URL, r.allowed())
	if res.Status != keys.OK {
		return nil, ErrForeign
	}
	r.op.Lock()
	defer r.op.Unlock()
	d, err := r.download(ctx, res.Key)
	if err != nil {
		return nil, err
	}
	return r.save(res.Key, d)
}

// Clear forgets the subscription.
func (r *Repo) Clear() {
	r.op.Lock()
	defer r.op.Unlock()
	_ = os.Remove(r.infoPath())
	_ = os.Remove(r.configsPath())
	r.mu.Lock()
	r.info, r.configs = nil, nil
	r.mu.Unlock()
	if r.onChange != nil {
		r.onChange()
	}
}

type downloaded struct {
	configs []jsonx.Object
	raw     []json.RawMessage
	header  http.Header
}

func (r *Repo) save(key keys.Key, d *downloaded) (*Info, error) {
	entries := Entries(d.configs)
	hasServer := false
	for _, e := range entries {
		if e.Kind != KindSeparator {
			hasServer = true
			break
		}
	}
	if !hasServer {
		return nil, ErrNoServers
	}
	h := d.header
	user := ParseUserInfo(h.Get("subscription-userinfo"))
	info := &Info{
		URL:        key.URL,
		Title:      DecodeHeader(h.Get("profile-title")),
		Announce:   DecodeHeader(h.Get("announce")),
		FetchedAt:  time.Now().UnixMilli(),
		Servers:    entries,
		SupportURL: strings.TrimSpace(h.Get("support-url")),
	}
	if !strings.HasPrefix(info.SupportURL, "http") {
		info.SupportURL = ""
	}
	if n, err := strconv.Atoi(strings.TrimSpace(h.Get("profile-update-interval"))); err == nil {
		info.UpdateIntervalHours = n
	}
	if v, ok := user["upload"]; ok {
		info.Upload = &v
	}
	if v, ok := user["download"]; ok {
		info.Download = &v
	}
	if v, ok := user["total"]; ok && v > 0 {
		info.Total = &v
	}
	if v, ok := user["expire"]; ok && v > 0 {
		info.Expire = &v
	}
	configsJSON, err := json.Marshal(d.raw)
	if err != nil {
		return nil, err
	}
	infoJSON, err := json.Marshal(info)
	if err != nil {
		return nil, err
	}
	if err := settings.WriteAtomically(r.configsPath(), configsJSON); err != nil {
		return nil, err
	}
	if err := settings.WriteAtomically(r.infoPath(), infoJSON); err != nil {
		return nil, err
	}
	r.mu.Lock()
	r.info, r.configs = info, d.raw
	r.mu.Unlock()
	if r.onChange != nil {
		r.onChange()
	}
	return info, nil
}

// download tries the key's own host first and, if it is slow or unreachable, the mirror domains
// with the same path. The first valid response wins.
func (r *Repo) download(ctx context.Context, key keys.Key) (*downloaded, error) {
	allowed := r.allowed()
	candidates := []string{key.URL + "/json"}
	for _, mirror := range r.mirrors() {
		base := strings.TrimRight(strings.TrimSpace(mirror), "/")
		host := base
		if i := strings.Index(host, "://"); i >= 0 {
			host = host[i+3:]
		}
		host = strings.SplitN(strings.SplitN(host, "/", 2)[0], ":", 2)[0]
		if host != "" && host != key.Host && keys.HostAllowed(host, allowed) {
			u := base + key.Path + "/json"
			if !contains(candidates, u) {
				candidates = append(candidates, u)
			}
		}
	}
	tasks := make([]func(context.Context) (*downloaded, error), len(candidates))
	for i, u := range candidates {
		u := u
		tasks[i] = func(ctx context.Context) (*downloaded, error) { return r.fetch(ctx, u, allowed) }
	}
	return RaceStaggered(ctx, tasks, 3*time.Second, pickError)
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func (r *Repo) fetch(ctx context.Context, u string, allowed []string) (*downloaded, error) {
	req, err := http.NewRequestWithContext(WithAllowedHosts(ctx, allowed), http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range r.headers() {
		req.Header[k] = v
	}
	resp, err := r.do(ctx, req)
	if err != nil {
		return nil, &NetworkError{err}
	}
	defer resp.Body.Close()
	// Redirects must not lead outside our domains.
	if !keys.HostAllowed(resp.Request.URL.Hostname(), allowed) {
		return nil, ErrForeign
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &NetworkError{fmt.Errorf("HTTP %d", resp.StatusCode)}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, &NetworkError{err}
	}
	configs, err := ParseConfigs(body)
	if err != nil || len(configs) == 0 {
		return nil, ErrBadFormat
	}
	raw := make([]json.RawMessage, len(configs))
	for i, c := range configs {
		b, err := jsonx.Marshal(c)
		if err != nil {
			return nil, ErrBadFormat
		}
		raw[i] = b
	}
	return &downloaded{configs: configs, raw: raw, header: resp.Header}, nil
}

func pickError(errs []error) error {
	for _, target := range []error{ErrNotFound, ErrForeign, ErrBadFormat, ErrNoServers} {
		for _, e := range errs {
			if errors.Is(e, target) {
				return target
			}
		}
	}
	for _, e := range errs {
		var ne *NetworkError
		if errors.As(e, &ne) {
			return e
		}
	}
	if len(errs) > 0 {
		return &NetworkError{errs[len(errs)-1]}
	}
	return &NetworkError{errors.New("no candidates")}
}

// RaceStaggered starts the tasks one after another with a delay (a failed task starts the next
// one at once) and returns the first successful result; when all fail, pick chooses the error.
func RaceStaggered[T any](ctx context.Context, tasks []func(context.Context) (T, error), stagger time.Duration, pick func([]error) error) (T, error) {
	var zero T
	if len(tasks) == 0 {
		return zero, pick(nil)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type result struct {
		v   T
		err error
	}
	results := make(chan result, len(tasks))
	started := 0
	start := func() {
		i := started
		started++
		go func() {
			v, err := tasks[i](ctx)
			results <- result{v, err}
		}()
	}
	start()
	timer := time.NewTimer(stagger)
	defer timer.Stop()
	var errs []error
	for {
		select {
		case <-ctx.Done():
			return zero, ctx.Err()
		case <-timer.C:
			if started < len(tasks) {
				start()
				timer.Reset(stagger)
			}
		case r := <-results:
			if r.err == nil {
				return r.v, nil
			}
			errs = append(errs, r.err)
			if len(errs) == len(tasks) {
				return zero, pick(errs)
			}
			if started < len(tasks) {
				start()
				timer.Reset(stagger)
			}
		}
	}
}
