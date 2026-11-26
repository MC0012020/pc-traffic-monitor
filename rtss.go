package main

import (
 "bytes"
 "encoding/binary"
 "sync/atomic"
 "unsafe"
)

// RTSS SDK shared-memory v2.14+. The ABI advertises its array offset,
// entry size and count. Keep the renderer lock and reserved slot intact.
// Reference: RTSS SDK RTSSSharedMemory.h and UpdateOSD sample.
const rtssOwner = "PcTrafficMonitor"
const rtssSignature = 0x52545353
const rtssMinVersion = 0x0002000e
func rtssWord(b []byte,i int)uint32{return binary.LittleEndian.Uint32(b[i:i+4])}
func rtssLayout(b []byte)(offset,stride,count int,ok bool){
 if len(b)<40||rtssWord(b,0)!=rtssSignature{return}
 v:=rtssWord(b,4)
 if v<rtssMinVersion||v>>16!=2{return}
 stride,offset,count=int(rtssWord(b,20)),int(rtssWord(b,24)),int(rtssWord(b,28))
 if stride<4608||offset<40||offset%4!=0||count<2||count>4096||offset>len(b)||stride>(len(b)-offset)/count{return}
 ok=true
 return
}
func rtssName(b []byte)string{
 if i:=bytes.IndexByte(b,0);i>=0{return string(b[:i])}
 return ""
}
// Returns true when the requested state is applied, including no owned slot
// on release. A busy renderer is never waited on during an ordinary refresh.
func rtssWrite(b []byte,text string,enabled bool)bool{
 if _,_,_,ok:=rtssLayout(b);!ok{return false}
 if uintptr(unsafe.Pointer(&b[36]))%4!=0{return false}
 lock:=(*uint32)(unsafe.Pointer(&b[36]))
 if !atomic.CompareAndSwapUint32(lock,0,1){return false}
 defer atomic.AndUint32(lock,^uint32(1))
 offset,stride,count,ok:=rtssLayout(b);if !ok{return false}
 owned,free:=-1,-1
 for i:=1;i<count;i++{
  j:=offset+i*stride
  owner:=b[j+256:j+512]
  if rtssName(owner)==rtssOwner{owned=j;break}
  if free<0&&owner[0]==0{free=j}
 }
 if !enabled{
  if owned>=0{clear(b[owned:owned+4608]);atomic.AddUint32((*uint32)(unsafe.Pointer(&b[32])),1)}
  return true
 }
 if owned<0{owned=free}
 if owned<0||len(text)>=4096{return false}
 entry:=b[owned:owned+4608]
 if rtssName(entry[256:512])==rtssOwner&&rtssName(entry[512:4608])==text{return true}
 clear(entry[:256]);clear(entry[512:4608]);copy(entry[512:4608],text)
 clear(entry[256:512]);copy(entry[256:512],rtssOwner)
 atomic.AddUint32((*uint32)(unsafe.Pointer(&b[32])),1)
 return true
}
