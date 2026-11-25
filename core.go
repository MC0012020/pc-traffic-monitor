package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Bytes struct{ Down, Up uint64 }

func (b Bytes) add(c Bytes) Bytes { return Bytes{b.Down + c.Down, b.Up + c.Up} }
func (b Bytes) sub(c Bytes) Bytes { return Bytes{diff(b.Down, c.Down), diff(b.Up, c.Up)} }
func (b Bytes) sum() uint64       { return b.Down + b.Up }
func diff(a, b uint64) uint64 {
	if a < b {
		return 0
	}
	return a - b
}

type Connection struct {
	ID           string    `json:"id"`
	Download     uint64    `json:"download"`
	Upload       uint64    `json:"upload"`
	Start        time.Time `json:"start"`
	Chains       []string  `json:"chains"`
	OutboundType string    `json:"-"`
	Metadata     struct {
		Process       string `json:"process"`
		ProcessPath   string `json:"processPath"`
		Host          string `json:"host"`
		DestinationIP string `json:"destinationIP"`
		SourceIP      string `json:"sourceIP"`
		SourcePort    string `json:"sourcePort"`
	} `json:"metadata"`
}

func (c Connection) bytes() Bytes { return Bytes{c.Download, c.Upload} }

type Snapshot struct {
	DownloadTotal *uint64      `json:"downloadTotal"`
	UploadTotal   *uint64      `json:"uploadTotal"`
	Connections   []Connection `json:"connections"`
}

func (s Snapshot) total() (Bytes, bool) {
	if s.DownloadTotal == nil || s.UploadTotal == nil {
		return Bytes{}, false
	}
	return Bytes{*s.DownloadTotal, *s.UploadTotal}, true
}

const (
	Proxy   = 0
	Direct  = 1
	Unknown = 2
)

type Counts [3]Bytes

func (c Counts) add(d Counts) Counts {
	for i := range c {
		c[i] = c[i].add(d[i])
	}
	return c
}
func route(c Connection) int {
	if len(c.Chains) == 0 {
		return Unknown
	}
	switch strings.ToUpper(strings.TrimSpace(c.OutboundType)) {
	case "DIRECT", "COMPATIBLE":
		return Direct
	case "REJECT", "REJECTDROP", "REJECT-DROP", "PASS", "UNKNOWN", "SELECTOR", "URLTEST", "FALLBACK", "LOADBALANCE", "RELAY":
		return Unknown
	case "": // legacy fallback for tests; production marks unverified chains unknown
	default:
		return Proxy
	}
	switch strings.ToUpper(strings.TrimSpace(c.Chains[0])) {
	case "DIRECT", "COMPATIBLE":
		return Direct
	case "REJECT", "REJECT-DROP", "PASS":
		return Unknown
	}
	return Proxy
}
func routeName(i int) string { return []string{"代理", "直连", "未知"}[i] }

type Activity struct {
	Conn  Connection
	Delta Bytes
}
type Tracker struct {
	Last     map[string]Connection
	At       time.Time
	Total    Bytes
	HasTotal bool
	Source   string
}

func (t *Tracker) Reset() { *t = Tracker{} }
func (t *Tracker) Step(s Snapshot, now time.Time, source string) (Counts, []Activity, float64) {
	var counts Counts
	var rows []Activity
	total, hasTotal := s.total()
	elapsed := now.Sub(t.At).Seconds()
	baseline := t.At.IsZero() || t.Source != source || elapsed <= 0 || elapsed > 5 || (hasTotal && t.HasTotal && (total.Down < t.Total.Down || total.Up < t.Total.Up))
	next := make(map[string]Connection, len(s.Connections))
	for _, c := range s.Connections {
		if c.ID == "" {
			continue
		}
		next[c.ID] = c
		var delta Bytes
		if !baseline {
			if old, ok := t.Last[c.ID]; ok {
				delta = c.bytes().sub(old.bytes())
			} else if !c.Start.IsZero() && !c.Start.Before(t.At) {
				delta = c.bytes()
			}
			i := route(c)
			if old, found := t.Last[c.ID]; found && route(old) != i {
				i = Unknown
			}
			counts[i] = counts[i].add(delta)
		}
		rows = append(rows, Activity{c, delta})
	}
	if !baseline && hasTotal && t.HasTotal {
		overall := total.sub(t.Total)
		classified := counts[0].add(counts[1]).add(counts[2])
		// Snapshot counters can be read at slightly different instants. If the per-connection
		// sum exceeds the total, preserve the total and do not guess the classification.
		if classified.Down > overall.Down {
			for i := range counts {
				counts[i].Down = 0
			}
			counts[Unknown].Down = overall.Down
		} else {
			counts[Unknown].Down += overall.Down - classified.Down
		}
		if classified.Up > overall.Up {
			for i := range counts {
				counts[i].Up = 0
			}
			counts[Unknown].Up = overall.Up
		} else {
			counts[Unknown].Up += overall.Up - classified.Up
		}
	}
	t.Last = next
	t.At = now
	t.Total = total
	t.HasTotal = hasTotal
	t.Source = source
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Delta.sum() == rows[j].Delta.sum() {
			return rows[i].Conn.bytes().sum() > rows[j].Conn.bytes().sum()
		}
		return rows[i].Delta.sum() > rows[j].Delta.sum()
	})
	if baseline {
		elapsed = 1
	}
	return counts, rows, elapsed
}

type Ledger struct {
	Days    map[string]Counts `json:"days"`
	QuotaGB float64           `json:"quotaGB"`
}

func (l *Ledger) Add(now time.Time, c Counts) {
	if l.Days == nil {
		l.Days = map[string]Counts{}
	}
	k := now.Format("2006-01-02")
	l.Days[k] = l.Days[k].add(c)
}
func (l Ledger) Totals(now time.Time) (Counts, Counts) {
	day := l.Days[now.Format("2006-01-02")]
	var month Counts
	for k, c := range l.Days {
		if strings.HasPrefix(k, now.Format("2006-01")) {
			month = month.add(c)
		}
	}
	return day, month
}
func loadLedger(p string) Ledger {
	var l Ledger
	b, e := os.ReadFile(p)
	if e == nil {
		_ = json.Unmarshal(b, &l)
	}
	if l.Days == nil {
		l.Days = map[string]Counts{}
	}
	return l
}
func saveLedger(p string, l Ledger) error {
	b, e := json.MarshalIndent(l, "", "  ")
	if e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(p), 0700); e != nil {
		return e
	}
	if e = os.WriteFile(p+".tmp", b, 0600); e != nil {
		return e
	}
	return os.Rename(p+".tmp", p)
}
func human(n float64) string {
	for _, u := range []string{"B", "KB", "MB", "GB", "TB"} {
		if n < 1024 || u == "TB" {
			if u == "B" {
				return fmt.Sprintf("%.0f %s", n, u)
			}
			return fmt.Sprintf("%.2f %s", n, u)
		}
		n /= 1024
	}
	return "0 B"
}
func short(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

func classifyChain(c Connection, types map[string]string) string {
	for _, name := range c.Chains {
		t := strings.ToUpper(types[name])
		switch t {
		case "SHADOWSOCKS", "SHADOWSOCKSR", "VMESS", "VLESS", "TROJAN", "HTTP", "SOCKS5", "SNELL", "WIREGUARD", "HYSTERIA", "HYSTERIA2", "TUIC", "SSH", "ANYTLS", "MIERU", "MASQUE", "SUDOKU", "TRUSTTUNNEL":
			return t
		}
	}
	for _, name := range c.Chains {
		t := strings.ToUpper(types[name])
		if t == "DIRECT" || t == "COMPATIBLE" || strings.EqualFold(name, "DIRECT") {
			return "DIRECT"
		}
	}
	return "UNKNOWN"
}
