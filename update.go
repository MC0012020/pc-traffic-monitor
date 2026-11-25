package main

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

//go:embed VERSION
var versionText string

func appVersion() string { return strings.TrimSpace(versionText) }

func parseVersion(s string) ([3]int, bool) {
	var v [3]int
	s = strings.TrimPrefix(s, "v")
	p := strings.Split(s, ".")
	if len(p) != 3 { return v, false }
	for i, n := range p {
		if n == "" || len(n) > 8 { return v, false }
		for _, c := range n { if c < '0' || c > '9' { return v, false } }
		x, e := strconv.Atoi(n)
		if e != nil || strconv.Itoa(x) != n { return v, false }
		v[i] = x
	}
	return v, true
}
func newer(candidate, current string) bool {
	a, ok := parseVersion(candidate); b, good := parseVersion(current)
	if !ok || !good { return false }
	for i := range a { if a[i] != b[i] { return a[i] > b[i] } }
	return false
}
const releaseBase = "https://github.com/MC0012020/pc-traffic-monitor/releases/download/"
type ReleaseAsset struct { Name string; URL string `json:"browser_download_url"`; Size int64 }
type ReleaseInfo struct { Tag string `json:"tag_name"`; Draft, Prerelease bool; Assets []ReleaseAsset }
type UpdateOffer struct { Tag string; Exe, Sums ReleaseAsset }
func pickUpdate(rs []ReleaseInfo, current string) (UpdateOffer, bool) {
	var best UpdateOffer
	for _, r := range rs {
		if r.Draft || !strings.HasPrefix(r.Tag,"v") || !newer(r.Tag,current) || (best.Tag!="" && !newer(r.Tag,best.Tag)) { continue }
		name := "PcTrafficMonitor_V"+strings.TrimSuffix(strings.TrimPrefix(r.Tag,"v"),".0")+".exe"
		var exe, sums ReleaseAsset
		for _, a := range r.Assets {
			if a.URL != releaseBase+r.Tag+"/"+a.Name || a.Size <= 0 { continue }
			if a.Name==name && a.Size<=64<<20 { exe=a }
			if a.Name=="SHA256SUMS.txt" && a.Size<=64<<10 { sums=a }
		}
		if exe.Name!="" && sums.Name!="" { best=UpdateOffer{r.Tag,exe,sums} }
	}
	return best,best.Tag!=""
}
func expectedHash(manifest, filename string) (string,error) {
	var found string
	for _, line := range strings.Split(manifest,"\n") {
		fields:=strings.Fields(line)
		if len(fields)!=2 || strings.TrimPrefix(fields[1],"*")!=filename {continue}
		h:=strings.ToLower(fields[0]); b,e:=hex.DecodeString(h)
		if e!=nil || len(b)!=sha256.Size || found!="" {return "",fmt.Errorf("invalid or duplicate SHA256")}
		found=h
	}
	if found=="" {return "",fmt.Errorf("SHA256 entry missing")}
	return found,nil
}
func verifyHash(data []byte, hash string) bool { return fmt.Sprintf("%x",sha256.Sum256(data))==hash }

// Percentage is computed from received bytes, never elapsed time.
func downloadPercent(received,total int64) int {
 if total<=0||received<=0{return 0};if received>=total{return 100};return int(received*100/total)
}
