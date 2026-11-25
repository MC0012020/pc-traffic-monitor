package main

import (
 "debug/pe"
 "encoding/json"
 "fmt"
 "io"
 "net/http"
 "os"
 "os/exec"
 "path/filepath"
 "strconv"
 "strings"
 "sync/atomic"
 "runtime"
 "syscall"
 "time"
 "unsafe"
)

const idUpdate=106
var updateButton uintptr
var updateOffer UpdateOffer
var updateBusy bool
var updateProgress atomic.Pointer[downloadState]
var updateWake = make(chan struct{}, 1)
var updateLastDraw string

type downloadState struct { stage string; received, total int64 }
func setDownloadState(stage string, received, total int64) { updateProgress.Store(&downloadState{stage, received, total}) }
func requestUpdateCheck(){ select {case updateWake <- struct{}{}: default:} }

type progressReader struct { reader io.Reader; received, total int64 }
func (r *progressReader) Read(p []byte)(int,error){ n,e:=r.reader.Read(p); r.received+=int64(n); if n>0{setDownloadState("正在下载",r.received,r.total)};return n,e }
var updateEvents=make(chan updateEvent,2)
type updateEvent struct { offer UpdateOffer; stage, hash string; err error; downloading bool }

func updateClient(timeout time.Duration)*http.Client{
 return &http.Client{Timeout:timeout,CheckRedirect:func(r *http.Request, via []*http.Request)error{
  h:=r.URL.Hostname()
  if len(via)>=8||r.URL.Scheme!="https"||(h!="github.com"&&h!="api.github.com"&&h!="release-assets.githubusercontent.com"&&h!="objects.githubusercontent.com"){return fmt.Errorf("unexpected update redirect")}
  return nil
 }}
}
func getUpdateFile(c *http.Client,url string,maxBytes int64)([]byte,error){ return readUpdateFile(c,url,maxBytes,0) }
func readUpdateFile(c *http.Client,url string,maxBytes,total int64)([]byte,error){
 req,e:=http.NewRequest("GET",url,nil);if e!=nil{return nil,e}
 req.Header.Set("User-Agent","PcTrafficMonitor/"+appVersion())
 res,e:=c.Do(req);if e!=nil{return nil,e};defer res.Body.Close()
 if res.StatusCode!=200{return nil,fmt.Errorf("HTTP %d",res.StatusCode)}
 var reader io.Reader=io.LimitReader(res.Body,maxBytes+1)
 if total>0 { setDownloadState("正在下载",0,total);reader=&progressReader{reader:reader,total:total} }
 b,e:=io.ReadAll(reader);if e==nil&&int64(len(b))>maxBytes{e=fmt.Errorf("file too large")}
 return b,e
}
func updateCheckLoop(){
 timer:=time.NewTimer(2*time.Second);defer timer.Stop()
 var lastCheck time.Time
 for{select{case <-done:return;case <-timer.C:case <-updateWake:
  if !lastCheck.IsZero()&&time.Since(lastCheck)<time.Minute {continue}
  if !timer.Stop(){select{case <-timer.C:default:}}
 }
 lastCheck=time.Now()
  b,e:=getUpdateFile(updateClient(15*time.Second),"https://api.github.com/repos/MC0012020/pc-traffic-monitor/releases?per_page=30",2<<20)
  var r []ReleaseInfo
  if e==nil{e=json.Unmarshal(b,&r)}
  delay:=5*time.Minute
  if e==nil{if offer,ok:=pickUpdate(r,appVersion());ok{select{case updateEvents<-updateEvent{offer:offer}:case <-done:return}}}else{delay=time.Minute}
  timer.Reset(delay)
 }
}
func downloadUpdate(o UpdateOffer)(stage,hash string,err error){
 c:=updateClient(180*time.Second)
 setDownloadState("正在连接 GitHub…",0,0)
 sums,e:=getUpdateFile(c,o.Sums.URL,64<<10);if e!=nil{return "","",e}
 h,e:=expectedHash(string(sums),o.Exe.Name);if e!=nil{return "","",e}
 setDownloadState("正在连接下载服务器…",0,0)
 b,e:=readUpdateFile(c,o.Exe.URL,64<<20,o.Exe.Size);if e!=nil{return "","",e}
 setDownloadState("正在校验文件…",0,0)
 if int64(len(b))!=o.Exe.Size||!verifyHash(b,h){return "","",fmt.Errorf("SHA256 or file size mismatch")}
 exe,e:=os.Executable();if e!=nil{return "","",e}
 dir,e:=os.MkdirTemp(filepath.Dir(exe),".pcmonitor-update-");if e!=nil{return "","",e}
 success:=false;defer func(){if !success{_ = os.RemoveAll(dir)}}()
 pending:=filepath.Join(dir,"new.exe")
 if e=os.WriteFile(pending,b,0700);e!=nil{return "","",e}
 p,e:=pe.Open(pending);if e!=nil{return "","",e}
 machine:=p.Machine;p.Close();if machine!=pe.IMAGE_FILE_MACHINE_AMD64{return "","",fmt.Errorf("not a Windows x64 executable")}
 setDownloadState("正在准备更新…",0,0)
 original,e:=os.ReadFile(exe);if e!=nil{return "","",e}
 if e=os.WriteFile(filepath.Join(dir,"helper.exe"),original,0700);e!=nil{return "","",e}
 success=true;return dir,h,nil
}
func startUpdate(){
 if updateBusy||updateOffer.Tag==""{return}
 o:=updateOffer
 p,_:=syscall.UTF16PtrFromString("发现 "+o.Tag+"，是否下载并更新？\n更新将短暂退出并重新启动软件，保留今日统计。\n此版本未签名，SHA256 仅验证下载完整性。")
 title,_:=syscall.UTF16PtrFromString("软件更新")
 answer,_,_:=user.NewProc("MessageBoxW").Call(mainWindow,uintptr(unsafe.Pointer(p)),uintptr(unsafe.Pointer(title)),0x10024)
 runtime.KeepAlive(p);runtime.KeepAlive(title)
 if answer!=6{return}
 updateBusy=true
 updateLastDraw=""
 setDownloadState("正在连接 GitHub…",0,0)
 updateButtonState()
 refreshUpdateProgress()
 go func(){dir,hash,e:=downloadUpdate(o);select{case updateEvents<-updateEvent{stage:dir,hash:hash,err:e,downloading:true}:case <-done:if dir!=""{_ = os.RemoveAll(dir)}}}()
}
func updateButtonState(){
 if updateButton==0{return}
 visible(updateButton,updateOffer.Tag!="")
 tip:="更新 "+updateOffer.Tag
 if updateBusy{tip="正在下载更新…"}
 updateButtonTip(updateButton,tip)
 user.NewProc("EnableWindow").Call(updateButton,map[bool]uintptr{true:0,false:1}[updateBusy])
 user.NewProc("InvalidateRect").Call(updateButton,0,1)
}
func updateStatusText(normal string) string {
 if !updateBusy{return normal}
 p:=updateProgress.Load();if p==nil{return "正在准备更新…"}
 if p.total>0{return fmt.Sprintf("正在下载更新 %d%%  ·  %s / %s",downloadPercent(p.received,p.total),human(float64(p.received)),human(float64(p.total)))}
 return p.stage
}
func refreshUpdateProgress(){
 if !updateBusy{return}
 s:=updateStatusText("");if s==updateLastDraw{return};updateLastDraw=s
 text(status,s);updateButtonTip(updateButton,s)
 user.NewProc("InvalidateRect").Call(updateButton,0,0)
}
func pollUpdateUI(){
 refreshUpdateProgress()
 select{
 case e:=<-updateEvents:
  if !e.downloading{if updateOffer.Tag==""||newer(e.offer.Tag,updateOffer.Tag){updateOffer=e.offer;updateButtonState()};return}
  if e.err!=nil{finishUpdateUI();changeDisplay(actionOpen);msg("更新失败，原程序保持不变：\n"+e.err.Error());return}
  setDownloadState("正在安装并重启…",0,0);refreshUpdateProgress()
  user.NewProc("UpdateWindow").Call(mainWindow)
  exe,err:=os.Executable();if err!=nil{_ = os.RemoveAll(e.stage);finishUpdateUI();changeDisplay(actionOpen);msg(err.Error());return}
  cmd:=exec.Command(filepath.Join(e.stage,"helper.exe"),"--apply-update",exe,strconv.Itoa(os.Getpid()),e.hash)
  cmd.Dir=filepath.Dir(exe)
  if err=cmd.Start();err!=nil{_ = os.RemoveAll(e.stage);finishUpdateUI();changeDisplay(actionOpen);msg("启动更新助手失败："+err.Error());return}
  _=cmd.Process.Release()
  changeDisplay(actionExit)
 default:
 }
}
func finishUpdateUI(){updateBusy=false;updateProgress.Store(nil);updateLastDraw="";updateButtonState();text(status,last.Status+"  |  "+last.ProxyStatus)}
// The helper is a copy of the current executable. It waits until the old
// instance has closed and saved history, verifies the payload again, and swaps
// the executable with a recoverable backup in the same volume.
func applyUpdateHelper(args []string){
 if len(args)!=5{return}
 self,e:=os.Executable();if e!=nil{return}
 dir:=filepath.Dir(self);target,e:=filepath.Abs(args[2]);if e!=nil||!strings.EqualFold(filepath.Dir(target),filepath.Dir(dir))||!strings.HasPrefix(filepath.Base(dir),".pcmonitor-update-")||!strings.EqualFold(filepath.Base(self),"helper.exe")||!strings.HasSuffix(strings.ToLower(target),".exe"){return}
 pid,e:=strconv.Atoi(args[3]);if e!=nil||pid<=0{return}
 handle,_,_:=kernel.NewProc("OpenProcess").Call(0x00100000,0,uintptr(pid))
 if handle!=0{r,_,_:=kernel.NewProc("WaitForSingleObject").Call(handle,30000);syscall.CloseHandle(syscall.Handle(handle));if r!=0{msg("更新等待原程序退出超时，未替换程序。");return}}
 pending,backup:=filepath.Join(dir,"new.exe"),filepath.Join(dir,"previous.exe")
 restart:=func(){cmd:=exec.Command(target);cmd.Dir=filepath.Dir(target);if cmd.Start()==nil{_ = cmd.Process.Release()}}
 b,e:=os.ReadFile(pending);if e!=nil||!verifyHash(b,args[4]){msg("更新校验失败，未替换程序。");restart();return}
 // Give the Windows image loader and antivirus a brief opportunity to release the original file.
 for i:=0;i<20;i++{e=os.Rename(target,backup);if e==nil{break};time.Sleep(250*time.Millisecond)}
 if e!=nil{msg("无法替换原程序："+e.Error());restart();return}
 if e=os.Rename(pending,target);e!=nil{if restore:=os.Rename(backup,target);restore==nil{restart()}else{msg("恢复失败，请保留备份："+backup)};msg("更新替换失败："+e.Error());return}
 cmd:=exec.Command(target,"--cleanup-update",dir);cmd.Dir=filepath.Dir(target)
 if e=cmd.Start();e!=nil{_ = os.Remove(target);if restore:=os.Rename(backup,target);restore==nil{restart()}else{msg("恢复失败，请保留备份："+backup)};msg("新版启动失败："+e.Error());return}
 _=cmd.Process.Release()
}
func cleanupUpdate(dir string){
 exe,e:=os.Executable();if e!=nil{return}
 dir,e=filepath.Abs(dir);if e!=nil||!strings.EqualFold(filepath.Dir(dir),filepath.Dir(exe))||!strings.HasPrefix(filepath.Base(dir),".pcmonitor-update-"){return}
 // Only delete known update files, and only after the helper has stopped.
 for i:=0;i<20;i++{if e=os.Remove(filepath.Join(dir,"helper.exe"));e==nil||os.IsNotExist(e){_ = os.Remove(filepath.Join(dir,"previous.exe"));_ = os.Remove(filepath.Join(dir,"new.exe"));_ = os.Remove(dir);return};time.Sleep(500*time.Millisecond)}
}
func drawUpdateIcon(dc uintptr,r rect32){
 if updateBusy {
  p:=updateProgress.Load(); label:="…";percent:=0
  if p!=nil&&p.total>0{percent=downloadPercent(p.received,p.total);label=fmt.Sprintf("%d%%",percent)}
  floatText(dc,label,int(r.L),int(r.T),int(r.R-r.L),int(r.B-r.T)-3,smallFont,green)
  floatLine(dc,int(r.L)+5,int(r.B)-5,int(r.R-r.L)-10,2,lineBrush)
  if percent>0{floatLine(dc,int(r.L)+5,int(r.B)-5,(int(r.R-r.L)-10)*percent/100,2,greenBrush)}
  return
 }
 x,y:=(r.L+r.R)/2,(r.T+r.B)/2
 pen,_,_:=gdi.NewProc("CreatePen").Call(0,2,green);old,_,_:=gdi.NewProc("SelectObject").Call(dc,pen)
 pts:=[]point{{x-6,y-2},{x,y-8},{x+6,y-2}};gdi.NewProc("Polyline").Call(dc,uintptr(unsafe.Pointer(&pts[0])),3)
 gdi.NewProc("MoveToEx").Call(dc,uintptr(x),uintptr(y-8),0);gdi.NewProc("LineTo").Call(dc,uintptr(x),uintptr(y+4))
 pts=[]point{{x-7,y+3},{x-7,y+8},{x+7,y+8},{x+7,y+3}};gdi.NewProc("Polyline").Call(dc,uintptr(unsafe.Pointer(&pts[0])),4)
 gdi.NewProc("SelectObject").Call(dc,old);gdi.NewProc("DeleteObject").Call(pen)
}
