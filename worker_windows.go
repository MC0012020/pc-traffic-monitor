package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Settings struct{}
type Update struct {
	Status, ProxyStatus, LocalStatus string
	Connected, Ready, LocalReady     bool
	Speed, Day                       Counts
	Seconds, ProxySeconds            float64
	Rows                             [2][]ViewRow
}
type APIResult struct {
	connected   bool
	counts      Counts
	rows        []Activity
	connections []Connection
	seconds     float64
	when        time.Time
}

func dataPath() string {
	p := os.Getenv("LOCALAPPDATA")
	if p == "" {
		p = os.TempDir()
	}
	return filepath.Join(p, "PcTrafficMonitorV31", "daily.json")
}
func sumText(b Bytes) string { return human(float64(b.sum())) }
func proxyLoop(out chan<- APIResult, done <-chan struct{}) {
	var selected Target
	var tr Tracker
	var types map[string]string
	var typesAt time.Time
	emit := func(r APIResult) bool {
		select {
		case out <- r:
			return true
		case <-done:
			return false
		}
	}
	for {
		select {
		case <-done:
			return
		default:
		}
		var snapshot Snapshot
		ok := false
		if selected.Address != "" {
			s, e := fetch(selected)
			if e == nil {
				snapshot = s
				ok = true
			} else {
				selected = Target{}
				tr.Reset()
				types = nil
				typesAt = time.Time{}
			}
		}
		if !ok {
			for _, target := range discover("") {
				select {
				case <-done:
					return
				default:
				}
				s, e := fetch(target)
				if e == nil {
					snapshot = s
					selected = target
					ok = true
					break
				}
			}
		}
		if !ok {
			if !emit(APIResult{when: time.Now()}) {
				return
			}
			select {
			case <-done:
				return
			case <-time.After(3 * time.Second):
			}
			continue
		}
		if time.Since(typesAt) > 5*time.Second {
			m, e := fetchTypes(selected)
			if e == nil {
				types = m
			} else {
				types = nil
			}
			typesAt = time.Now()
		}
		now := time.Now()
		old := tr.Last
		for i := range snapshot.Connections {
			c := &snapshot.Connections[i]
			c.OutboundType = classifyChain(*c, types)
		}
		counts, rows, sec := tr.Step(snapshot, now, selected.Address)
		// Drop rows whose route changed and reconcile each direction to official counter deltas.
		for i := range rows {
			a := &rows[i]
			if p, ok := old[a.Conn.ID]; ok && route(p) != route(a.Conn) {
				a.Delta = Bytes{}
			}
		}
		for _, kind := range []int{Proxy, Direct} {
			var used Bytes
			for i := range rows {
				a := &rows[i]
				if route(a.Conn) != kind {
					continue
				}
				if a.Delta.Down > diff(counts[kind].Down, used.Down) {
					a.Delta.Down = diff(counts[kind].Down, used.Down)
				}
				if a.Delta.Up > diff(counts[kind].Up, used.Up) {
					a.Delta.Up = diff(counts[kind].Up, used.Up)
				}
				used = used.add(a.Delta)
			}
		}
		if !emit(APIResult{true, counts, rows, snapshot.Connections, sec, now}) {
			return
		}
		select {
		case <-done:
			return
		case <-time.After(800 * time.Millisecond):
		}
	}
}
func views(rows map[string]FlowDatum, sec float64) []ViewRow {
	a := make([]FlowDatum, 0, len(rows))
	for _, v := range rows {
		if v.Bytes.sum() > 0 {
			a = append(a, v)
		}
	}
	sort.Slice(a, func(i, j int) bool { return a[i].Bytes.sum() > a[j].Bytes.sum() })
	if len(a) > maxRecentRows {
		a = a[:maxRecentRows]
	}
	r := make([]ViewRow, 0, len(a))
	for _, v := range a {
		r = append(r, ViewRow{v.Name, v.Key.Remote, human(float64(v.Bytes.Down)/sec) + "/s", human(float64(v.Bytes.Up)/sec) + "/s", float64(v.Bytes.sum()) / sec})
	}
	return r
}
func addRow(m map[string]FlowDatum, name, target string, b Bytes) {
	if b.sum() == 0 {
		return
	}
	key := name + "\n" + target
	v := m[key]
	v.Name = name
	v.Key.Remote = target
	v.Bytes = v.Bytes.add(b)
	m[key] = v
}
func coreLeg(v FlowDatum, o Owners, matched map[string]time.Time, tun map[string]bool) bool {
	if coreName(v.Name) || o.core[v.Key.PID] {
		return true
	}
	remote := net.ParseIP(v.Key.Remote)
	local := net.ParseIP(v.Key.Local)
	if (remote != nil && remote.IsLoopback() && o.ports[v.Key.RP]) || (local != nil && local.IsLoopback() && o.ports[v.Key.LP]) {
		return true
	}
	if _, ok := matched[fmt.Sprintf("%s:%d", v.Key.Local, v.Key.LP)]; ok {
		return true
	}
	if _, ok := matched[fmt.Sprintf("%s:%d", v.Key.Remote, v.Key.RP)]; ok {
		return true
	}
	if v4 := remote.To4(); v4 != nil && v4[0] == 198 && (v4[1] == 18 || v4[1] == 19) && o.coreRunning {
		return true
	}
	if o.coreRunning {
		if b, ok := tun[v.Key.Remote]; ok {
			return b
		}
		b := o.viaVirtual(v.Key.Remote)
		tun[v.Key.Remote] = b
		return b
	}
	return false
}
func runWorker(_ <-chan Settings, updates chan Update, done <-chan struct{}, stopped chan<- struct{}) {
	defer close(stopped)
	c := newCapture()
	c.start()
	defer c.stop()
	ledger := loadLedger(dataPath())
	defer func() { _ = saveLedger(dataPath(), ledger) }()
	api := make(chan APIResult, 32)
	go proxyLoop(api, done)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	last := time.Now()
	lastSave := last
	var latest APIResult
	matched := map[string]time.Time{}
	var proxyRows = map[string]FlowDatum{}
	var directRows = map[string]FlowDatum{}
	var counts Counts
	var proxySec float64
	for {
		select {
		case <-done:
			return
		case ar := <-api:
			latest = ar
			if !ar.connected {
				continue
			}
			counts = counts.add(ar.counts)
			proxySec += ar.seconds
			o := getOwners(c)
			for _, conn := range ar.connections {
				if conn.Metadata.SourcePort != "" && conn.Metadata.SourceIP != "" {
					matched[conn.Metadata.SourceIP+":"+conn.Metadata.SourcePort] = ar.when
				}
			}
			for _, a := range ar.rows {
				kind := route(a.Conn)
				if kind != Proxy && kind != Direct {
					continue
				}
				meta := a.Conn.Metadata
				name := meta.Process
				if name == "" {
					name = filepath.Base(meta.ProcessPath)
				}
				if name == "" || name == "." {
					p, _ := strconv.Atoi(meta.SourcePort)
					name = o.resolve(meta.SourceIP, uint16(p))
					if name == "" {
						name = "未知程序"
					}
				}
				host := meta.Host
				if host == "" {
					host = meta.DestinationIP
				}
				m := proxyRows
				if kind == Direct {
					m = directRows
				}
				addRow(m, name, host, a.Delta)
			}
		case now := <-ticker.C:
			sec := now.Sub(last).Seconds()
			if sec < .01 {
				sec = 1
			}
			last = now
			for key, t := range matched {
				if now.Sub(t) > 8*time.Second {
					delete(matched, key)
				}
			}
			o := getOwners(c)
			flows, etwStatus, lost := c.snapshot()
			var local Bytes
			tun := map[string]bool{}
			for _, v := range flows {
				if coreLeg(v, o, matched, tun) {
					continue
				}
				local = local.add(v.Bytes)
				addRow(directRows, v.Name, v.Key.Remote, v.Bytes)
			}
			// Only classified DIRECT API data + non-proxy ETW events. Never subtract mismatched counters.
			local = local.add(counts[Direct])
			var delta Counts
			delta[Proxy] = counts[Proxy]
			delta[Direct] = local
			delta[Unknown] = counts[Unknown]
			ledger.Add(now, delta)
			// Daily-only persistence (no month statistics).
			for k := range ledger.Days {
				if k != now.Format("2006-01-02") {
					delete(ledger.Days, k)
				}
			}
			u := Update{Connected: latest.connected && now.Sub(latest.when) < 5*time.Second, Ready: true, LocalReady: strings.Contains(etwStatus, "实时采集"), Speed: delta, Day: ledger.Days[now.Format("2006-01-02")], Seconds: sec, ProxySeconds: proxySec, Status: etwStatus, LocalStatus: "本地网络", ProxyStatus: "Clash 未开启"}
			if o.coreRunning {
				u.ProxyStatus = "Clash 接口未连接"
			}
			if u.Connected {
				u.ProxyStatus = "Clash 已连接"
			}
			if lost > 0 {
				u.Status += fmt.Sprintf(" · 丢失事件 %d", lost)
			}
			if o.coreRunning && !u.Connected {
				u.Status += " · 代理分类暂不可用"
			}
			if counts[Unknown].sum() > 0 {
				u.Status += " · 部分短连接未归类"
			}
			if proxySec > 0 {
				u.Rows[Proxy] = views(proxyRows, proxySec)
			}
			u.Rows[Direct] = views(directRows, sec)
			counts = Counts{}
			proxySec = 0
			proxyRows = map[string]FlowDatum{}
			directRows = map[string]FlowDatum{}
			if now.Sub(lastSave) >= 5*time.Second {
				if e := saveLedger(dataPath(), ledger); e != nil {
					u.Status += " · 保存失败"
				}
				lastSave = now
			}
			select {
			case updates <- u:
			default:
				select {
				case <-updates:
				default:
				}
				select {
				case updates <- u:
				default:
				}
			}
		}
	}
}
