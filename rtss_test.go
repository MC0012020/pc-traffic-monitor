package main

import (
 "bytes"
 "encoding/binary"
 "testing"
)
func testRTSSMemory()[]byte{
 b:=make([]byte,64+4*4608)
 for k,v:=range map[int]uint32{0:rtssSignature,4:rtssMinVersion,20:4608,24:64,28:4}{binary.LittleEndian.PutUint32(b[k:],v)}
 return b
}
func TestRTSSOwnSlot(t *testing.T){
 b:=testRTSSMemory()
 copy(b[64+256:],"Primary")
 copy(b[64+4608+256:],"Other")
 saved:=append([]byte(nil),b[64:64+2*4608]...)
 if !rtssWrite(b,"hello",true){t.Fatal("write failed")}
 j:=64+2*4608
 if rtssName(b[j+256:j+512])!=rtssOwner||rtssName(b[j+512:j+4608])!="hello"||rtssWord(b,32)!=1||rtssWord(b,36)!=0{t.Fatal("bad slot or lock")}
 if !rtssWrite(b,"hello",true)||rtssWord(b,32)!=1{t.Fatal("unchanged text republished")}
 binary.LittleEndian.PutUint32(b[36:],1)
 if rtssWrite(b,"blocked",true)||rtssWord(b,36)!=1{t.Fatal("busy renderer modified")}
 binary.LittleEndian.PutUint32(b[36:],0)
 if !rtssWrite(b,"new",true)||rtssWord(b,32)!=2{t.Fatal("update failed")}
 if !rtssWrite(b,"",false)||rtssWord(b,32)!=3||b[j+256]!=0||b[j+512]!=0{t.Fatal("release failed")}
 if !bytes.Equal(saved,b[64:64+2*4608]){t.Fatal("foreign slots modified")}
}
func TestRTSSInvalidMapping(t *testing.T){
 for _,tc:=range []struct{k int;v uint32}{{0,0},{4,0x2000d},{20,1},{24,0},{24,65},{28,0xffffffff},{28,1000}}{
  b:=testRTSSMemory();binary.LittleEndian.PutUint32(b[tc.k:],tc.v);before:=append([]byte(nil),b...)
  if rtssWrite(b,"test",true)||!bytes.Equal(b,before){t.Fatal("invalid mapping accepted",tc)}
 }
 if rtssWrite(make([]byte,8),"x",true){t.Fatal("short mapping accepted")}
}
