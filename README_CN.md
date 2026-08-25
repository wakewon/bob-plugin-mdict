# MDict for Bob

当前产品版本：**0.2.1** · 本地 API：**v2**。

[English](README.md) | 简体中文

在 [Bob](https://bobtranslate.com/) 中查询你自己的本地 MDict 词典。查询、
解析和发音播放全部在本机完成；真人录音直接来自词典配套的 MDD。

> 本项目是词典阅读器，不提供任何词典数据。请自行合法取得并使用
> `.mdx` 与可选的 `.mdd` 文件。

## 功能特点

- 支持 MDict v1.x/v2.x、递归发现、多本词典和多卷 MDD（`.mdd`、
  `.1.mdd`、`.2.mdd` 等）。
- 完全本地：没有在线词典 API、云端解析、遥测或外部网络请求；服务只监听
  loopback 地址。
- 结构化词条：词性、义项与子义项、双语释义、例句、词形、短语、习语、
  短语动词、交叉引用和说明。
- 真人发音优先且仅限真人发音。英式、美式、共用音标和未标口音信息分别
  保留；项目中不存在 TTS 后备路径。
- 默认无需配置词典 ID：自动使用第一本收录当前词的词典。

## 工作方式

```text
Bob 插件 → http://127.0.0.1:15321 → MDX/MDD → 语义解析器
                                               → EntrySet IR → Bob toDict
```

`bob-mdict` 是负责索引、解析和 MDD 资源的本地 Go 服务。Bob 插件只是轻量
JavaScript 客户端，不解析 MDX、HTML 或音频。插件与服务通过带版本号的本地
API 协作。

## 安装

插件要求 **Bob 1.20.0 或更高版本**。`/list` 控制查询依赖 Bob 的
`query.originalText`；普通查词仍使用预处理后的 `query.text`。

### 1. 安装本地服务

使用 Homebrew：

```bash
brew install wakewon/tap/bob-mdict
brew services start bob-mdict
```

不使用 Homebrew 时，从
[最新 Release](https://github.com/wakewon/bob-plugin-mdict/releases) 下载对应
Mac 架构的压缩包，解压后运行：

```bash
./packaging/install.sh
```

独立安装脚本会把程序放入 `~/.local/bin`，注册登录时启动的 LaunchAgent，
并创建默认词典目录。卸载可运行 `./packaging/uninstall.sh`；脚本不会删除
你的词典。

### 2. 添加 MDX/MDD

建议每本词典使用一个子目录，统一放在：

```text
~/Library/Application Support/bob-mdict/dictionaries/
```

例如：

```text
dictionaries/
├── 我的词典/
│   ├── 我的词典.mdx
│   ├── 我的词典.mdd
│   └── 我的词典.1.mdd
└── 另一部词典/
    └── 另一部词典.mdx
```

服务会递归发现 MDX，并按文件名匹配同目录中的 MDD 卷。添加完成后执行：

```bash
bob-mdict --rescan
bob-mdict --check
```

### 3. 安装并添加 Bob 插件

从 Release 下载 `MDict-vX.Y.Z.bobplugin` 并双击安装。根据 Bob 当前官方
用语，打开 **Bob 偏好设置 → 翻译 → 服务**，选择 **文本翻译**，点击下方
`+` 号并选择 **MDict**；启用后点击右下角 **保存**。

已安装插件也可以在 **Bob 偏好设置 → 插件** 中查看。

## 选择词典

每个 Bob MDict 服务实例只显示一本词典的结果：

```text
词典 ID 留空  → 按本地服务顺序，使用第一本收录查询词的词典
填写词典 ID  → 只查询该 ID 对应的词典
```

普通用户保持留空即可。需要固定词典时，在 Bob 中用 MDict 精确查询
`/list`。结果会显示所有已发现词典的名称、ID，以及不可用词典的诊断。
`/list` 前后的空白会被忽略；普通单词 `list` 仍然按词条查询。

如果希望固定同时使用多本词典，请在 Bob 中多次添加 MDict 文本翻译服务，
并为每个实例填写不同 ID。Bob 自己负责服务排序、启停和独立结果卡片，
不会把不同词典的义项混在一张卡片里。

词典 ID 是根据 MDX 内容分段采样得到的 16 位指纹。移动目录、修改目录名或
MDX 文件名不会改变 ID；更换词典版本通常会改变 ID。早期开发版本使用过
基于路径的 ID；升级后如旧 ID 失效，查询一次 `/list` 并替换即可。

查询方向由已安装 MDX 的词头索引决定。很多“英汉”词典只索引英文词头，
释义中的中文译文并不是反向查询键。若需中译英查询，请安装词头索引中包含
中文条目的 MDX。

## 同一词头的多条记录

部分 MDict 会为同一词头保存多条独立记录。默认的“分条浏览”只完整显示第一
条，并在 `Other entries` 中提供可点击的 `wound²`、`wound³` 等入口。也可以
直接输入 `wound²`、`wound^2` 或 `wound^{2}`；`wound¹` 可返回第一条。词尾的
上标整数保留为本插件的记录导航语法。

在插件设置中选择“合并显示”后，所有记录会继续在同一词典卡中用 `¹`、`²`、
`³` 等标记完整展示。

例句会直接按展示释义分块，例如 `Examples · verb 1`、
`Examples · verb 2`。See also 交叉引用会在适用时使用 Bob 的结构化
`relatedWordParts` 表达；短语和其它带解释的扩展内容仍使用 additions。

## 插件设置

| 设置 | 默认值 | 作用 |
|---|---|---|
| 本地服务地址 | `http://127.0.0.1:15321` | 只有服务改过端口时才需修改。 |
| 词典 ID（可选） | 留空 | 留空使用首个命中；填写后固定一本。用 `/list` 查看 ID。 |
| 重复词条显示方式 | 分条浏览 | 完整显示一条并提供可点击的 `Other entries`；“合并显示”会在同一卡片展示全部带序号记录。 |
| 显示例句 | 显示 | 显示例句及双语翻译。 |
| 显示扩展内容 | 显示 | 显示短语、习语、短语动词、结构化交叉引用、词形和说明。 |
| 每个释义最多例句数 | `3` | 分别限制每个释义或子释义显示的例句数。 |

Bob 的“验证”会检查服务身份、API 版本、是否有可用词典，以及已填写的词典
ID 是否仍然有效。

## 命令行

```bash
bob-mdict --version              # 程序版本与 API 版本
bob-mdict --check                # 安装、词典和音频解码检查
bob-mdict --list-dictionaries    # 名称、ID、词条数和解析 Profile
bob-mdict --rescan               # 重新发现并建立索引
bob-mdict --debug-lookup WORD    # 输出结构化 EntrySet IR，供开发调试
```

本地 HTTP API 见 [docs/API.md](docs/API.md)。

## 故障排除

### 无法连接本地 MDict 服务

```bash
brew services start bob-mdict
curl http://127.0.0.1:15321/v2/status
```

确认插件中的“本地服务地址”与服务实际端口一致。状态响应中的
`serviceVersion`、`buildCommit`、`apiVersion` 标识端口上真正运行的进程；
仅仅生成一个新 binary 并不会更新该进程。

### 未发现词典

```bash
open ~/Library/Application\ Support/bob-mdict/dictionaries/
bob-mdict --rescan
bob-mdict --list-dictionaries
```

每本词典至少需要一个 MDX。MDD 可选，主要提供真人录音和其它资源。

### 指定的词典 ID 无效或不可用

在 Bob 中查询 `/list`，复制当前 ID 并更新对应服务实例。不可用词典会显示
诊断，且不会影响其它词典。

### 查到词条但没有发音按钮

只有词条引用的真人录音确实存在于匹配的 MDD 中时，才会显示发音。缺少
MDD、资源键不存在或原词典没有录音时都不会显示；本项目不会用 TTS 补齐。

### MDD 存在但部分发音缺失

少数旧词典使用 Ogg-Speex（`.spx`）。安装解码器：

```bash
brew install speex
```

转码后的 WAV 只保存在本机。服务启动时会删除超过 30 天的缓存，并把总量
限制在 256 MiB。

### 插件与服务版本不兼容

```bash
brew upgrade bob-mdict
```

然后从 Release 更新 Bob 插件。

## 隐私与安全

- 查询词和词典资源不会离开 Mac；没有分析、遥测或使用上报。
- 服务只绑定 `127.0.0.1`/`::1`，并拒绝非 loopback Origin。
- MDD 资源使用每次进程启动后生成的不可伪造 token，不暴露文件路径。
- API 不接受任意文件路径，本地服务也不会发起外部网络请求。

## 版权

本仓库、二进制和 `.bobplugin` 均不包含词典内容。MDX/MDD 的权利属于相应
出版方，用户应自行确保来源和使用方式合法。

仓库中的 parser fixture 是专为测试从零构造的最小 synthetic HTML：词头、
释义、翻译、例句和资源路径均为虚构内容，只保留兼容性测试必需的少量
selector/class 和 DOM 关系。

项目采用 GPL-3.0-or-later，详见 [LICENSE](LICENSE) 与
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。

## 开发

```bash
gofmt -w .
go vet ./...
go test ./...
go test -race ./...
node --test plugin/main.test.js
./scripts/build-plugin.sh
./scripts/build-server.sh
./scripts/dev-deploy.sh
```

`build-server.sh` 只生成构建产物。`dev-deploy.sh` 会安全更新独立安装方式的
开发 LaunchAgent，等待实际监听进程，并拒绝替换 Homebrew 或其它方式管理的
daemon；最后核对 runtime 版本/commit 是否与 `VERSION` 和仓库 HEAD 一致。

真实词典集成测试不会把词条内容写入 tracked snapshot。可以指向自己合法
持有的本地词典目录：

```bash
BOB_MDICT_TEST_DICTIONARIES=/path/to/dictionaries go test ./internal/service -v
```

更多说明：[架构](docs/ARCHITECTURE.md) · [Parser](docs/PARSER.md) ·
[HTTP API](docs/API.md)
