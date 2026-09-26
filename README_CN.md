# MDict for Bob

当前产品版本：**1.3.0** · 本地 API：**v2**。

[English](README.md) | 简体中文

在 [Bob](https://bobtranslate.com/) 中查询你自己的本地 MDict 词典，全程离线。
发音直接来自词典自带的音频文件，不使用 TTS。

> 本项目是词典阅读器，不提供任何词典数据。请自行合法取得并使用
> `.mdx` 与可选的 `.mdd` 文件。

## 功能特点

- 支持 MDict v1.x/v2.x 词典，可同时使用多本，支持多卷 `.mdd`。
- 完全离线：没有云服务、遥测或网络请求。
- 四种词条显示方式：词典卡片、纯文本、Markdown，以及 Markdown（网页排版）——
  按词典自己的页面原样显示。
- 网页排版中的词可以直接点击查询，并能返回上一个词。
- 发音在后台播放，音量和语速可调。

## 工作方式

```text
Bob 插件 → 本地服务（127.0.0.1:15321）→ 你的 MDX/MDD 文件
```

`bob-mdict` 是运行在你 Mac 上的小服务，负责读取词典并回应 Bob 插件；插件只
负责显示结果。两者分别安装、分别更新，详见[更新](#更新)。

## 安装

需要 **Bob 1.20.0 或更高版本**。

### 1. 安装本地服务

使用 Homebrew：

```bash
brew install wakewon/tap/bob-mdict
brew services start bob-mdict
```

不使用 Homebrew 时，从
[最新 Release](https://github.com/wakewon/bob-plugin-mdict/releases) 下载
`bob-mdict-X.Y.Z-macos-installer.tar.gz`，解压并进入目录后运行：

```bash
./install.sh
```

之后如需卸载，运行 `./uninstall.sh`，不会删除你的词典。

### 2. 添加词典

每本词典放在各自的文件夹里，统一放在：

```text
~/Library/Application Support/bob-mdict/dictionaries/
```

```text
dictionaries/
├── 我的词典/
│   ├── 我的词典.mdx
│   └── 我的词典.mdd
└── 另一部词典/
    └── 另一部词典.mdx
```

`.mdd` 文件可有可无，里面是发音和图片。添加后重新扫描并检查：

```bash
bob-mdict --rescan
bob-mdict --check
```

### 3. 安装 Bob 插件

从 Release 下载 `MDict-vX.Y.Z.bobplugin` 并双击安装。然后打开
**Bob 偏好设置 → 翻译 → 服务**，选择 **文本翻译**，点击 `+` 并选择
**MDict**，启用后保存。

## 更新

本地服务和插件是分别更新的，新功能通常需要两者都更新，请一起升级。

1. **服务。** 使用 Homebrew 时，升级后需要重启服务才会生效：

   ```bash
   brew update
   brew upgrade bob-mdict
   brew services restart bob-mdict
   ```

   使用独立安装包时，下载新的安装包并再次运行 `./install.sh`。
2. **插件。** 在 Bob 中检查插件更新，或从 Release 页面下载新的 `.bobplugin`
   并双击安装。

如果新增的选项好像没有效果，多半是服务还是旧版本：对比 `bob-mdict --version`
与 `curl http://127.0.0.1:15321/v2/status` 中的 `serviceVersion`，不一致就重启
服务。新插件配旧服务时，新功能不会生效。词典和设置都会保留。

## 显示方式

在插件的“显示方式”中选择。

| | 词典卡片 | 纯文本 | Markdown | Markdown（网页排版） |
|---|---|---|---|---|
| 显示内容 | Bob 原生卡片 | 纯文本 | 结构化 Markdown | 词典自己的页面 |
| Bob 版本 | 1.20.0+ | 1.20.0+ | 1.21.0+（macOS 13+） | 1.21.0+（macOS 13+） |
| 发音 | Bob 自带按钮 | 不能播放 | 🔊 | 🔊 |
| 音量、语速、音量均衡 | ❌ | ❌ | ✅ | ✅ |
| 可点击查词 | 相关词 | ❌ | ❌ | ✅ |
| 例句 / 语法 / 扩展内容设置 | ✅ | ✅ | ✅ | ❌ 按词典原样显示 |
| 合并 / 分条显示记录 | ✅ | ✅ | ✅ | ✅ |

音量、语速和音量均衡只对由本项目播放的 🔊 有效，也就是两种 Markdown 显示。
词典卡片里的发音由 Bob 自己播放，没有这些调节。

**Markdown（网页排版）** 按词典出版方设计的版式显示，最接近原版词典。如果
`/list` 提示某本词典“缺少样式表”，把列出的 `.css` 文件复制到对应 `.mdx` 所在
文件夹并重新扫描，效果会更好。

### 点击查词与发音播放

网页排版中可以直接点击词条查词，通过链接打开的页面末尾有“← 原词”可以返回。
两种 Markdown 中的 🔊 会直接播放，不会打开浏览器。

Bob 无法直接接收这些点击，所以服务会在你的 Mac 上生成一个小助手
`MDict Lookup.app`（位于 `~/Library/Application Support/bob-mdict/`）。它由系统
自带工具在本机生成，不需要开发者证书，也不下载任何东西。使用时请留意：

- 第一次点击会询问是否允许 **MDict Lookup** 控制 Bob，请选择允许；服务更新后
  可能再问一次。
- Bob 可能每次都问“是否打开此链接？”。如需关闭，在 Bob 设置中把“打开翻译结果中
  的链接”改为“从不确认”（对 Bob 所有服务生效）。
- 如果小助手无法建立，链接会保持为普通文字，🔊 会改为在浏览器中打开录音。
- 点击记录和出错信息保存在 `~/Library/Logs/bob-mdict-helper.log`。

## 选择词典

每个 Bob MDict 服务显示一本词典的结果。

- **词典 ID 留空**（默认）：使用第一本收录该词的词典。
- **填写词典 ID**：只查这一本。

在 Bob 中用 MDict 查询 `/list` 可以看到各词典的 ID。想同时看多本词典，就在
Bob 中多次添加 MDict 服务并填写不同的 ID，Bob 会把它们显示为各自独立的卡片。

查询方向取决于词典自己的索引。很多英汉词典只索引英文词头，所以中译英需要
索引中包含中文条目的词典。

## 同一个词的多条记录

有些词典会在同一个词下保存多条记录。默认的“分条浏览”显示第一条，并在
`Other entries` 中列出 `wound²`、`wound³` 等入口；输入 `wound²` 即可打开，
输入 `wound¹` 回到第一条。网页排版中这些入口可以点击，其它显示方式下请复制文字。
“合并显示”会把所有记录放在一起。

## 插件设置

在 **Bob 偏好设置 → 翻译 → 服务 → MDict** 中调整。

| 设置 | 默认值 | 作用 |
|---|---|---|
| 本地服务地址 | `http://127.0.0.1:15321` | 只有服务改用其它端口时才需要修改。 |
| 词典 ID（可选） | 留空 | 留空使用第一本收录该词的词典；填写后只用这一本。 |
| 显示方式 | 词典卡片 | 结果的呈现方式，见[显示方式](#显示方式)。两种 Markdown 需要 Bob 1.21.0+（macOS 13+），在 Bob 1.20.0 上会显示 Markdown 原文。 |
| 重复词条显示方式 | 分条浏览 | **分条浏览**显示一条并附 `Other entries`；**合并显示**把全部记录放在一起。 |
| 发音音量 | 100% | 🔊 的播放音量，50%～200%。仅 Markdown 显示有效。 |
| 发音语速 | 1.0× | 🔊 的播放速度，0.5×～1.5×，变速不变调。仅 Markdown 显示有效。 |
| 发音音量均衡 | 开启 | 让不同词典的录音音量相近；关闭后按原有音量播放。仅 Markdown 显示有效。 |
| 显示例句 | 显示 | 显示例句及翻译。网页排版不使用此项。 |
| 显示语法限定说明 | 显示 | 显示 `[with object]` 之类的语法说明。网页排版不使用此项。 |
| 显示扩展内容 | 显示 | 显示短语、习语、词形和用法说明。网页排版不使用此项。 |
| 每个释义最多例句数 | `3` | 每个释义下最多显示几条例句。网页排版不使用此项。 |

## 故障排除

**无法连接本地服务。** 启动服务，并确认插件中的“本地服务地址”与服务端口一致：

```bash
brew services start bob-mdict
curl http://127.0.0.1:15321/v2/status
```

**未发现词典。** 每本词典至少需要一个 `.mdx` 文件：

```bash
open ~/Library/Application\ Support/bob-mdict/dictionaries/
bob-mdict --rescan
bob-mdict --list-dictionaries
```

**词典 ID 无效。** 在 Bob 中查询 `/list`，复制当前 ID 并更新对应服务。更换词典
版本时 ID 可能改变；移动或改名文件不会。

**查到词条但没有发音按钮。** 只有词典的 `.mdd` 中确有对应录音时才会出现发音，
不会用其它方式补齐。

**部分发音缺失。** 少数旧词典使用 `.spx` 录音，用 `brew install speex` 安装解码器。

**插件与服务版本不兼容，或新设置没有效果。** 按[更新](#更新)同时升级两者。

## 隐私与版权

一切都留在你的 Mac 上：没有遥测、没有对外请求，服务只接受本机连接。

本项目不含任何词典内容；MDX/MDD 的权利属于相应出版方，请自行确保来源和使用方式
合法。项目采用 GPL-3.0-or-later，详见 [LICENSE](LICENSE) 与
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。

## 面向开发者

```bash
bob-mdict --debug-lookup WORD   # 解析器如何理解这个词
bob-mdict --diagnose NAME       # 词典被理解到什么程度
bob-mdict --validate NAME --validate-out DIR
```

```bash
gofmt -w .
go vet ./...
go test ./...
go test -short -race ./...
node --test plugin/main.test.js
./scripts/release.sh doctor
```

更多说明：[架构](docs/ARCHITECTURE.md) · [Parser](docs/PARSER.md) ·
[HTTP API](docs/API.md) · [发布](docs/RELEASE.md) ·
[已知问题与局限](docs/KNOWN_ISSUES.md)
