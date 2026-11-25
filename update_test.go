package main
import ("testing";"fmt";"crypto/sha256")
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
