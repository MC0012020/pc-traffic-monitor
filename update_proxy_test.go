package main

import "testing"

func TestMihomoUpdateProxyPorts(t *testing.T){
 p:=mihomoPorts{Mixed:7897,HTTP:7890,SOCKS:7891}.proxies()
 if len(p)!=3||p[0].String()!="http://127.0.0.1:7897"||p[1].String()!="http://127.0.0.1:7890"||p[2].String()!="socks5://127.0.0.1:7891"{t.Fatal(p)}
 if len((mihomoPorts{Mixed:0,HTTP:-1,SOCKS:65536}).proxies())!=0{t.Fatal("invalid ports")}
}
func TestWindowsHTTPSProxy(t *testing.T){
 for _,tc:=range []struct{ input,want string }{
 {"127.0.0.1:7897","http://127.0.0.1:7897"},
 {"http=127.0.0.1:8080;https=127.0.0.1:7897","http://127.0.0.1:7897"},
 {"HTTPS=[::1]:7897","http://[::1]:7897"},
 {"http=127.0.0.1:8080",""},{"",""},{"http://user:pass@localhost:80",""},{"localhost:99999",""},{"http://localhost:80/path",""},
 }{
  u:=windowsHTTPSProxy(tc.input);got:="";if u!=nil{got=u.String()};if got!=tc.want{t.Fatal(tc,got)}
 }
}
