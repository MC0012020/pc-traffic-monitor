package main
import ("testing";"fmt";"crypto/sha256";"net/url")
func TestVersionOrder(t *testing.T){
 for _,p:=range [][2]string{{"v3.13.0","3.12.0"},{"v3.12.10","3.12.9"},{"v4.0.0","3.99.99"}}{if !newer(p[0],p[1]){t.Fatal(p)}}
 for _,p:=range [][2]string{{"v3.12.0","3.12.0"},{"v3.9.0","3.12.0"},{"v3.13.0-rc1","3.12.0"},{"v03.13.0","3.12.0"},{"v3.x.0","3.12.0"}}{if newer(p[0],p[1]){t.Fatal(p)}}
}
func TestPickUpdate(t *testing.T){
 makeRelease:=func(tag, name string)ReleaseInfo{return ReleaseInfo{Tag:tag,Prerelease:true,Assets:[]ReleaseAsset{{name,releaseBase+tag+"/"+name,1000},{"SHA256SUMS.txt",releaseBase+tag+"/SHA256SUMS.txt",100}}}}
 a:=makeRelease("v3.13.0","PcTrafficMonitor_V3.13.exe");b:=makeRelease("v3.14.0","PcTrafficMonitor_V3.14.exe");b.Draft=true;c:=makeRelease("v3.15.0","PcTrafficMonitor_V3.15.exe");c.Assets[0].URL="https://example.com/evil.exe"
 r,ok:=pickUpdate([]ReleaseInfo{b,c,a},"3.12.0");if !ok||r.Tag!=a.Tag{t.Fatal(r,ok)}
 if _,ok:=pickUpdate([]ReleaseInfo{a},"3.13.0");ok{t.Fatal("same version")}
}
func TestUpdateHash(t *testing.T){
 b:=[]byte("test");h:=fmt.Sprintf("%x",sha256.Sum256(b));s:=h+"  app.exe\r\n"
 got,e:=expectedHash(s,"app.exe");if e!=nil||got!=h||!verifyHash(b,got)||verifyHash([]byte("bad"),got){t.Fatal(got,e)}
 for _,bad:=range []string{"bad  app.exe",s+s,h+"  other.exe"}{if _,e:=expectedHash(bad,"app.exe");e==nil{t.Fatal(bad)}}
}

func TestDownloadPercent(t *testing.T){for _,tc:=range []struct{received,total int64;want int}{{0,100,0},{25,100,25},{99,100,99},{100,100,100},{101,100,100},{10,0,0},{-1,100,0}}{if got:=downloadPercent(tc.received,tc.total);got!=tc.want{t.Fatal(tc,got)}}}


func TestVersionFileOffer(t *testing.T){
 o,e:=versionFileOffer("3.13.7\r\n","3.13.4")
 if e!=nil||o.Tag!="v3.13.7"||o.Exe.URL!=releaseBase+"v3.13.7/PcTrafficMonitor_V3.13.7.exe"||o.Sums.URL!=releaseBase+"v3.13.7/SHA256SUMS.txt"||o.Exe.Size!=0{t.Fatal(o,e)}
 o,e=versionFileOffer("4.0.0\n","3.13.4");if e!=nil||o.Exe.Name!="PcTrafficMonitor_V4.0.exe"{t.Fatal(o,e)}
 for _,v:=range []string{"3.13.4","3.12.0"}{o,e=versionFileOffer(v,"3.13.4");if e!=nil||o.Tag!=""{t.Fatal(o,e)}}
 for _,v:=range []string{"<html>","v3.13.7","3.13.7/evil","03.13.7",""}{if _,e=versionFileOffer(v,"3.13.4");e==nil{t.Fatal(v)}}
}
func TestAllowedUpdateURL(t *testing.T){
 for _,s:=range []string{rawVersionURL,"https://github.com/a","https://release-assets.githubusercontent.com/a?q=1","https://api.github.com:443/a"}{u,e:=url.Parse(s);if e!=nil||!allowedUpdateURL(u){t.Fatal(s)}}
 for _,s:=range []string{"http://github.com/a","https://github.com.evil.test/a","https://user@github.com/a","https://github.com:444/a","https://evil.test/a"}{u,_:=url.Parse(s);if allowedUpdateURL(u){t.Fatal(s)}}
}
func TestActualReleaseJSON(t *testing.T){
 // GitHub lists prereleases and may return entries in non-version order.
 rs:=[]ReleaseInfo{
 {Tag:"v3.12.0",Prerelease:true,Assets:[]ReleaseAsset{{"PcTrafficMonitor_V3.12.exe",releaseBase+"v3.12.0/PcTrafficMonitor_V3.12.exe",5806592},{"SHA256SUMS.txt",releaseBase+"v3.12.0/SHA256SUMS.txt",447}}},
 {Tag:"v3.13.6",Prerelease:true,Assets:[]ReleaseAsset{{"PcTrafficMonitor_V3.13.6.exe",releaseBase+"v3.13.6/PcTrafficMonitor_V3.13.6.exe",6065152},{"SHA256SUMS.txt",releaseBase+"v3.13.6/SHA256SUMS.txt",451}}},
 }
 o,ok:=pickUpdate(rs,"3.13.4");if !ok||o.Tag!="v3.13.6"{t.Fatal(o,ok)}
}
