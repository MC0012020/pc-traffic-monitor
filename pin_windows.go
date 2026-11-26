package main

import (
 "syscall"
 "time"
 "unsafe"
)

// The hooks are out-of-context and run on our existing message-loop thread.
// No game injection, no foreground activation, no periodic Z-order polling.
const pinForegroundEvent = 0x0003
const pinReorderEvent = 0x8004
var pinHooks [2]uintptr
var pinCallback uintptr
var pinPending bool
var pinPendingAt time.Time
var pinRecovery pinRecoveryState

func pinWinEvent(hook,event,hwnd,object,child,thread,stamp uintptr)uintptr{
 if !top||currentDisplay==displayBackground||currentDisplay==displayExit||hwnd==0{return 0}
 // REORDER can be delivered for a desktop/window/client parent. Ignore
 // individual accessible children; coalesce the event before inspecting HWNDs.
 if child!=0{return 0}
 if event==pinForegroundEvent||event==pinReorderEvent{
  if !pinPending{pinPending=true;pinPendingAt=time.Now()}
 }
 return 0
}
func startPinEvents(){
 if pinCallback==0{pinCallback=syscall.NewCallback(pinWinEvent)}
 for i,event:=range []uintptr{pinForegroundEvent,pinReorderEvent}{
  pinHooks[i],_,_=user.NewProc("SetWinEventHook").Call(event,event,0,pinCallback,0,0,2) // OUTOFCONTEXT | SKIPOWNPROCESS
 }
}
func stopPinEvents(){
 for i,h:=range pinHooks{if h!=0{user.NewProc("UnhookWinEvent").Call(h);pinHooks[i]=0}}
 pinPending=false
}
func resetPinRecovery(){
 // User actions may start a new display session, but keep the global
 // cooldown so rapid toggles cannot cause repeated automatic raises.
 pinRecovery.attempted=nil
 pinPending=false
}
func pinRect(h uintptr)(rect32,bool){
 var r rect32
 ok,_,_:=user.NewProc("GetWindowRect").Call(h,uintptr(unsafe.Pointer(&r)))
 return r,ok!=0&&r.R>r.L&&r.B>r.T
}
func pinWindowVisible(h uintptr)bool{
 if h==0{return false}
 v,_,_:=user.NewProc("IsWindowVisible").Call(h)
 m,_,_:=user.NewProc("IsIconic").Call(h)
 return v!=0&&m==0
}
func pinForegroundCandidate(fg uintptr)bool{
 if fg==0||fg==mainWindow||fg==floatWindow||!pinWindowVisible(fg){return false}
 // Do not cover our own modal dialogs, shell surfaces or foreground menus.
 root,_,_:=user.NewProc("GetAncestor").Call(fg,3) // GA_ROOTOWNER
 if root==mainWindow||root==floatWindow{return false}
 desk,_,_:=user.NewProc("GetDesktopWindow").Call()
 shellH,_,_:=user.NewProc("GetShellWindow").Call()
 if fg==desk||fg==shellH{return false}
 var cls [128]uint16
 user.NewProc("GetClassNameW").Call(fg,uintptr(unsafe.Pointer(&cls[0])),uintptr(len(cls)))
 switch syscall.UTF16ToString(cls[:]){
 case "Progman","WorkerW","Shell_TrayWnd","Shell_SecondaryTrayWnd","#32768","#32770","Windows.UI.Core.CoreWindow":return false
 }
 return true
}
func foregroundCoversPinned(target,fg uintptr)bool{
 if !pinWindowVisible(target)||!pinForegroundCandidate(fg){return false}
 a,ok:=pinRect(target);if !ok{return false}
 b,ok:=pinRect(fg);if !ok||!pinRectsOverlap(int(a.L),int(a.T),int(a.R),int(a.B),int(b.L),int(b.T),int(b.R),int(b.B)){return false}
 // Only recover if the foreground HWND is actually above us in ordinary
 // Win32 Z-order. Do not infer invisible GPU/independent-flip surfaces.
 h:=target
 for i:=0;i<256;i++{
  prev,_,_:=user.NewProc("GetWindow").Call(h,3) // GW_HWNDPREV
  if prev==0||prev==h{return false}
  if prev==fg{return true}
  h=prev
 }
 return false
}
func processPinEvent(){
 if !pinPending{return}
 // Coalesce bursts on the existing UI timer; no event means no HWND work.
 now:=time.Now()
 if now.Sub(pinPendingAt)<150*time.Millisecond{return}
 pinPending=false
 if !top||exitRequested{return}
 var target uintptr
 switch currentDisplay{
 case displayFull:target=mainWindow
 case displayFloat:if floatDragging{return};target=floatWindow
 default:return
 }
 fg,_,_:=user.NewProc("GetForegroundWindow").Call()
 if !foregroundCoversPinned(target,fg){return}
 if !pinRecovery.allow(target,fg,now.UnixMilli()){return}
 // Recheck foreground just before the one-shot non-activating raise.
 check,_,_:=user.NewProc("GetForegroundWindow").Call()
 if check!=fg{return}
 applyWindowTop(target,true)
 // Never retry the same foreground HWND in this display session, even
 // if the raise fails or the application immediately takes back its layer.
}
