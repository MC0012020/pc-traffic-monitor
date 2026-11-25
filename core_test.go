package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func snap(d, u uint64, c ...Connection) Snapshot { return Snapshot{&d, &u, c} }
func TestTracker(t *testing.T) {
	now := time.Now()
	a := Connection{ID: "a", Download: 100, Upload: 10, Chains: []string{"node", "group"}, Start: now}
	b := Connection{ID: "b", Download: 20, Chains: []string{"DIRECT"}, Start: now}
	var tr Tracker
	c, _, _ := tr.Step(snap(120, 10, a, b), now, "pipe")
	if c != (Counts{}) {
		t.Fatal(c)
	}
	a.Download = 150
	a.Upload = 15
	b.Download = 40
	c, _, _ = tr.Step(snap(210, 15, a, b), now.Add(time.Second), "pipe")
	if c[Proxy] != (Bytes{50, 5}) || c[Direct].Down != 20 || c[Unknown].Down != 20 {
		t.Fatal(c)
	}
	c, _, _ = tr.Step(snap(0, 0), now.Add(2*time.Second), "pipe")
	if c != (Counts{}) {
		t.Fatal(c)
	}
	c, _, _ = tr.Step(snap(300, 50, a, b), now.Add(20*time.Second), "pipe")
	if c[Unknown] != (Bytes{300,50}) || c[Direct] != (Bytes{}) || c[Proxy] != (Bytes{}) {
		t.Fatal(c)
	}
}
func TestLedger(t *testing.T) {
	p := filepath.Join(t.TempDir(), "traffic.json")
	l := loadLedger(p)
	n := time.Now()
	l.Add(n, Counts{{100, 5}, {20, 1}, {3, 0}})
	if e := saveLedger(p, l); e != nil {
		t.Fatal(e)
	}
	l.Add(n, Counts{{1, 0}})
	if e := saveLedger(p, l); e != nil {
		t.Fatal(e)
	}
	d, m := loadLedger(p).Totals(n)
	if d[0].Down != 101 || m != d {
		t.Fatal(d, m)
	}
	_, _ = os.Stat(p)
}

func TestCustomDirectAndUnknown(t *testing.T) {
	for _, v := range []struct {
		typ      string
		expected int
	}{{"Direct", Direct}, {"Shadowsocks", Proxy}, {"Unknown", Unknown}, {"Selector", Unknown}, {"RejectDrop", Unknown}} {
		c := Connection{Chains: []string{"custom"}, OutboundType: v.typ}
		if route(c) != v.expected {
			t.Fatal(v)
		}
	}
}
func TestReconcile(t *testing.T) {
	now := time.Now()
	var tr Tracker
	a := Connection{ID: "a", Download: 100, Start: now, Chains: []string{"DIRECT"}}
	tr.Step(snap(100, 0, a), now, "local")
	a.Download = 200
	counts, _, _ := tr.Step(snap(175, 0, a), now.Add(time.Second), "local")
	if counts[Direct].Down != 0 || counts[Unknown].Down != 75 {
		t.Fatal(counts)
	}
}

func TestOfficialChainClassification(t *testing.T) {
	types := map[string]string{"out": "Shadowsocks", "sel": "Selector", "bypass": "Direct"}
	for _, tc := range []struct {
		chain []string
		want  string
	}{{[]string{"sel", "out"}, "SHADOWSOCKS"}, {[]string{"sel", "bypass"}, "DIRECT"}, {[]string{"DIRECT"}, "DIRECT"}, {[]string{"sel", "unrecognized"}, "UNKNOWN"}} {
		if got := classifyChain(Connection{Chains: tc.chain}, types); got != tc.want {
			t.Fatalf("%v: got %s want %s", tc.chain, got, tc.want)
		}
	}
}

func TestRecentRows(t *testing.T) {
	var r RecentRows
	n := time.Now()
	a := ViewRow{"chrome.exe", "example.com", "10 B/s", "2 B/s", 12}
	b := ViewRow{"game.exe", "192.168.1.2", "20 B/s", "1 B/s", 21}
	first := r.Update([2][]ViewRow{{a}, {b}}, n, 9)
	if len(first) != 2 || first[0].Name != b.Name || !first[0].Active {
		t.Fatal(first)
	}
	second := r.Update([2][]ViewRow{{a}, nil}, n.Add(time.Second), 9)
	if len(second) != 2 || second[0].Name != a.Name || second[1].Active || second[1].Down != "0 B/s" || second[1].Up != "0 B/s" || !second[1].Seen.Equal(n) {
		t.Fatal(second)
	}
	third := r.Update([2][]ViewRow{}, n.Add(time.Hour), 9)
	if len(third) != 2 || third[0].Active || third[1].Active || third[0].Down != "0 B/s" {
		t.Fatal(third)
	}
}

func TestRecentFiveActiveFirst(t *testing.T) {
	var r RecentRows
	n := time.Now()
	input := [2][]ViewRow{{{"a", "a", "1 B/s", "0 B/s", 1}, {"b", "b", "2 B/s", "0 B/s", 2}, {"c", "c", "3 B/s", "0 B/s", 3}}, {{"d", "d", "4 B/s", "0 B/s", 4}, {"e", "e", "5 B/s", "0 B/s", 5}, {"f", "f", "6 B/s", "0 B/s", 6}}}
	out := r.Update(input, n, 5)
	if len(out) != 5 || out[0].Name != "f" || out[4].Name != "b" {
		t.Fatal(out)
	}
	out = r.Update([2][]ViewRow{{input[0][0]}, nil}, n.Add(time.Second), 5)
	if len(out) != 5 || out[0].Name != "a" || !out[0].Active || out[1].Active || out[1].Down != "0 B/s" {
		t.Fatal(out)
	}
}

func TestRecentResizeKeepsHiddenRows(t *testing.T) {
	var r RecentRows
	now := time.Now()
	var input [2][]ViewRow
	for i := 1; i <= 40; i++ {
		input[0] = append(input[0], ViewRow{Name: fmt.Sprintf("p%d", i), Target: "example", Rate: float64(i)})
	}
	full := r.Update(input, now, maxRecentRows)
	if len(full) != 40 || full[0].Name != "p40" {
		t.Fatal(full)
	}
	small := full[:5]
	large := full[:25]
	if len(small) != 5 || len(large) != 25 || large[24].Name != "p16" {
		t.Fatal(small, large)
	}
	next := r.Update([2][]ViewRow{}, now.Add(time.Second), maxRecentRows)
	if len(next) != 40 || next[0].Active || next[0].Down != "0 B/s" {
		t.Fatal(next)
	}
}

func TestTrackerLongGapPreservesAuthoritativeBytes(t *testing.T) {
 var tr Tracker
 now:=time.Now()
 tr.Step(snap(100,20),now,"pipe")
 c,_,sec:=tr.Step(snap(3100,1020),now.Add(20*time.Second),"pipe")
 if c[Unknown]!=(Bytes{3000,1000})||c[Direct]!=(Bytes{})||c[Proxy]!=(Bytes{})||sec!=20{t.Fatal(c,sec)}
}
