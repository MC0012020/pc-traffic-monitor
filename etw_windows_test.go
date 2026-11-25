package main

import (
 "bytes"
 "io"
 "net"
 "os"
 "strings"
 "testing"
 "time"
 "unsafe"
)

func TestETWNativeLayout(t *testing.T) {
 var h traceLogHeader
 var f traceLogFile
 if unsafe.Sizeof(h)!=280 || unsafe.Offsetof(h.BootTime)!=248 || unsafe.Offsetof(f.Header)!=120 || unsafe.Offsetof(f.BufferCallback)!=400 || unsafe.Offsetof(f.EventCallback)!=424 || unsafe.Offsetof(f.Context)!=440 || unsafe.Sizeof(f)!=448 {
  t.Fatalf("Windows x64 ETW ABI: header=%d boot=%d headerOffset=%d buffer=%d event=%d context=%d total=%d",unsafe.Sizeof(h),unsafe.Offsetof(h.BootTime),unsafe.Offsetof(f.Header),unsafe.Offsetof(f.BufferCallback),unsafe.Offsetof(f.EventCallback),unsafe.Offsetof(f.Context),unsafe.Sizeof(f))
 }
}

func TestDirectLANAndProxyDedup(t *testing.T) {
 now:=time.Now()
 remote:=FlowKey{PID:123,Local:"192.168.1.20",Remote:"192.168.1.30",LP:50000,RP:47999,UDP:true}
 v:=FlowDatum{Key:remote,Name:"remote.exe",Bytes:Bytes{40000,1000}}
 o:=Owners{core:map[uint32]bool{999:true},ports:map[uint16]bool{47999:true},coreRunning:true}
 // A remote UDP endpoint must not disappear simply because its port
 // matches a core port, or the machine also has a VPN.
 if got:=coreLeg(v,o,nil,true);got!=Direct{t.Fatal("LAN UDP incorrectly excluded",got)}
 wrong:=remote;wrong.PID=0;wrong.Remote="192.168.1.31"
 if got:=coreLeg(v,o,map[FlowKey]time.Time{wrong:now},true);got!=Direct{t.Fatal("source-only match",got)}
 correct:=remote;correct.PID=0
 if got:=coreLeg(v,o,map[FlowKey]time.Time{correct:now},true);got!=-1{t.Fatal("exact match not deduplicated",got)}
 if got:=coreLeg(v,o,map[FlowKey]time.Time{correct:now},false);got!=Direct{t.Fatal("stale API excluded direct traffic",got)}
 v.Key.PID=999
 if got:=coreLeg(v,o,nil,false);got!=Unknown{t.Fatal("unavailable API silently discarded core traffic",got)}
 if got:=coreLeg(v,o,nil,true);got!=-1{t.Fatal("core double counted",got)}
}

type trafficProbe struct { name string; localPort, remotePort uint16; udp bool; want Bytes }

func probeTCP(t *testing.T,network,ip string) trafficProbe {
 t.Helper()
 l,e:=net.Listen(network,net.JoinHostPort(ip,"0"));if e!=nil{t.Fatal(e)};defer l.Close()
 payload:=bytes.Repeat([]byte{0x5a},64*1024)
 server:=make(chan error,1)
 go func(){
  c,e:=l.Accept();if e!=nil{server<-e;return};defer c.Close();_ = c.SetDeadline(time.Now().Add(8*time.Second))
  b:=make([]byte,len(payload));if _,e=io.ReadFull(c,b);e==nil{_,e=io.Copy(c,bytes.NewReader(b))};server<-e
 }()
 c,e:=net.DialTimeout(network,l.Addr().String(),5*time.Second);if e!=nil{t.Fatal(e)};defer c.Close()
 _=c.SetDeadline(time.Now().Add(8*time.Second))
 if _,e=io.Copy(c,bytes.NewReader(payload));e!=nil{t.Fatal(e)}
 b:=make([]byte,len(payload));if _,e=io.ReadFull(c,b);e!=nil||!bytes.Equal(b,payload){t.Fatal("TCP payload",e)}
 if e=<-server;e!=nil{t.Fatal(e)}
 return trafficProbe{network+"/"+ip,uint16(c.LocalAddr().(*net.TCPAddr).Port),uint16(l.Addr().(*net.TCPAddr).Port),false,Bytes{uint64(len(payload)),uint64(len(payload))}}
}

func probeUDP(t *testing.T,network,ip string) trafficProbe {
 t.Helper()
 server,e:=net.ListenUDP(network,&net.UDPAddr{IP:net.ParseIP(ip)});if e!=nil{t.Fatal(e)};defer server.Close()
 c,e:=net.DialUDP(network,nil,server.LocalAddr().(*net.UDPAddr));if e!=nil{t.Fatal(e)};defer c.Close()
 deadline:=time.Now().Add(10*time.Second);_ = c.SetDeadline(deadline);_ = server.SetDeadline(deadline)
 payload:=bytes.Repeat([]byte{0x6b},1024);b:=make([]byte,2048)
 for i:=0;i<64;i++{
  if _,e=c.Write(payload);e!=nil{t.Fatal(e)}
  n,addr,e:=server.ReadFromUDP(b);if e!=nil||n!=len(payload){t.Fatal("UDP receive",n,e)}
  if _,e=server.WriteToUDP(b[:n],addr);e!=nil{t.Fatal(e)}
  n,e=c.Read(b);if e!=nil||n!=len(payload)||!bytes.Equal(b[:n],payload){t.Fatal("UDP echo",n,e)}
 }
 return trafficProbe{network+"/"+ip,uint16(c.LocalAddr().(*net.UDPAddr).Port),uint16(server.LocalAddr().(*net.UDPAddr).Port),true,Bytes{65536,65536}}
}

// Real Windows network test, explicitly enabled by the release workflow.
// No generated/fake ETW events: exercise actual Winsock traffic and require
// measured send AND receive bytes for the test process and its socket pair.
func TestETWLiveTraffic(t *testing.T){
 if os.Getenv("PCMONITOR_LIVE_ETW")!="1"{t.Skip("enable PCMONITOR_LIVE_ETW=1 for elevated Windows integration test")}
 admin,_,_:=isAdmin.Call();if admin==0{t.Fatal("live capture requires administrator rights")}
 c:=newCapture();c.start();defer c.stop()
 _,status,_,_:=c.snapshot()
 if c.handle==0||strings.Contains(status,"失败"){t.Fatal(status)}
 time.Sleep(time.Second)
 probes:=[]trafficProbe{probeTCP(t,"tcp4","127.0.0.1"),probeUDP(t,"udp4","127.0.0.1"),probeTCP(t,"tcp6","::1"),probeUDP(t,"udp6","::1")}
 totals:=make([]Bytes,len(probes));deadline:=time.Now().Add(12*time.Second)
 for {
  rows,status,lost,overflow:=c.snapshot()
  if lost!=0||overflow.sum()!=0{t.Fatalf("ETW loss during probe: %d %v",lost,overflow)}
  for _,r:=range rows{
   if r.Key.PID!=uint32(os.Getpid()){continue}
   for i,p:=range probes{if r.Key.LP==p.localPort&&r.Key.RP==p.remotePort&&r.Key.UDP==p.udp{totals[i]=totals[i].add(r.Bytes)}}
  }
  ok:=true;for i,p:=range probes{if totals[i].Down<p.want.Down||totals[i].Up<p.want.Up{ok=false}}
  if ok{for i,p:=range probes{t.Logf("%s: down=%d up=%d (payload >= %d each)",p.name,totals[i].Down,totals[i].Up,p.want.Down)};return}
  if time.Now().After(deadline){t.Fatalf("real capture incomplete: status=%s probes=%+v measured=%+v",status,probes,totals)}
  time.Sleep(500*time.Millisecond)
 }
}
