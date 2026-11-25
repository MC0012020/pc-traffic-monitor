package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var kernel = syscall.NewLazyDLL("kernel32.dll")
var createEvent = kernel.NewProc("CreateEventW")
var getOverlapped = kernel.NewProc("GetOverlappedResult")
var waitNamedPipe = kernel.NewProc("WaitNamedPipeW")

type namedPipe struct{ h syscall.Handle }

func openPipe(name string) (*namedPipe, error) {
	p, e := syscall.UTF16PtrFromString(name)
	if e != nil {
		return nil, e
	}
	h, e := syscall.CreateFile(p, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_OVERLAPPED|0x00100000|0x00010000, 0)
	if e == syscall.Errno(231) {
		waitNamedPipe.Call(uintptr(unsafe.Pointer(p)), 400)
		h, e = syscall.CreateFile(p, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_OVERLAPPED|0x00100000|0x00010000, 0)
	}
	if e != nil {
		return nil, e
	}
	return &namedPipe{h}, nil
}
func (p *namedPipe) Close() error { return syscall.CloseHandle(p.h) }
func (p *namedPipe) rw(b []byte, write bool) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	ev, _, e := createEvent.Call(0, 1, 0, 0)
	if ev == 0 {
		return 0, e
	}
	defer syscall.CloseHandle(syscall.Handle(ev))
	o := syscall.Overlapped{HEvent: syscall.Handle(ev)}
	var n uint32
	var err error
	if write {
		err = syscall.WriteFile(p.h, b, &n, &o)
	} else {
		err = syscall.ReadFile(p.h, b, &n, &o)
	}
	if err == syscall.ERROR_IO_PENDING {
		w, we := syscall.WaitForSingleObject(syscall.Handle(ev), 3000)
		if we != nil || w != 0 {
			_ = syscall.CancelIoEx(p.h, &o)
			_, _ = syscall.WaitForSingleObject(syscall.Handle(ev), syscall.INFINITE)
			runtime.KeepAlive(b)
			return 0, fmt.Errorf("pipe timeout")
		}
		r, _, ge := getOverlapped.Call(uintptr(p.h), uintptr(unsafe.Pointer(&o)), uintptr(unsafe.Pointer(&n)), 0)
		if r == 0 {
			err = ge
		} else {
			err = nil
		}
	}
	runtime.KeepAlive(b)
	if err == syscall.ERROR_BROKEN_PIPE || err == syscall.Errno(232) {
		return int(n), io.EOF
	}
	if err == nil && n == 0 && !write {
		return 0, io.EOF
	}
	return int(n), err
}
func (p *namedPipe) Read(b []byte) (int, error)  { return p.rw(b, false) }
func (p *namedPipe) Write(b []byte) (int, error) { return p.rw(b, true) }

type Target struct{ Address, Secret string }

func (t Target) label() string {
	if strings.HasPrefix(t.Address, `\\.\pipe\`) {
		return t.Address
	}
	return t.Address
}
func decodeSnapshot(r *http.Response) (Snapshot, error) {
	defer r.Body.Close()
	var s Snapshot
	if r.StatusCode != 200 {
		return s, fmt.Errorf("HTTP %d", r.StatusCode)
	}
	e := json.NewDecoder(io.LimitReader(r.Body, 32<<20)).Decode(&s)
	if e == nil && (s.DownloadTotal == nil || s.UploadTotal == nil) {
		e = fmt.Errorf("invalid /connections response")
	}
	return s, e
}

var client = &http.Client{Timeout: 1800 * time.Millisecond, Transport: &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 500 * time.Millisecond}).DialContext, DisableKeepAlives: true}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

type pipeBody struct {
	io.ReadCloser
	pipe *namedPipe
}

func (p *pipeBody) Close() error { e := p.pipe.Close(); _ = p.ReadCloser.Close(); return e }
func fetch(t Target) (Snapshot, error) {
	r, e := request(t, "/connections")
	if e != nil {
		return Snapshot{}, e
	}
	return decodeSnapshot(r)
}
func fetchTypes(t Target) (map[string]string, error) {
	r, e := request(t, "/proxies")
	if e != nil {
		return nil, e
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", r.StatusCode)
	}
	var data struct {
		Proxies map[string]struct {
			Type string `json:"type"`
		} `json:"proxies"`
	}
	e = json.NewDecoder(io.LimitReader(r.Body, 32<<20)).Decode(&data)
	if e != nil {
		return nil, e
	}
	m := map[string]string{}
	for k, v := range data.Proxies {
		m[k] = v.Type
	}
	return m, nil
}
func request(t Target, path string) (*http.Response, error) {
	pipe := strings.HasPrefix(t.Address, `\\.\pipe\`)
	base := t.Address
	if pipe {
		base = "http://localhost"
	} else {
		if !strings.Contains(base, "://") {
			base = "http://" + base
		}
		u, e := url.Parse(base)
		if e != nil || u.Scheme != "http" || u.User != nil || u.Path != "" && u.Path != "/" || u.RawQuery != "" || u.Fragment != "" {
			return nil, fmt.Errorf("invalid local API URL")
		}
		h := u.Hostname()
		if h != "localhost" && h != "127.0.0.1" && h != "::1" {
			return nil, fmt.Errorf("only localhost is allowed")
		}
		base = "http://" + u.Host
	}
	req, e := http.NewRequestWithContext(context.Background(), "GET", base+path, nil)
	if e != nil {
		return nil, e
	}
	if t.Secret != "" {
		req.Header.Set("Authorization", "Bearer "+t.Secret)
	}
	req.Close = true
	if pipe {
		p, e := openPipe(t.Address)
		if e != nil {
			return nil, e
		}

		if e = req.Write(p); e != nil {
			p.Close()
			return nil, e
		}
		r, e := http.ReadResponse(bufio.NewReader(p), req)
		if e != nil {
			p.Close()
			return nil, e
		}
		r.Body = &pipeBody{ReadCloser: r.Body, pipe: p}
		return r, nil
	}
	r, e := client.Do(req)
	if e != nil {
		return nil, e
	}
	return r, nil
}
func discover(secret string) []Target {
	var a []Target
	seen := map[Target]bool{}
	add := func(t Target) {
		if t.Address != "" && !seen[t] {
			seen[t] = true
			a = append(a, t)
		}
	}
	p := `\\.\pipe\verge-mihomo`
	add(Target{p, ""})
	add(Target{`\\.\pipe\mihomo`, ""})
	for _, root := range []string{os.Getenv("APPDATA"), os.Getenv("LOCALAPPDATA")} {
		for _, dir := range []string{"io.github.clash-verge-rev.clash-verge-rev", "io.github.clash-verge-rev.clash-verge", "io.github.clash-verge.clash-verge", "Clash Verge", "clash-verge"} {
			for _, file := range []string{"config.yaml", "clash-verge.yaml", "verge.yaml"} {
				b, e := os.ReadFile(filepath.Join(root, dir, file))
				if e != nil {
					continue
				}
				var address, key, pipe string
				for _, line := range strings.Split(string(b), "\n") {
					if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
						continue
					}
					parts := strings.SplitN(line, ":", 2)
					if len(parts) != 2 {
						continue
					}
					v := strings.TrimSpace(parts[1])
					if i := strings.Index(v, " #"); i >= 0 {
						v = v[:i]
					}
					v = strings.TrimSpace(v)
					if strings.HasPrefix(v, `"`) {
						if q, e := strconv.Unquote(v); e == nil {
							v = q
						}
					}
					v = strings.Trim(v, "\"'")
					switch strings.TrimSpace(parts[0]) {
					case "external-controller":
						address = v
					case "external-controller-pipe":
						pipe = v
					case "secret":
						key = v
					}
				}
				if secret != "" {
					key = secret
				}
				if strings.HasPrefix(pipe, `\\.\pipe\`) {
					add(Target{pipe, ""})
				}
				if strings.HasPrefix(address, "0.0.0.0:") {
					address = "127.0.0.1:" + strings.TrimPrefix(address, "0.0.0.0:")
				}
				if strings.HasPrefix(address, ":") {
					address = "127.0.0.1" + address
				}
				if address != "" {
					add(Target{address, key})
				}
				add(Target{"127.0.0.1:9097", key})
				add(Target{"127.0.0.1:9090", key})
			}
		}
	}
	add(Target{"127.0.0.1:9097", secret})
	add(Target{"127.0.0.1:9090", secret})
	return a
}

var getIfTable = syscall.NewLazyDLL("iphlpapi.dll").NewProc("GetIfTable")

type AdapterCounter struct{ In, Out uint32 }

func adapters() map[uint32]AdapterCounter {
	var size uint32
	getIfTable.Call(0, uintptr(unsafe.Pointer(&size)), 0)
	if size < 4 || size > 16<<20 {
		return nil
	}
	b := make([]byte, size)
	r, _, _ := getIfTable.Call(uintptr(unsafe.Pointer(&b[0])), uintptr(unsafe.Pointer(&size)), 0)
	if r != 0 {
		return nil
	}
	count := *(*uint32)(unsafe.Pointer(&b[0]))
	rowSize := int(unsafe.Sizeof(syscall.MibIfRow{}))
	out := map[uint32]AdapterCounter{}
	for i := 0; i < int(count); i++ {
		off := 4 + i*rowSize
		if off+rowSize > len(b) {
			break
		}
		row := (*syscall.MibIfRow)(unsafe.Pointer(&b[off]))
		if (row.Type == 6 || row.Type == 71) && row.OperStatus >= 4 {
			out[row.Index] = AdapterCounter{row.InOctets, row.OutOctets}
		}
	}
	return out
}
