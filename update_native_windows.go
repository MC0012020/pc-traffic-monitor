package main

import (
 "bytes"
 "fmt"
 "net/url"
 "runtime"
 "strings"
 "syscall"
 "time"
 "unsafe"
)

// WinINet is the Windows desktop HTTP stack. PRECONFIG uses the current
// user's Internet settings. This is a fallback, not a browser automation.
var updateWinInet = syscall.NewLazyDLL("wininet.dll")
var updateInternetOpen = updateWinInet.NewProc("InternetOpenW")
var updateInternetOpenURL = updateWinInet.NewProc("InternetOpenUrlW")
var updateInternetClose = updateWinInet.NewProc("InternetCloseHandle")
var updateInternetSetOption = updateWinInet.NewProc("InternetSetOptionW")
var updateInternetRead = updateWinInet.NewProc("InternetReadFile")
var updateHTTPQuery = updateWinInet.NewProc("HttpQueryInfoW")

func nativeQueryNumber(h uintptr, option uintptr)(uint32,bool){
 var value uint32
 size:=uint32(4)
 r,_,_:=updateHTTPQuery.Call(h,option|0x20000000,uintptr(unsafe.Pointer(&value)),uintptr(unsafe.Pointer(&size)),0)
 return value,r!=0
}
func nativeQueryLocation(h uintptr)(string,error){
 b:=make([]uint16,32768)
 size:=uint32(len(b)*2)
 r,_,e:=updateHTTPQuery.Call(h,33,uintptr(unsafe.Pointer(&b[0])),uintptr(unsafe.Pointer(&size)),0)
 if r==0{return "",fmt.Errorf("redirect location: %w",e)}
 return syscall.UTF16ToString(b),nil
}
func readNativeUpdate(route updateRoute,address string,maxBytes,total int64)([]byte,error){
 agent,_:=syscall.UTF16PtrFromString("PcTrafficMonitor/"+appVersion())
 session,_,e:=updateInternetOpen.Call(uintptr(unsafe.Pointer(agent)),0,0,0,0)
 runtime.KeepAlive(agent)
 if session==0{return nil,fmt.Errorf("WinINet session: %w",e)}
 defer updateInternetClose.Call(session)
 // Bound each blocking operation. Certificate and hostname checks are not disabled.
 for _,opt:=range []uintptr{2,5,6}{
  limit:=int64(15000);if opt==6&&total!=0{limit=60000}
  ms:=uint32(min(route.nativeTimeout.Milliseconds(),limit))
  r,_,e:=updateInternetSetOption.Call(session,opt,uintptr(unsafe.Pointer(&ms)),4)
  if r==0{return nil,fmt.Errorf("WinINet timeout option: %w",e)}
 }
 deadline:=time.Now().Add(route.nativeTimeout)
 for redirects:=0;redirects<8;redirects++{
  if time.Now().After(deadline){return nil,fmt.Errorf("WinINet request timed out")}
  u,err:=url.Parse(address)
  if err!=nil||!allowedUpdateURL(u){return nil,fmt.Errorf("unexpected update URL")}
  header:="Cache-Control: no-cache\r\nAccept-Encoding: identity\r\n"
  if strings.EqualFold(u.Hostname(),"api.github.com"){header+="Accept: application/vnd.github+json\r\n"}
  headers,_:=syscall.UTF16FromString(header)
  addressW,_:=syscall.UTF16PtrFromString(address)
  // RELOAD | NO_CACHE_WRITE | NO_AUTO_REDIRECT | NO_COOKIES | NO_UI |
  // SECURE | PRAGMA_NOCACHE. Redirect destinations are validated below.
  h,_,e:=updateInternetOpenURL.Call(session,uintptr(unsafe.Pointer(addressW)),uintptr(unsafe.Pointer(&headers[0])),uintptr(len(headers)-1),0x84a80300,0)
  runtime.KeepAlive(addressW);runtime.KeepAlive(headers)
  if h==0{return nil,fmt.Errorf("WinINet open: %w",e)}
  status,ok:=nativeQueryNumber(h,19)
  if !ok{updateInternetClose.Call(h);return nil,fmt.Errorf("WinINet response status unavailable")}
  if status==301||status==302||status==303||status==307||status==308{
   location,err:=nativeQueryLocation(h);updateInternetClose.Call(h)
   if err!=nil{return nil,err}
   next,err:=u.Parse(location)
   if err!=nil||!allowedUpdateURL(next){return nil,fmt.Errorf("unexpected update redirect")}
   address=next.String();continue
  }
  if status!=200{updateInternetClose.Call(h);return nil,fmt.Errorf("HTTP %d",status)}
  b,err:=readNativeUpdateBody(h,route.name,maxBytes,total,deadline)
  updateInternetClose.Call(h)
  return b,err
 }
 return nil,fmt.Errorf("too many update redirects")
}
func readNativeUpdateBody(h uintptr,name string,maxBytes,total int64,deadline time.Time)([]byte,error){
 length,known:=nativeQueryNumber(h,5)
 if known&&int64(length)>maxBytes{return nil,fmt.Errorf("file too large")}
 if total>0&&known&&int64(length)!=total{return nil,fmt.Errorf("unexpected file size")}
 progress:=total!=0
 shownTotal:=total
 if total<0&&known&&length>0{shownTotal=int64(length)}
 if progress{setDownloadState(name,0,shownTotal)}
 var b bytes.Buffer
 buf:=make([]byte,32768)
 for {
  select{case <-done:return nil,fmt.Errorf("update canceled");default:}
  if time.Now().After(deadline){return nil,fmt.Errorf("WinINet download timed out")}
  var n uint32
  r,_,e:=updateInternetRead.Call(h,uintptr(unsafe.Pointer(&buf[0])),uintptr(len(buf)),uintptr(unsafe.Pointer(&n)))
  if r==0{return nil,fmt.Errorf("WinINet read: %w",e)}
  if n==0{break}
  if int64(b.Len())+int64(n)>maxBytes{return nil,fmt.Errorf("file too large")}
  b.Write(buf[:int(n)])
  if progress{setDownloadState(name,int64(b.Len()),shownTotal)}
 }
 if total>0&&int64(b.Len())!=total{return nil,fmt.Errorf("incomplete download")}
 if known&&int64(b.Len())!=int64(length){return nil,fmt.Errorf("incomplete response")}
 return b.Bytes(),nil
}
