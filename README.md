# YTB Music

用 Go 和 [MyGo](https://github.com/egoist/mygo) 写的 YouTube Music 桌面客户端：界面由 MyGo 原生绘制，听歌时不启动 WebView。

基于 [mygo-cloudmusic](https://github.com/egoist/mygo-cloudmusic) 的界面与播放器改写，数据来自 YouTube Music。

## 功能

- 首页精选、快速精选、排行榜
- 搜索单曲、视频、歌手、专辑与歌单，输入时显示搜索建议，保留搜索历史
- 歌单、专辑与歌手页
- 歌曲电台：从任意一首歌开启，无尽播放相似歌曲
- 视频：正在播放页左侧在封面与 MV 之间切换（原生 AVPlayer 硬件解码 HLS，最高 1080p），与歌曲同步，可进影院模式；仅 macOS
- 边下边播：AAC 128k；Premium 账号可选 AAC 256k（仅 macOS），听过的歌曲缓存在本机
- 登录：从 Chrome、Safari、Edge、Arc、Brave、Firefox 等浏览器导入 YouTube 登录（仅 macOS），或 Google 账号登录、粘贴 Cookie；登录后同步歌单、喜欢、播放记录与关注
- 音量均衡（按 YouTube 测得的响度），暂停与继续时淡出淡入
- 同步歌词（LRCLib），没有同步歌词时显示纯文本歌词
- 控制中心「正在播放」与媒体键、刘海播放器、菜单栏
- 退出后再打开，从上次的播放队列与进度继续
- 浅色 / 深色 / 跟随系统

暂不支持播客。

## 构建

需要 Go 1.27+。MyGo 取自 fork [wishworldbetter/mygo](https://github.com/wishworldbetter/mygo) 的 `native-video` 分支（`go.mod` 里的 `replace`），它加了原生视频视图。同时改 MyGo 时，把它克隆到本仓库旁的 `../mygo`，再建一个 `go.work`（已在 .gitignore 里）：`go work init . ../mygo`。

```bash
go tool mygo dev     # 开发模式运行
go tool mygo build   # 打包到 build/
go tool mygo build -platform windows/amd64,linux/amd64   # 在 Mac 上交叉编译 Windows、Linux 版
go test ./...        # 运行测试；LIVE=1 时运行访问 YouTube 的测试
```

## 发布

1. 改 `mygo.json` 的 `version`、`CHANGELOG.md`，提交后推送对应的 tag（如 `v0.2.0`）
2. GitHub Actions 在 Ubuntu 上编出 Windows（安装包）与 Linux（.deb、tar.gz）版，放进 Release 草稿
3. 在 Mac 上运行 `scripts/release-mac.sh -upload`：编出通用版（Apple 芯片与 Intel），用 Developer ID 签名，经 `asc` 交 Apple 公证并钉上票据，再传到同一个草稿
4. 检查草稿后发布

## 实现说明

- 目录与搜索走 InnerTube（music.youtube.com 的 `WEB_REMIX` 客户端）
- 音频与视频流走 `VISIONOS` 客户端：音频取 itag 140（AAC-LC），按区间分段下载；视频取 HLS 清单
- Premium 的 256k（itag 141）来自登录后的 `WEB_REMIX` 客户端，其地址经 YouTube 播放器脚本加扰：用 [yt-dlp/ejs](https://github.com/yt-dlp/ejs) 在系统 JavaScriptCore 里解出，解不出时退回 128k
- 音频由 MyGo 的 `audio/aac` 解码；视频是 MyGo 的原生 `VideoView`（AVPlayerLayer），静音播放并跟随原生音频的进度，不使用 WebView
- 浏览器登录读取各浏览器的 Cookie 数据库（Chromium 系用钥匙串里的密钥解密）；读取 Chrome、Safari 等需要在系统设置里允许「完全磁盘访问权限」
- Windows、Linux 版可以编译，但视频、浏览器导入登录与 256k 暂只支持 macOS

## 致谢

- [MyGo](https://github.com/egoist/mygo)：原生 UI 与应用框架
- [mygo-cloudmusic](https://github.com/egoist/mygo-cloudmusic)：界面与播放器
- [LRCLib](https://lrclib.net)：同步歌词
- [yt-dlp/ejs](https://github.com/yt-dlp/ejs)：解出 YouTube 流地址（Unlicense，内含 meriyah、astring）

本项目与 YouTube、Google 无关，音乐版权归各权利人所有。

## 许可证

[AGPL-3.0](LICENSE)
