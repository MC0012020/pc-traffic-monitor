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
// Match only the complete observed transport tuple. A source port alone,
// a private address, or a generic VPN route is not proof of proxy ownership.
func connectionFlowKey(c Connection) (FlowKey,bool) {
 m:=c.Metadata
 local,remote:=net.ParseIP(m.SourceIP),net.ParseIP(m.DestinationIP)
 lp,e:=strconv.ParseUint(m.SourcePort,10,16);rp,err:=strconv.ParseUint(m.DestinationPort,10,16)
 protocol:=strings.ToLower(m.Network)
 if local==nil||remote==nil||e!=nil||err!=nil||lp==0||rp==0||(protocol!="udp"&&protocol!="tcp"){return FlowKey{},false}
 return FlowKey{Local:local.String(),Remote:remote.String(),LP:uint16(lp),RP:uint16(rp),UDP:protocol=="udp"},true
}
func coreLeg(v FlowDatum,o Owners,matched map[FlowKey]time.Time,apiReady bool) int {
 if coreName(v.Name)||o.core[v.Key.PID] {
  if apiReady{return -1}
  return Unknown
 }
 key:=v.Key;key.PID=0
 if apiReady {
  if _,ok:=matched[key];ok{return -1}
 }
 remote:=net.ParseIP(key.Remote)
 // An exact TCP listener owned by the proxy is an internal leg, not an
 // independent internet connection. Unknown when the API is unavailable.
 if !key.UDP&&remote!=nil&&remote.IsLoopback()&&o.ports[key.RP] {
  if apiReady{return -1}
  return Unknown
 }
 return Direct
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
	matched := map[FlowKey]time.Time{}
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
				if key,ok:=connectionFlowKey(conn);ok{matched[key]=ar.when}
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
		case <-ticker.C:
			now := time.Now()
			sec := now.Sub(last).Seconds()
			if sec < .01 {
				sec = 1
			}
			last = now
			for key, t := range matched {
				if now.Sub(t) > 3*time.Second {
					delete(matched, key)
				}
			}
			o := getOwners(c)
			flows, etwStatus, lost, overflow := c.snapshot()
			var local, unclassified Bytes
			apiReady:=latest.connected&&now.Sub(latest.when)<5*time.Second
			for _, v := range flows {
				switch coreLeg(v,o,matched,apiReady){
				case Direct: local=local.add(v.Bytes);addRow(directRows,v.Name,v.Key.Remote,v.Bytes)
				case Unknown: unclassified=unclassified.add(v.Bytes)
				}
			}
			unclassified=unclassified.add(overflow)
			// Only classified DIRECT API data + non-proxy ETW events. Never subtract mismatched counters.
			local = local.add(counts[Direct])
			var delta Counts
			delta[Proxy] = counts[Proxy]
			delta[Direct] = local
			delta[Unknown] = counts[Unknown].add(unclassified)
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
			if overflow.sum()>0 {u.Status += " · 连接容量超限"}
			if lost > 0 {
				u.Status += fmt.Sprintf(" · 丢失事件 %d", lost)
			}
			if o.coreRunning && !u.Connected {
				u.Status += " · 代理分类暂不可用"
			}
			if delta[Unknown].sum() > 0 {
				u.Status += " · 未归类 "+human(float64(delta[Unknown].sum())/sec)+"/s"
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
