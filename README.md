# PcTrafficMonitor · 网络流量监控

![Windows](https://img.shields.io/badge/Windows-10%20%2F%2011%20x64-0078D4)
![Go](https://img.shields.io/badge/Go-1.23-00ADD8)
![Release](https://img.shields.io/github/v/release/MC0012020/pc-traffic-monitor?include_prereleases)

轻量级 Windows 网络流量监控工具。橙色显示 Clash 代理流量，绿色显示本地流量。无需安装运行库；Clash 不运行时也可监控本地流量。

## 下载与运行

[前往 Releases 下载](https://github.com/MC0012020/pc-traffic-monitor/releases)

下载 `PcTrafficMonitor_V3.12_Portable.zip`，解压后双击 `PcTrafficMonitor_V3.12.exe`，允许管理员权限（ETW 采集需要）。也可以直接下载 EXE。升级前从托盘退出旧版。当前版本为未签名预发布，可能出现 Windows SmartScreen 提示，请先核对 SHA256。

## 功能与操作

- 实时上传、下载速度与今日累计；自动保存统计，跨日重置。
- 自动识别 Clash/Mihomo 官方接口；显示真实联网程序、目标地址及近期连接。列表按窗口高度自适应，列宽可拖动。
- 主窗口默认置顶；图钉切换置顶。关闭 × 进入悬浮窗，最小化进入托盘后台。
- 悬浮窗仅显示两侧实时下载速度；左键拖动位置、单击恢复主窗口，鼠标悬停及右键均无动作。
- 托盘单击打开主窗口；右键菜单仅有：打开窗口、悬浮显示、退出程序。
- 切换显示状态不停止采集；仅主动退出或系统结束会话时停止。若托盘不可用，先从悬浮窗恢复主窗口。

## 统计口径和隐私

本地侧使用 Windows ETW TCP/UDP IPv4/IPv6 收发事件，包含局域网及回环；代理侧使用 Mihomo `/connections` 与 `/proxies` 的字节增量。通过进程、监听端口、源地址端口、Fake-IP、虚拟接口路由排除已识别的重复代理传输，Mihomo DIRECT 计入本地。短连接、TUN、自定义接口、其他 VPN、端口重用及 ETW 丢失事件可能影响分类；非 TCP/UDP 协议不在 ETW 范围内，回环两端事件可能重复计量。因此不保证与服务商账单或物理网卡字节完全一致。不会将未知出口强行计为代理。

仅统计程序运行期间。今日数据保存在 `%LOCALAPPDATA%\PcTrafficMonitorV31\daily.json`，每 5 秒保存；近期连接记录只在内存保留。没有遥测或远程上传功能。本地 API 密钥只用于本机连接。

## 自行构建

需要 Go 1.23.2、Python 3.12 和 Pillow。图标源程序随仓库提供，编译前生成图标资源。

```powershell
python -m pip install Pillow==12.3.0
python make_icon.py
go test -v ./...
$env:GOOS = 'windows'
$env:GOARCH = 'amd64'
$env:CGO_ENABLED = '0'
go build -trimpath -ldflags='-s -w -H windowsgui' -o PcTrafficMonitor.exe .
```

GitHub Actions 自动运行测试及 Windows 构建，将 EXE、便携 ZIP、SHA256 校验文件发布至 Releases。生成的图标资源也包含于 CI 构建归档中。

## 验证状态

V3.12.0 在 Linux 上通过 12 项 Go 单元测试，并成功交叉编译为 Windows x64 GUI EXE。尚未在真实 Windows 桌面验证界面、ETW 采集及托盘操作；因此标记为预发布。下载后可以用 PowerShell 的 `Get-FileHash -Algorithm SHA256` 与 Release 中的 `SHA256SUMS.txt` 比对。

## 授权

暂未指定开源许可证。公开可见不代表授权任意再分发或修改；相关许可由仓库所有者决定。
