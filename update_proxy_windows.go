package main

import (
 "encoding/json"
 "io"
 "net/url"
 "syscall"
 "unsafe"
 "runtime"
)

func runningMihomoUpdateProxies() []*url.URL {
 for _,target:=range discover("") {
  select{case <-done:return nil;default:}
  r,e:=request(target,"/configs");if e!=nil{continue}
  var p mihomoPorts
  if r.StatusCode==200 {e=json.NewDecoder(io.LimitReader(r.Body,1<<20)).Decode(&p)} else {e=io.ErrUnexpectedEOF}
  r.Body.Close()
  if e==nil {if a:=p.proxies();len(a)>0{return a}}
 }
 return nil
}

// Read the current user's explicitly enabled static Windows proxy without
// changing the registry. PAC/WPAD are not interpreted here.
func staticWindowsUpdateProxy() *url.URL {
 path,_:=syscall.UTF16PtrFromString("Software\\Microsoft\\Windows\\CurrentVersion\\Internet Settings")
 var h syscall.Handle
 r,_,_:=adv.NewProc("RegOpenKeyExW").Call(uintptr(syscall.HKEY_CURRENT_USER),uintptr(unsafe.Pointer(path)),0,1,uintptr(unsafe.Pointer(&h)))
 runtime.KeepAlive(path)
 if r!=0{return nil}
 defer adv.NewProc("RegCloseKey").Call(uintptr(h))
 query:=adv.NewProc("RegQueryValueExW")
 enabledName,_:=syscall.UTF16PtrFromString("ProxyEnable")
 var enabled,typ uint32
 size:=uint32(4)
 r,_,_=query.Call(uintptr(h),uintptr(unsafe.Pointer(enabledName)),0,uintptr(unsafe.Pointer(&typ)),uintptr(unsafe.Pointer(&enabled)),uintptr(unsafe.Pointer(&size)))
 runtime.KeepAlive(enabledName)
 if r!=0||typ!=4||enabled==0{return nil}
 name,_:=syscall.UTF16PtrFromString("ProxyServer")
 size=0
 r,_,_=query.Call(uintptr(h),uintptr(unsafe.Pointer(name)),0,uintptr(unsafe.Pointer(&typ)),0,uintptr(unsafe.Pointer(&size)))
 if r!=0||typ!=1||size<2||size>65536{return nil}
 b:=make([]uint16,(size+1)/2)
 r,_,_=query.Call(uintptr(h),uintptr(unsafe.Pointer(name)),0,uintptr(unsafe.Pointer(&typ)),uintptr(unsafe.Pointer(&b[0])),uintptr(unsafe.Pointer(&size)))
 runtime.KeepAlive(name)
 if r!=0||typ!=1{return nil}
 return windowsHTTPSProxy(syscall.UTF16ToString(b))
}
