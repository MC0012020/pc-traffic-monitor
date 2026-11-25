from PIL import Image, ImageDraw
from pathlib import Path
import struct,io
S=256*3
im=Image.new('RGBA',(S,S));d=ImageDraw.Draw(im)
d.rounded_rectangle((20,20,S-20,S-20),radius=160,fill='#183842')
d.line([(120,406),(223,406),(306,235),(407,526),(500,340),(640,340)],fill='#FFFFFF',width=34,joint='curve')
d.line((124,608,334,608),fill='#E89A42',width=31);d.line((434,608,644,608),fill='#2CA984',width=31)
base=im.resize((256,256),Image.Resampling.LANCZOS);base.save('app.ico',sizes=[(16,16),(32,32),(48,48),(64,64),(256,256)])
# Native icon resources: single COFF .rsrc section with RVA relocations.
ico=Path('app.ico').read_bytes();n=struct.unpack_from('<H',ico,4)[0];icons=[];group=bytearray(struct.pack('<HHH',0,1,n))
for i in range(n):
 w,h,c,r,p,b,size,off=struct.unpack_from('<BBBBHHII',ico,6+i*16)
 icons.append(ico[off:off+size]);group.extend(struct.pack('<BBBBHHIH',w,h,c,r,p,b,size,i+1))
res=bytearray();rel=[]
def reserve(k): o=len(res);res.extend(b'\0'*k);return o
def directory(ids):
 o=reserve(16+8*len(ids));struct.pack_into('<HH',res,o+12,0,len(ids))
 for i,id in enumerate(ids):struct.pack_into('<I',res,o+16+8*i,id)
 return o
def entry(dir,i,off,sub=True):struct.pack_into('<I',res,dir+20+8*i,off|(0x80000000 if sub else 0))
root=directory([3,14]);idir=directory(list(range(1,n+1)));gdir=directory([1]);entry(root,0,idir);entry(root,1,gdir)
for i,blob in enumerate(icons+[bytes(group)]):
 lang=directory([1033]);entry(idir if i<n else gdir,i if i<n else 0,lang);data=reserve(16);entry(lang,0,data,False)
 while len(res)%4:res.append(0)
 off=len(res);res.extend(blob);struct.pack_into('<IIII',res,data,off,len(blob),0,0);rel.append(data)
 while len(res)%4:res.append(0)
# one section symbol, relocation ADDR32NB; raw addend is offset relative to .rsrc.
raw=60;rp=raw+len(res);sp=rp+10*len(rel)
coff=struct.pack('<HHIIIHH',0x8664,1,0,sp,1,0,0)
sec=struct.pack('<8sIIIIIIHHI',b'.rsrc',0,0,len(res),raw,rp,0,len(rel),0,0x40300040)
rels=b''.join(struct.pack('<IIH',p,0,3) for p in rel)
sym=struct.pack('<8sIhHBB',b'.rsrc',0,1,0,3,0)
Path('icon_windows_amd64.syso').write_bytes(coff+sec+res+rels+sym+struct.pack('<I',4))
