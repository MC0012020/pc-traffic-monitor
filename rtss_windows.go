package main

import (
 "strings"
 "time"
 "unsafe"
)

var rtssOwned bool
var rtssLastTry time.Time
// MEMORY_BASIC_INFORMATION layout for the supported Windows amd64 build.
type rtssMemoryInfo struct{
 base,allocationBase uintptr
 allocationProtect uint32
 _ uint32
 size uintptr
 state,protect,kind uint32
 _ uint32
}
func rtssMapping(text string,enabled bool)bool{
 h,_,_:=kernel.NewProc("OpenFileMappingW").Call(6,0,ptr("RTSSSharedMemoryV2"))
 if h==0{return false}
 defer kernel.NewProc("CloseHandle").Call(h)
 view,_,_:=kernel.NewProc("MapViewOfFile").Call(h,6,0,0,0)
 if view==0{return false}
 defer kernel.NewProc("UnmapViewOfFile").Call(view)
 var info rtssMemoryInfo
 n,_,_:=kernel.NewProc("VirtualQuery").Call(view,uintptr(unsafe.Pointer(&info)),unsafe.Sizeof(info))
 if n!=unsafe.Sizeof(info)||info.base!=view||info.state!=0x1000||info.protect!=4||info.size<40||info.size>512*1024*1024{return false}
 b:=unsafe.Slice((*byte)(unsafe.Pointer(view)),int(info.size))
 return rtssWrite(b,text,enabled)
}
func rtssValue(h uintptr)string{
 s:=cachedText[h]
 if s==""||strings.Contains(s,"—"){return "--"}
 return strings.TrimPrefix(s,"上传 ")
}
func syncRTSSOverlay(){
 enabled:=top&&(currentDisplay==displayFull||currentDisplay==displayFloat)
 if !enabled{
  if rtssOwned&&rtssMapping("",false){rtssOwned=false}
  return
 }
 // At most one publication per 250ms; no RTSS startup or profile changes.
 now:=time.Now()
 if !rtssLastTry.IsZero()&&now.Sub(rtssLastTry)<250*time.Millisecond{return}
 rtssLastTry=now
 pd,dd:=rtssValue(proxyDown),rtssValue(directDown)
 pt,dt:=rtssValue(proxyDay),rtssValue(directDay)
 var msg string
 if currentDisplay==displayFloat{
  msg="<C=C77A30>"+pd+"<C>  |  <C=138C72>"+dd+"<C>\n<C=C77A30>"+pt+"<C>  |  <C=138C72>"+dt+"<C>"
 }else{
  msg="<C=C77A30>Proxy: "+pd+" down / "+rtssValue(proxyUp)+" up\nToday: "+pt+"<C>\n<C=138C72>Local: "+dd+" down / "+rtssValue(directUp)+" up\nToday: "+dt+"<C>"
 }
 if rtssMapping(msg,true){rtssOwned=true}
}
func closeRTSSOverlay(){
 if !rtssOwned{return}
 // Bounded cleanup in case RTSS is drawing at shutdown.
 for i:=0;i<4;i++{if rtssMapping("",false){rtssOwned=false;return};time.Sleep(5*time.Millisecond)}
}
