# PcTrafficMonitor · 网络流量监控

![Windows](https://img.shields.io/badge/Windows-10%20%2F%2011%20x64-0078D4)
![Go](https://img.shields.io/badge/Go-1.23-00ADD8)
![Release](https://img.shields.io/github/v/release/MC0012020/pc-traffic-monitor?include_prereleases)

轻量级 Windows 网络流量监控工具。橙色显示 Clash 代理流量，绿色显示本地流量。无需安装运行库；Clash 不运行时也可监控本地流量。

## 下载与运行

[前往 Releases 下载](https://github.com/MC0012020/pc-traffic-monitor/releases)

下载 `PcTrafficMonitor_V3_Portable.zip`，解压后双击 `PcTrafficMonitor_V3.exe`，允许管理员权限（ETW 采集需要）。也可以直接下载 EXE。升级前从托盘退出旧版。旧 V3.13.x 版本不能自动降级至 V3，请手动下载 V3 并退出旧程序后替换 EXE（今日统计保留）。当前版本为未签名预发布，可能出现 Windows SmartScreen 提示，请先核对 SHA256。

## 功能与操作

- 实时上传、下载速度与今日累计（上传＋下载）；自动保存统计，跨日重置。
- 自动识别 Clash/Mihomo 官方接口；显示真实联网程序、目标地址及近期连接。列表按窗口高度自适应，列宽可拖动。
- 主窗口与悬浮窗均按所在显示器分辨率自适应，主窗口字体、控件、表格及窗口大小同步缩放，保留拖动列宽和窗口缩放；主窗口内部仅保留状态栏和操作图标，系统标题栏仍显示名称与版本；图钉统一控制主窗口与悬浮窗置顶，默认开启；创建时设置置顶样式，之后仅手动切换时调整层级。关闭 × 进入悬浮窗，最小化进入托盘后台。
- 悬浮窗按所在显示器分辨率自适应缩放（1920×1080 为基准，2560×1600 约 133%，最高 200%，跨屏拖动自动调整）；第一行显示两侧实时下载速度，第二行仅显示各自今日累计（上传＋下载）的数值和单位；左键拖动位置、单击恢复主窗口，鼠标悬停及右键均无动作。悬浮窗预留稳定文字宽度，流量刷新仅重绘、不改变窗口层级或激活窗口；跨屏时仍适配尺寸。不开启置顶时可被其他窗口覆盖；不检测或干预全屏程序，后台采集不停止。全屏应用优先：不检测全屏、不强制覆盖、不自动恢复置顶；普通窗口可正常置顶，全屏时允许被覆盖。
- 托盘单击打开主窗口；右键菜单仅有：打开窗口、悬浮显示、退出程序。
- 切换显示状态不停止采集；仅主动退出或系统结束会话时停止。若托盘不可用，先从悬浮窗恢复主窗口。

## 软件更新

启动后及每1分钟检查 GitHub VERSION 文件，并确认发布校验文件存在；必要时回退 GitHub Releases API（包含预发布，按检测开始时间计算间隔，网络请求未完成时不重叠）；恢复主窗口也触发检查（至少间隔1分钟）。检测失败时在现有状态栏提示，不增加按钮；恢复正常后自动清除。版本检测和下载自动尝试 Windows 原生网络、系统代理、直连、Clash/Mihomo 及环境变量代理，并复用最近成功线路，不强制代理；无需设置端口。下载状态显示连接方式，失败自动尝试下一连接方式，每条下载线路总超时15分钟。最近检测结果保存在本地 `%LOCALAPPDATA%\PcTrafficMonitorV31\update-status.txt`，用于排查失败原因。不修改 Clash 规则或 Windows 代理设置，实际出口仍由代理规则决定；Go 静态代理路径不解析 PAC/WPAD，Windows 原生网络接口使用系统配置。发现更新时主窗口显示独立更新图标；点击确认后显示连接、实际下载字节数及百分比、校验、安装状态，并下载 EXE，按 Release 的 SHA256SUMS.txt 核验文件大小与哈希，正常退出保存统计，再替换并重启。更新失败时保留原程序或尝试回滚。更新要求 EXE 所在目录可写、可访问 GitHub；程序未签名，SHA256 验证不是数字签名。仅向 GitHub 发起版本检查及下载请求，不上传流量记录。

## 统计口径和隐私

本地侧使用 Windows ETW TCP/UDP IPv4/IPv6 收发事件，包含局域网及回环；代理侧使用 Mihomo `/connections` 与 `/proxies` 的字节增量。通过已确认的代理核心进程、TCP 回环监听端口及完整 TCP/UDP 地址端口匹配去重，不再按通用虚拟网卡或单个源端口排除网络事件。Mihomo DIRECT 计入本地，不能可靠归类的字节保存在独立内部计数中，状态栏提示未归类及采集损失。短连接、TUN、自定义接口、其他 VPN、端口重用及 ETW 丢失事件可能影响分类；非 TCP/UDP 协议不在 ETW 范围内，回环两端事件可能重复计量。因此不保证与服务商账单或物理网卡字节完全一致。不会将未知出口强行计为代理。

仅统计程序运行期间。今日数据保存在 `%LOCALAPPDATA%\PcTrafficMonitorV31\daily.json`，每 5 秒保存；近期连接记录只在内存保留。没有遥测或远程上传功能；更新仅访问公开 GitHub 发布资源。本地 API 密钥只用于本机连接。

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

GitHub Actions 运行统计、布局、版本比较、ETW ABI、局域网去重及校验测试，并要求 Windows 构建机两轮真实 TCP/UDP IPv4/IPv6 回环收发测试全部通过才允许发布。回环测试不等同于用户物理网卡、真实远程桌面或 TUN 环境验证；因此仍标记为预发布。下载后可以用 PowerShell 的 `Get-FileHash -Algorithm SHA256` 与 Release 中的 `SHA256SUMS.txt` 比对。

## 授权

暂未指定开源许可证。公开可见不代表授权任意再分发或修改；相关许可由仓库所有者决定。
