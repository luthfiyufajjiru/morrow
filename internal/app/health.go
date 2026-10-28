package app

import (
	"fmt"
	"io"
	"morrow/internal/db"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

// Health check defaults and bounds.
const (
	DefaultHealthInterval = 10 * time.Second
	DefaultHealthFailures = 3
	maxHealthInterval     = time.Hour
	maxHealthFailures     = 100
	maxHealthTimeout      = 10 * time.Second
)

// SetHealthCheck sets the app's HTTP health check. An empty URL removes it.
func SetHealthCheck(name, rawURL string, interval time.Duration, failures int) error {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL != "" {
		u, err := url.Parse(rawURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("invalid health check URL %q (use http://host:port/path or https://...)", rawURL)
		}
	}
	if interval < time.Second || interval > maxHealthInterval || interval%time.Second != 0 {
		return fmt.Errorf("health check interval must be whole seconds between 1s and %s", maxHealthInterval)
	}
	if failures < 1 || failures > maxHealthFailures {
		return fmt.Errorf("health check failures must be between 1 and %d", maxHealthFailures)
	}
	res, err := db.DB.Exec(`
		UPDATE applications SET application_health_url = ?, application_health_interval = ?, application_health_failures = ?,
		       application_update_time = ?
		WHERE application_name = ?
	`, rawURL, int(interval/time.Second), failures, now(), name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("application %s not found", name)
	}
	return nil
}

// probe GETs the URL once: any response below 400 is healthy. Redirects are
// not followed, and proxies from the environment are not used, since the
// app is normally on this machine.
func probe(client *http.Client, target string) error {
	resp, err := client.Get(target)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

// watchHealth runs in the relay while instance pid runs. It probes the app's
// health URL every interval, starting one interval after the start, so an
// app has failures x interval to come up. After that many failures in a row
// it marks the instance unhealthy and stops it gracefully; the relay then
// restarts it. It returns when done is closed (the app exited).
func watchHealth(a *Application, pid int, done <-chan struct{}, unhealthy *atomic.Bool, logf func(string)) {
	interval := time.Duration(a.HealthInterval) * time.Second
	if interval <= 0 {
		interval = DefaultHealthInterval
	}
	limit := a.HealthFailures
	if limit <= 0 {
		limit = DefaultHealthFailures
	}
	client := &http.Client{
		Timeout:       min(interval, maxHealthTimeout),
		Transport:     &http.Transport{Proxy: nil, DisableKeepAlives: true},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	tick := time.NewTicker(interval)
	defer tick.Stop()
	fails := 0
	for {
		select {
		case <-done:
			return
		case <-tick.C:
		}
		err := probe(client, a.HealthURL)
		if err == nil {
			if fails > 0 {
				logf("[morrow] health check passed again")
			}
			fails = 0
			continue
		}
		fails++
		logf(fmt.Sprintf("[morrow] health check failed (%d/%d): %v", fails, limit, err))
		if fails < limit {
			continue
		}
		// Only an instance that is still running as far as anyone knows: a
		// stop by the user is already under way otherwise.
		var n int
		if db.DB.QueryRow("SELECT count(*) FROM applications WHERE application_name = ? AND application_pid = ? AND application_status = 'running'",
			a.Name, pid).Scan(&n); n == 0 {
			return
		}
		unhealthy.Store(true)
		logf(fmt.Sprintf("[morrow] unhealthy after %d failed health checks; stopping it to restart", fails))
		if err := gracefulStop(pid, DefaultStopTimeout); err != nil {
			logf(fmt.Sprintf("[morrow] failed to stop the unhealthy app: %v", err))
		}
		return
	}
}
