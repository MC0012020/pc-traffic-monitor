package main

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// Windows x64 ETW ABI, system logger: real TCP/UDP send/recv events, IPv4 and IPv6.
var etw = syscall.NewLazyDLL("advapi32.dll")
var startTrace = etw.NewProc("StartTraceW")
var openTrace = etw.NewProc("OpenTraceW")
var processTrace = etw.NewProc("ProcessTrace")
var controlTrace = etw.NewProc("ControlTraceW")
var closeTrace = etw.NewProc("CloseTrace")
var getTCP = syscall.NewLazyDLL("iphlpapi.dll").NewProc("GetExtendedTcpTable")
var getUDP = syscall.NewLazyDLL("iphlpapi.dll").NewProc("GetExtendedUdpTable")
var bestInterface = syscall.NewLazyDLL("iphlpapi.dll").NewProc("GetBestInterfaceEx")
var queryImage = kernel.NewProc("QueryFullProcessImageNameW")
var isAdmin = syscall.NewLazyDLL("shell32.dll").NewProc("IsUserAnAdmin")

type FlowKey struct {
	PID           uint32
	Local, Remote string
	LP, RP        uint16
	UDP           bool
}
type FlowDatum struct {
	Key   FlowKey
	Name  string
	Bytes Bytes
}
type Capture struct {
	mu                       sync.Mutex
	rows                     map[FlowKey]FlowDatum
	names                    map[uint32]namedPID
	status                   string
	lost                     uint32
	overflow Bytes
	session, handle          uint64
	props                    []byte
	name                     *uint16
	logfile                  *traceLogFile
	callback, bufferCallback uintptr
	traceDone chan struct{}
}
type namedPID struct {
	Name string
	At   time.Time
}

func newCapture() *Capture {
	return &Capture{rows: map[FlowKey]FlowDatum{}, names: map[uint32]namedPID{}, status: "正在启动 Windows 采集"}
}
func (c *Capture) getName(pid uint32) string {
	if pid == 0 || pid == 4 {
		return "System"
	}
	if n, ok := c.names[pid]; ok && time.Since(n.At) < 10*time.Second {
		return n.Name
	}
	n := fmt.Sprintf("PID %d", pid)
	h, e := syscall.OpenProcess(0x1000, false, pid)
	if e == nil {
		b := make([]uint16, 1024)
		l := uint32(len(b))
		r, _, _ := queryImage.Call(uintptr(h), 0, uintptr(unsafe.Pointer(&b[0])), uintptr(unsafe.Pointer(&l)))
		syscall.CloseHandle(h)
		if r != 0 {
			p := syscall.UTF16ToString(b[:l])
			if i := strings.LastIndexAny(p, "\\/"); i >= 0 {
				p = p[i+1:]
			}
			n = p
		}
	}
	if len(c.names) > 20000 {
		c.names = map[uint32]namedPID{}
	}
	c.names[pid] = namedPID{n, time.Now()}
	return n
}

// Windows SDK TRACE_LOGFILE_HEADER (x64): 280 bytes. In particular the
// two pointer fields and the aligned LARGE_INTEGER fields must not use x86 offsets.
type traceLogHeader struct {
	BufferSize, Version, ProviderVersion, NumberOfProcessors uint32
	EndTime int64
	TimerResolution, MaximumFileSize, LogFileMode, BuffersWritten uint32
	StartBuffers, PointerSize, EventsLost, CpuSpeedInMHz uint32
	LoggerName, LogFileName uintptr
	TimeZone [172]byte
	_ [4]byte // alignment before the 64-bit timestamp fields
	BootTime, PerfFreq, StartTime int64
	ReservedFlags, BuffersLost uint32
}

// EVENT_TRACE_LOGFILEW (x64): CurrentEvent at 32, LogfileHeader at 120,
// BufferCallback at 400, EventRecordCallback at 424, total 448 bytes.
type traceLogFile struct {
	LogFileName, LoggerName        *uint16
	CurrentTime                    int64
	BuffersRead, Mode              uint32
	CurrentEvent                   [88]byte
	Header                         traceLogHeader
	BufferCallback                 uintptr
	BufferSize, Filled, EventsLost uint32
	_                              uint32
	EventCallback                  uintptr
	IsKernelTrace                  uint32
	_pad                           uint32
	Context                        uintptr
}

func (c *Capture) setStatus(s string) { c.mu.Lock(); c.status = s; c.mu.Unlock() }
func (c *Capture) snapshot() ([]FlowDatum, string, uint32, Bytes) {
	if c.session != 0 {
		p := make([]byte, len(c.props))
		copy(p, c.props)
		if r,_,_ := controlTrace.Call(uintptr(c.session),0,uintptr(unsafe.Pointer(&p[0])),0); r==0 {
			lost := u32(p,88) // EVENT_TRACE_PROPERTIES.EventsLost
			c.mu.Lock(); if lost>c.lost { c.lost=lost }; c.mu.Unlock()
		}
	}
	c.mu.Lock()
	rows, status, lost, overflow := c.rows, c.status, c.lost, c.overflow
	c.overflow = Bytes{}
	c.rows = map[FlowKey]FlowDatum{}
	c.mu.Unlock()
	a := make([]FlowDatum, 0, len(rows))
	for _, v := range rows {
		v.Name = c.getName(v.Key.PID)
		a = append(a, v)
	}
	return a, status, lost, overflow
}
func u32(b []byte, i int) uint32 { return binary.LittleEndian.Uint32(b[i : i+4]) }
func (c *Capture) event(rec uintptr) uintptr {
	if rec == 0 {
		return 0
	}
	h := unsafe.Slice((*byte)(unsafe.Pointer(rec)), 112)
	// EVENT_RECORD header ProviderId (GUID), event descriptor and payload pointer.
	provider := u32(h, 24)
	if provider != 0x9a280ac0 && provider != 0xbf3a50c5 {
		return 0
	}
	if h[42] == 0 {
		return 0
	} // Windows XP payload version not supported.
	opcode := h[45]
	if opcode != 10 && opcode != 11 && opcode != 26 && opcode != 27 {
		return 0
	}
	l := int(binary.LittleEndian.Uint16(h[86:88]))
	ptr := *(*uintptr)(unsafe.Pointer(rec + 96))
	if ptr == 0 {
		return 0
	}
	ipv6 := opcode == 26 || opcode == 27
	iplen := 4
	if ipv6 {
		iplen = 16
	}
	if l < 8+iplen*2+4 {
		return 0
	}
	b := unsafe.Slice((*byte)(unsafe.Pointer(ptr)), l)
	pid, size := u32(b, 0), u32(b, 4)
	if size == 0 || size > 1<<30 {
		return 0
	}
	dst, src := net.IP(b[8:8+iplen]).String(), net.IP(b[8+iplen:8+2*iplen]).String()
	dp, sp := binary.BigEndian.Uint16(b[8+2*iplen:]), binary.BigEndian.Uint16(b[10+2*iplen:])
	up := opcode == 10 || opcode == 26
	key := FlowKey{pid, src, dst, sp, dp, provider == 0xbf3a50c5}
	// TCP receive records already use the local socket in the source fields;
	// UDP receive records use the destination fields for the local socket.
	if !up && key.UDP {
		key.Local, key.Remote, key.LP, key.RP = dst, src, dp, sp
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	// Opening a trace session does not establish that events are being delivered.
	if c.status == "Windows 等待网络事件" {
		c.status = "Windows 实时采集中"
	}
	v, ok := c.rows[key]
	if !ok {
		if len(c.rows) >= 50000 {
			// Preserve captured bytes in a diagnostic bucket even when the flow limit is hit.
			if up { c.overflow.Up += uint64(size) } else { c.overflow.Down += uint64(size) }
			return 0
		}
		v = FlowDatum{Key: key}
	}
	if up {
		v.Bytes.Up += uint64(size)
	} else {
		v.Bytes.Down += uint64(size)
	}
	c.rows[key] = v
	return 0
}
func (c *Capture) start() {
	// Never attach to or stop another app's kernel trace session.
	n, _ := syscall.UTF16PtrFromString(fmt.Sprintf("PcTrafficV31-%d", os.Getpid()))
	c.name = n
	c.props = make([]byte, 120+1024)
	p := c.props
	put := func(i int, v uint32) { binary.LittleEndian.PutUint32(p[i:], v) }
	put(0, uint32(len(p)))
	put(40, 1)
	put(44, 0x00020000)
	put(48, 64)
	put(52, 32)
	put(56, 256)
	put(64, 0x02000100)
	put(68, 1)
	put(72, 0x00010000)
	put(116, 120)
	// Use a unique GUID for this private system logger session (Windows 8+).
	if _, e := rand.Read(p[24:40]); e != nil { c.setStatus("Windows ETW GUID 生成失败"); return }
	p[31] = p[31]&0x0f | 0x40
	p[32] = p[32]&0x3f | 0x80
	r, _, _ := startTrace.Call(uintptr(unsafe.Pointer(&c.session)), uintptr(unsafe.Pointer(n)), uintptr(unsafe.Pointer(&p[0])))
	if r != 0 {
		c.setStatus(fmt.Sprintf("Windows 采集失败 (%d)，请以管理员运行", r))
		return
	}
	c.callback = syscall.NewCallback(func(r uintptr) uintptr { return c.event(r) })
	c.bufferCallback = syscall.NewCallback(func(r uintptr) uintptr {
		if r != 0 {
			v := (*traceLogFile)(unsafe.Pointer(r)).EventsLost
			c.mu.Lock()
			if v > c.lost {
				c.lost = v
			}
			c.mu.Unlock()
		}
		return 1
	})
	c.logfile = &traceLogFile{LoggerName: n, Mode: 0x10000100, EventCallback: c.callback, BufferCallback: c.bufferCallback}
	h, _, e := openTrace.Call(uintptr(unsafe.Pointer(c.logfile)))
	if h == ^uintptr(0) {
		c.setStatus("Windows ETW 连接失败：" + e.Error())
		c.stop()
		return
	}
	c.handle = uint64(h)
	c.setStatus("Windows 等待网络事件")
	c.traceDone = make(chan struct{})
	traceHandle := c.handle
	go func() {
		defer close(c.traceDone)
		r, _, _ := processTrace.Call(uintptr(unsafe.Pointer(&traceHandle)), 1, 0, 0)
		c.setStatus(fmt.Sprintf("Windows 采集已停止 (%d)", r))
		runtime.KeepAlive(c.logfile)
	}()
}
func (c *Capture) stop() {
	if c.session != 0 {
		binary.LittleEndian.PutUint32(c.props[112:], 0)
		controlTrace.Call(uintptr(c.session), uintptr(unsafe.Pointer(c.name)), uintptr(unsafe.Pointer(&c.props[0])), 1)
		c.session = 0
	}
	if c.handle != 0 {
		closeTrace.Call(uintptr(c.handle))
		c.handle = 0
	}
	if c.traceDone != nil {
		select { case <-c.traceDone: case <-time.After(3*time.Second): }
	}
}
func coreName(s string) bool {
	s = strings.ToLower(strings.TrimSuffix(s, ".exe"))
	return s == "clash" || s == "mihomo" || s == "clash-meta" || s == "clash-win64" || s == "clash-windows-amd64" || strings.Contains(s, "mihomo") || strings.HasPrefix(s, "clash-meta")
}

type Owners struct {
	core        map[uint32]bool
	ports       map[uint16]bool
	owners      map[string]uint32
	names       map[uint32]string
	virtual     map[uint32]bool
	coreRunning bool
}

func getOwners(c *Capture) Owners {
	o := Owners{core: map[uint32]bool{}, ports: map[uint16]bool{}, owners: map[string]uint32{}, names: map[uint32]string{}, virtual: map[uint32]bool{}}
	// Toolhelp gives process names even if process image is inaccessible.
	snap, e := syscall.CreateToolhelp32Snapshot(2, 0)
	if e == nil {
		var pe syscall.ProcessEntry32
		pe.Size = uint32(unsafe.Sizeof(pe))
		for e = syscall.Process32First(snap, &pe); e == nil; e = syscall.Process32Next(snap, &pe) {
			n := syscall.UTF16ToString(pe.ExeFile[:])
			o.names[pe.ProcessID] = n
			if coreName(n) {
				o.core[pe.ProcessID] = true
				o.coreRunning = true
			}
		}
		syscall.CloseHandle(snap)
	}
	for _, fam := range []uintptr{2, 23} {
		for _, udp := range []bool{false, true} {
			proc := getTCP
			cls := uintptr(5)
			stride := 24
			ipoff, portoff, pidoff := 4, 8, 20
			listen := true
			if fam == 23 {
				stride = 56
				ipoff, portoff, pidoff = 0, 20, 52
			}
			if udp {
				proc = getUDP
				cls = 1
				stride = 12
				ipoff, portoff, pidoff = 0, 4, 8
				if fam == 23 {
					stride = 28
					portoff, pidoff = 20, 24
				}
			}
			var size uint32
			proc.Call(0, uintptr(unsafe.Pointer(&size)), 0, fam, cls, 0)
			if size < 4 || size > 32<<20 {
				continue
			}
			b := make([]byte, size)
			r, _, _ := proc.Call(uintptr(unsafe.Pointer(&b[0])), uintptr(unsafe.Pointer(&size)), 0, fam, cls, 0)
			if r != 0 {
				continue
			}
			cnt := int(u32(b, 0))
			if cnt > (len(b)-4)/stride {
				continue
			}
			for i := 0; i < cnt; i++ {
				row := b[4+i*stride : 4+(i+1)*stride]
				pid := u32(row, pidoff)
				port := binary.BigEndian.Uint16(row[portoff:])
				l := 4
				if fam == 23 {
					l = 16
				}
				ip := net.IP(row[ipoff : ipoff+l]).String()
				if !udp {
					state := 0
					if fam == 23 {
						state = 48
					}
					listen = u32(row, state) == 2
				}
				if o.core[pid] && listen && !udp {
					o.ports[port] = true
				}
				if !listen || udp {
					key := fmt.Sprintf("%s:%d", ip, port)
					if prev, ok := o.owners[key]; ok && prev != pid {
						o.owners[key] = 0
					} else {
						o.owners[key] = pid
					}
				}
			}
		}
	}
	ifs, _ := net.Interfaces()
	for _, i := range ifs {
		n := strings.ToLower(i.Name)
		if strings.Contains(n, "mihomo") || strings.Contains(n, "clash") {
			o.virtual[uint32(i.Index)] = true
		}
	}
	return o
}
func (o Owners) resolve(ip string, port uint16) string {
	p := o.owners[fmt.Sprintf("%s:%d", ip, port)]
	if p == 0 {
		p = o.owners[fmt.Sprintf("0.0.0.0:%d", port)]
	}
	if p == 0 {
		p = o.owners[fmt.Sprintf(":::%d", port)]
	}
	return o.names[p]
}
func (o Owners) viaVirtual(ip string) bool {
	if len(o.virtual) == 0 {
		return false
	}
	a := net.ParseIP(ip)
	if a == nil || a.IsLoopback() {
		return false
	}
	var addr [28]byte
	if v := a.To4(); v != nil {
		binary.LittleEndian.PutUint16(addr[:], 2)
		copy(addr[4:], v)
	} else {
		binary.LittleEndian.PutUint16(addr[:], 23)
		copy(addr[8:], a.To16())
	}
	var idx uint32
	r, _, _ := bestInterface.Call(uintptr(unsafe.Pointer(&addr[0])), uintptr(unsafe.Pointer(&idx)))
	return r == 0 && o.virtual[idx]
}
