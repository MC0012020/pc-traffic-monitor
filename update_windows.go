package main

import (
 "debug/pe"
 "encoding/json"
 "fmt"
 "io"
 "net/http"
 "net"
 "net/url"
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
var updateCheckFailed bool // UI thread only

type downloadState struct { stage string; received, total int64 }
func setDownloadState(stage string, received, total int64) { updateProgress.Store(&downloadState{stage, received, total}) }
func requestUpdateCheck(){ select {case updateWake <- struct{}{}: default:} }

type progressReader struct { reader io.Reader; received, total int64; route string }
func (r *progressReader) Read(p []byte)(int,error){ n,e:=r.reader.Read(p); r.received+=int64(n); if n>0{setDownloadState(r.route,r.received,r.total)};return n,e }
var updateEvents=make(chan updateEvent,2)
type updateEvent struct { offer UpdateOffer; stage, hash string; err error; downloading bool }

type updateRoute struct { name, key string; client *http.Client; nativeTimeout time.Duration }
var lastWorkingUpdateRoute atomic.Pointer[string]
type routedUpdateClient struct { routes []updateRoute; preferred int }

func newUpdateHTTPClient(timeout time.Duration, proxy func(*http.Request)(*url.URL,error))*http.Client {
 tr:=http.DefaultTransport.(*http.Transport).Clone()
 tr.Proxy=proxy
 tr.DialContext=(&net.Dialer{Timeout:8*time.Second,KeepAlive:30*time.Second}).DialContext
 tr.TLSHandshakeTimeout=10*time.Second
 tr.ResponseHeaderTimeout=30*time.Second
 tr.DisableCompression=true
 return &http.Client{Transport:tr,Timeout:timeout,CheckRedirect:func(r *http.Request, via []*http.Request)error{
  if len(via)>=8||!allowedUpdateURL(r.URL){return fmt.Errorf("unexpected update redirect")}
  return nil
 }}
}
func updateClient(timeout time.Duration)*routedUpdateClient {
 c:=&routedUpdateClient{}
 seen:=map[string]bool{}
 add:=func(name string,u *url.URL){
  if u==nil||seen[u.String()]{return};seen[u.String()]=true
  c.routes=append(c.routes,updateRoute{name:name,key:u.String(),client:newUpdateHTTPClient(timeout,http.ProxyURL(u))})
 }
 c.routes=append(c.routes,updateRoute{name:"Windows 网络",key:"native",nativeTimeout:timeout})
 add("系统代理",staticWindowsUpdateProxy())
 c.routes=append(c.routes,updateRoute{name:"直连",key:"direct",client:newUpdateHTTPClient(timeout,nil)})
 for _,u:=range runningMihomoUpdateProxies(){add("Clash 代理",u)}
 req,_:=http.NewRequest("GET","https://api.github.com/",nil)
 if u,e:=http.ProxyFromEnvironment(req);e==nil{add("环境代理",u)}
 if previous:=lastWorkingUpdateRoute.Load();previous!=nil{for i,r:=range c.routes{if r.key==*previous{c.preferred=i;break}}}
 return c
}
func (c *routedUpdateClient) close(){for _,r:=range c.routes{if r.client!=nil{r.client.CloseIdleConnections()}}}
func getUpdateFile(c *routedUpdateClient,url string,maxBytes int64)([]byte,error){return readUpdateFile(c,url,maxBytes,0,false)}
func readUpdateFile(c *routedUpdateClient,url string,maxBytes,total int64,showProgress bool)([]byte,error){
 return readUpdateFileValidated(c,url,maxBytes,total,showProgress,nil)
}
func readUpdateFileValidated(c *routedUpdateClient,address string,maxBytes,total int64,showProgress bool,validate func([]byte)error)([]byte,error){
 var failures []string
 for step:=0;step<len(c.routes);step++{
  i:=(c.preferred+step)%len(c.routes);route:=c.routes[i]
  if showProgress{setDownloadState("正在连接 GitHub · "+route.name,0,0)}
  b,e:=readUpdateRoute(route,address,maxBytes,total)
  if e==nil&&validate!=nil{e=validate(b)}
  if e==nil{c.preferred=i;key:=route.key;lastWorkingUpdateRoute.Store(&key);return b,nil}
  if ue,ok:=e.(*url.Error);ok{e=ue.Err}
  failures=append(failures,route.name+": "+e.Error())
 }
 return nil,fmt.Errorf("%s",strings.Join(failures,"\n"))
}
func readUpdateRoute(route updateRoute, address string,maxBytes,total int64)([]byte,error){
 u,e:=url.Parse(address);if e!=nil||!allowedUpdateURL(u){return nil,fmt.Errorf("unexpected update URL")}
 if route.client==nil{return readNativeUpdate(route,address,maxBytes,total)}
 req,e:=http.NewRequest("GET",address,nil);if e!=nil{return nil,e}
 req.Header.Set("User-Agent","PcTrafficMonitor/"+appVersion())
 req.Header.Set("Cache-Control","no-cache")
 req.Header.Set("Accept-Encoding","identity")
 if req.URL.Hostname()=="api.github.com" {req.Header.Set("Accept","application/vnd.github+json")}
 res,e:=route.client.Do(req);if e!=nil{return nil,e};defer res.Body.Close()
 if res.StatusCode!=200{return nil,fmt.Errorf("HTTP %d",res.StatusCode)}
 var reader io.Reader=io.LimitReader(res.Body,maxBytes+1)
 if res.ContentLength>maxBytes{return nil,fmt.Errorf("file too large")}
 if total>0&&res.ContentLength>=0&&res.ContentLength!=total{return nil,fmt.Errorf("unexpected file size")}
 if total!=0 {shownTotal:=total;if total<0&&res.ContentLength>0{shownTotal=res.ContentLength};setDownloadState(route.name,0,shownTotal);reader=&progressReader{reader:reader,total:shownTotal,route:route.name}}
 b,e:=io.ReadAll(reader);if e==nil&&int64(len(b))>maxBytes{e=fmt.Errorf("file too large")}
 if e==nil&&total>0&&int64(len(b))!=total{e=fmt.Errorf("incomplete download")}
 if e==nil&&res.ContentLength>=0&&int64(len(b))!=res.ContentLength{e=fmt.Errorf("incomplete response")}
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
  c:=updateClient(15*time.Second)
  offer,source,e:=checkGitHubUpdate(c)
  c.close()
  writeUpdateDiagnostic(source,offer,e)
  select{case updateEvents<-updateEvent{offer:offer,err:e}:case <-done:return}
  // Schedule from the start of each check, not the end; no overlapping requests.
  timer.Reset(max(0,time.Minute-time.Since(lastCheck)))
 }
}
// Avoid polling the rate-limited REST API when a small raw VERSION file
// is enough. A new source revision is not downloadable until its release
// checksum has actually been published.
func checkGitHubUpdate(c *routedUpdateClient)(UpdateOffer,string,error){
 var candidate UpdateOffer
 _,rawErr:=readUpdateFileValidated(c,rawVersionURL+"?minute="+strconv.FormatInt(time.Now().Unix()/60,10),128,0,false,func(b []byte)error{
  var e error
  candidate,e=versionFileOffer(string(b),appVersion())
  return e
 })
 if rawErr==nil{
  if candidate.Tag==""{return candidate,"GitHub VERSION",nil}
  _,e:=readUpdateFileValidated(c,candidate.Sums.URL,64<<10,0,false,func(b []byte)error{_,e:=expectedHash(string(b),candidate.Exe.Name);return e})
  if e==nil{return candidate,"GitHub VERSION + Release SHA256",nil}
  rawErr=fmt.Errorf("release not ready or checksum unavailable: %w",e)
 }
 var offer UpdateOffer
 _,apiErr:=readUpdateFileValidated(c,"https://api.github.com/repos/MC0012020/pc-traffic-monitor/releases?per_page=30",2<<20,0,false,func(b []byte)error{
  var rs []ReleaseInfo
  if e:=json.Unmarshal(b,&rs);e!=nil{return e}
  if rs==nil{return fmt.Errorf("invalid releases response")}
  offer,_=pickUpdate(rs,appVersion())
  return nil
 })
 if apiErr==nil{return offer,"GitHub Releases API",nil}
 return UpdateOffer{},"GitHub",fmt.Errorf("VERSION: %v\nReleases API: %v",rawErr,apiErr)
}
func writeUpdateDiagnostic(source string,offer UpdateOffer,err error){
 dir:=filepath.Dir(dataPath())
 if os.MkdirAll(dir,0700)!=nil{return}
 status:="up to date";if offer.Tag!=""{status="available: "+offer.Tag}
 if err!=nil{status=err.Error()}
 body:=fmt.Sprintf("%s\ncurrent: %s\nsource: %s\n%s\n",time.Now().Format(time.RFC3339),appVersion(),source,status)
 _=os.WriteFile(filepath.Join(dir,"update-status.txt"),[]byte(body),0600)
}
func downloadUpdate(o UpdateOffer)(stage,hash string,err error){
 setDownloadState("正在选择下载线路…",0,0)
 c:=updateClient(15*time.Minute);defer c.close()
 var h string
 _,e:=readUpdateFileValidated(c,o.Sums.URL,64<<10,0,true,func(b []byte)error{var err error;h,err=expectedHash(string(b),o.Exe.Name);return err});if e!=nil{return "","",e}
 setDownloadState("正在连接下载服务器…",0,0)
 total:=o.Exe.Size;if total==0{total=-1}
 b,e:=readUpdateFileValidated(c,o.Exe.URL,64<<20,total,true,func(b []byte)error{
  setDownloadState("正在校验文件…",0,0)
  if (o.Exe.Size>0&&int64(len(b))!=o.Exe.Size)||!verifyHash(b,h){return fmt.Errorf("SHA256 or file size mismatch")};return nil
 });if e!=nil{return "","",e}
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
 if !updateBusy{if updateCheckFailed{return normal+" · 更新检测失败"};return normal}
 p:=updateProgress.Load();if p==nil{return "正在准备更新…"}
 if p.total>0{return fmt.Sprintf("下载更新 %d%% · %s / %s · %s",downloadPercent(p.received,p.total),human(float64(p.received)),human(float64(p.total)),p.stage)}
 if p.total<0{return fmt.Sprintf("下载更新 · %s · %s",human(float64(p.received)),p.stage)}
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
  if !e.downloading{
   updateCheckFailed=e.err!=nil
   if e.err==nil&&!updateBusy&&updateOffer.Tag!=e.offer.Tag{updateOffer=e.offer;updateButtonState()}
   if !updateBusy{text(status,updateStatusText(last.Status+"  |  "+last.ProxyStatus))}
   return
  }
  if e.err!=nil{writeUpdateDiagnostic("download",updateOffer,e.err);finishUpdateUI();changeDisplay(actionOpen);msg("更新失败，原程序保持不变：\n"+e.err.Error());return}
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
func finishUpdateUI(){updateBusy=false;updateProgress.Store(nil);updateLastDraw="";updateButtonState();text(status,updateStatusText(last.Status+"  |  "+last.ProxyStatus))}
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
  floatText(dc,label,int(r.L),int(r.T),int(r.R-r.L),int(r.B-r.T)-mainPx(3),smallFont,green)
  floatLine(dc,int(r.L)+mainPx(5),int(r.B)-mainPx(5),int(r.R-r.L)-mainPx(10),mainPx(2),lineBrush)
  if percent>0{floatLine(dc,int(r.L)+mainPx(5),int(r.B)-mainPx(5),(int(r.R-r.L)-mainPx(10))*percent/100,mainPx(2),greenBrush)}
  return
 }
 x,y:=(r.L+r.R)/2,(r.T+r.B)/2
 pen,_,_:=gdi.NewProc("CreatePen").Call(0,uintptr(mainPx(2)),green);old,_,_:=gdi.NewProc("SelectObject").Call(dc,pen)
 pts:=[]point{{x-mainOffset(6),y-mainOffset(2)},{x,y-mainOffset(8)},{x+mainOffset(6),y-mainOffset(2)}};gdi.NewProc("Polyline").Call(dc,uintptr(unsafe.Pointer(&pts[0])),3)
 gdi.NewProc("MoveToEx").Call(dc,uintptr(x),uintptr(y-mainOffset(8)),0);gdi.NewProc("LineTo").Call(dc,uintptr(x),uintptr(y+mainOffset(4)))
 pts=[]point{{x-mainOffset(7),y+mainOffset(3)},{x-mainOffset(7),y+mainOffset(8)},{x+mainOffset(7),y+mainOffset(8)},{x+mainOffset(7),y+mainOffset(3)}};gdi.NewProc("Polyline").Call(dc,uintptr(unsafe.Pointer(&pts[0])),4)
 gdi.NewProc("SelectObject").Call(dc,old);gdi.NewProc("DeleteObject").Call(pen)
}
