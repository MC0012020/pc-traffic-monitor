package main

import (
 "runtime"
 "syscall"
 "testing"
 "unsafe"
)

// Check native Win32 styles and stable dimensions in a controlled window,
// independently of any installed game or GPU.
func TestFloatNativeTopAndStableGeometry(t *testing.T) {
 runtime.LockOSThread()
 defer runtime.UnlockOSThread()
 cls,_:=syscall.UTF16PtrFromString("STATIC")
 makeWindow:=func(ex uintptr)uintptr{
  h,_,_:=createWindow.Call(ex,uintptr(unsafe.Pointer(cls)),0,0x80000000,20,20,260,48,0,0,0,0)
  runtime.KeepAlive(cls)
  if h==0{t.Fatal("CreateWindowExW failed")}
  return h
 }
 mainWindow=makeWindow(windowTopStyle(0,defaultWindowTop))
 defer func(){user.NewProc("DestroyWindow").Call(mainWindow);mainWindow=0}()
 floatWindow=makeWindow(windowTopStyle(0x08000080,defaultWindowTop))
 defer func(){user.NewProc("DestroyWindow").Call(floatWindow);floatWindow=0;floatPlaced=false;top=defaultWindowTop;floatScale=100;floatWidth=0;floatFont=0;floatDayFont=0}()
 floatFont=makeFontQuality(17,600,4)
 floatDayFont=makeFontQuality(12,500,4)
 defer func(){gdi.NewProc("DeleteObject").Call(floatFont);gdi.NewProc("DeleteObject").Call(floatDayFont)}()
 if floatFont==0||floatDayFont==0{t.Fatal("CreateFontW failed")}
 floatAnchor=point{20,20}
 syncFloatScale(floatMonitor())
 measureFloat()
 style:=func(h uintptr)uintptr{v,_,_:=user.NewProc("GetWindowLongPtrW").Call(h,^uintptr(19));return v}
 if style(mainWindow)&8==0||style(floatWindow)&8==0||style(floatWindow)&0x08000000==0{t.Fatal("unexpected default styles")}
 for _,on:=range []bool{false,true}{
  if !applyWindowTop(mainWindow,on)||!applyWindowTop(floatWindow,on){t.Fatal("topmost transition failed")}
  top=on
  if (style(mainWindow)&8!=0)!=on||(style(floatWindow)&8!=0)!=on{t.Fatal("shared topmost styles mismatch")}
 }
 floatPlace()
 before:=floatWidth
 var bounds rect32
 if ok,_,_:=user.NewProc("GetWindowRect").Call(floatWindow,uintptr(unsafe.Pointer(&bounds)));ok==0{t.Fatal("GetWindowRect failed")}
 oldCache:=cachedText
 defer func(){cachedText=oldCache}()
 cachedText=map[uintptr]string{0:"1023.99 MB/s"}
 refreshFloat()
 cachedText[0]="1 B/s"
 refreshFloat()
 var after rect32
 user.NewProc("GetWindowRect").Call(floatWindow,uintptr(unsafe.Pointer(&after)))
 if before!=floatWidth||bounds!=after||style(floatWindow)&8==0{t.Fatal("refresh changed bounds or layer",bounds,after)}
}
