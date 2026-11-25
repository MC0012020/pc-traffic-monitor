package main

import (
 "net"
 "net/url"
 "strconv"
 "strings"
)

type mihomoPorts struct {
 Mixed int `json:"mixed-port"`
 HTTP int `json:"port"`
 SOCKS int `json:"socks-port"`
}

func (p mihomoPorts) proxies() []*url.URL {
 var out []*url.URL
 for _, entry := range []struct{ port int; scheme string }{{p.Mixed,"http"},{p.HTTP,"http"},{p.SOCKS,"socks5"}} {
  if entry.port>0 && entry.port<=65535 {
   out=append(out,&url.URL{Scheme:entry.scheme,Host:net.JoinHostPort("127.0.0.1",strconv.Itoa(entry.port))})
  }
 }
 return out
}

// Windows ProxyServer is either host:port or a semicolon-separated
// protocol mapping. The https mapping selects the proxy for HTTPS targets;
// it does not imply TLS between the client and the proxy.
func windowsHTTPSProxy(value string) *url.URL {
 value=strings.TrimSpace(value)
 if strings.Contains(value,"=") {
  var found string
  for _, part:=range strings.Split(value,";") {
   key,v,ok:=strings.Cut(strings.TrimSpace(part),"=")
   if ok && strings.EqualFold(strings.TrimSpace(key),"https"){found=strings.TrimSpace(v);break}
  }
  value=found
 }
 if value=="" {return nil}
 if !strings.Contains(value,"://"){value="http://"+value}
 u,e:=url.Parse(value)
 if e!=nil||u.User!=nil||u.Hostname()==""||u.Path!=""||u.RawQuery!=""||u.Fragment!=""{return nil}
 if u.Scheme!="http"&&u.Scheme!="https"&&u.Scheme!="socks5"&&u.Scheme!="socks5h"{return nil}
 port,e:=strconv.Atoi(u.Port());if e!=nil||port<1||port>65535{return nil}
 return u
}
