# T-REX

T-REX, Terminal Reimagined Experience, 是一个 Go 编写的终端原生 AI 助手。它面向代码协作、项目分析、自动化排障和授权安全测试，支持流式对话、工具调用、项目记忆、Skills、MCP、Hooks、权限控制和会话报告。

## 主要特性

- 单文件原生二进制，无需 Node.js 运行时。
- 支持 Anthropic Messages API 和 OpenAI 兼容接口。
- 交互模式和 `prompt` 模式都支持多轮工具调用。
- 严格工作区边界：内置文件工具默认只在当前 `project.path` 内读写、编辑和搜索；Shell 默认也固定在工作区内执行。
- 运行中插话：模型输出期间可以输入新指令，T-REX 会中断当前流并把新指令加入上下文继续执行。
- 实时上下文状态：输入提示和工具动画会显示 `ctx` 上下文占用。
- `/compact` 可压缩历史，保留目标、工具结果、关键路径、失败信息和最近消息。
- `/report` 可导出当前会话为 Markdown，默认写入 `.cache/reports/`。
- `/test` 会按项目类型建议测试命令。
- `--work-mode code-audit` 或 `/workmode code-audit` 启用源码安全审计工作流，覆盖 PHP、Java/JSP、.NET/ASP、JavaScript/Node.js、Ruby、Rust 等技术栈。
- `--work-mode pentest` 或 `/workmode pentest` 启用授权渗透测试/红队打点工作流。
- `--work-mode ai-security` 启用大模型、RAG、MCP 和智能体安全评估工作流。
- 自动工作模式路由：用户提示词明显要求“写代码/修复/实现”“代码审计/源码审计”“渗透测试/红队/漏洞验证”时，会自动切到对应工作流。
- 内置 Mission/Scope/Finding/Evidence 状态，支持任务闭环、证据库和结构化报告。
- 内置 `SecurityCatalog` 漏洞检查目录和 `SecurityTest` 测试矩阵，覆盖 Web/API、基础设施、云、客户端和 AI Agent。
- Pentest 模式要求漏洞 POC 优先输出 Burp 风格原始请求包和响应包，即 `Burp Request` / `Burp Response`。
- 内置 `Todo` 工具和 `/todo` 面板，适合长任务维护 checklist。
- 内置 Shell、Read、Write、Edit、Glob、Grep、Remember、Skills、MCP 和安装工具。
- `MCP_Install` 可添加 MCP server 配置，`Skills_Install` 可把本地 skill 安装到 `config.json` 同级的 `skills/`。
- 生命周期 Hooks：`SessionStart`、`UserPromptSubmit`、`PreToolUse`、`PostToolUse`、`PostToolUseFailure`、`Stop`、`SessionEnd`。
- 十二生肖启动动画：`startupArt: "auto"` 时按每小时 12 个 5 分钟区间轮换，避免 12 小时一轮太慢；也可以固定某个生肖。
- Windows 终端会尝试启用 ANSI/高对比颜色，并使用兼容性更好的 ASCII 动画。

## 快速开始

```bash
go build -buildvcs=false -o t-rex.exe .
```

Windows:

```bat
set ANTHROPIC_API_KEY=your-api-key
t-rex
```

Windows Terminal/PowerShell should use UTF-8 for readable Chinese output:

```powershell
chcp 65001
$OutputEncoding = [Console]::OutputEncoding = [Text.UTF8Encoding]::new($false)
Get-Content -Encoding utf8 README.md
```

T-REX also switches console input/output code pages to UTF-8 at startup when a Windows console is attached.

Linux/macOS:

```bash
export ANTHROPIC_API_KEY=your-api-key
./t-rex
```

常用命令：

```bash
t-rex --path ./my-project
t-rex --model claude-sonnet-4-20250514
t-rex prompt "运行测试并修复失败项" --max-tool-rounds 24
t-rex --work-mode code-audit
t-rex --work-mode pentest
t-rex --resume
```

如果当前目录的 `.git` 元数据不完整，Go 构建可能提示 VCS stamping 错误，可使用：

```bash
go build -buildvcs=false -o t-rex.exe .
```

## 任务闭环

T-REX 内置了轻量任务状态，适合把一次长任务从“聊天”推进成“执行闭环”：

- `Mission`: 目标、阶段、完成条件、阻塞原因、最终交付物。
- `Goal`: goal 模式的目标、验收条件（`done_when`）、进度和状态（active/achieved/blocked/paused）。
- `Scope`: 授权范围，尤其适合安全测试。
- `Finding`: 结构化漏洞/问题记录。
- `Evidence`: 将完整证据保存到 `.cache/evidence/`，上下文和报告里只引用摘要和路径。

交互命令：

```text
/mission set 对 example.com 做授权渗透测试并输出报告
/mission phase recon
/mission done 形成包含 Burp 请求/响应的报告草稿
/scope add https://example.com
/finding add 未授权访问用户资料
/finding list
/evidence list
/goal 让全部单元测试通过并补齐缺失用例
/goal criteria go test ./... 全绿
```

Agent 也可以直接调用 `Mission`、`Goal`、`Scope`、`Finding`、`Evidence` 工具维护同一份状态。

## 工作模式

T-REX 有三层“模式”，不要混淆：

- `workMode` 是 agent 工作流模式，决定系统提示词和任务方法。
- `agent mode` 是 agent 行为模式，决定它如何推进任务（直接执行 / 先规划 / 目标驱动）。
- `permission mode` 是权限模式，决定工具是否需要确认。

### Agent 模式（normal / plan / goal）

`agent mode` 与 `workMode`、`permission mode` 正交，可同时使用。

| 模式 | 用途 |
| --- | --- |
| `normal` | 默认：直接回答并按要求执行。 |
| `plan` | 只读调研，先产出结构化计划，用户批准后再执行。 |
| `goal` | 以目标 + 验收条件为完成定义，自主多轮推进直到达成或受阻。 |

- **plan 模式**：严格只读，`Write`/`Edit`/`Shell`/`Bash` 等写入与执行类工具被阻止，只允许 `Read`、`Glob`、`Grep`、`LSP`、`Skills`、`SecurityCatalog`、`CodeAuditCatalog` 等只读工具，以及 `Todo`/`Mission`/`Goal` 状态工具。计划完成后 agent 停止并等待你选择“批准并执行 / 继续完善 / 取消”。
- **goal 模式**：agent 用 `Goal` 工具维护 objective、`done_when` 验收条件、progress 和 status，每一步后自我检查，未全部满足且仍可推进时会自动继续（默认最多 12 次自主续跑），仅在验收条件有证据支持或确实受阻时停止。

切换方式：

```text
/agent            # 交互式选择
/agent plan
/agent goal
/agent normal
/plan <request>   # 进入 plan 模式并立即开始规划该请求
/goal <objective> # 进入 goal 模式并设置目标
```

也可以启动时通过环境变量指定：`T_REX_AGENT_MODE=plan`。

### Coding 模式

默认工作模式，适合代码阅读、修改、测试、排障、文档和自动化任务。

如果没有显式指定 `/workmode` 或 `--work-mode`，T-REX 会根据用户提示词自动路由。比如“对这个 CMS 做代码审计并出报告”会进入 `code-audit`，“对目标做授权渗透测试”会进入 `pentest`，“帮我实现上下键选择”会进入 `coding`。

支持的模式：

| 模式 | 用途 |
| --- | --- |
| `coding` | 默认代码协作、排障、文档和自动化 |
| `bugfix` | 复现、定位、最小修复、测试验证 |
| `code-review` | 正确性、安全、回归和测试缺口审查 |
| `code-audit` | PHP、Java/JSP、.NET/ASP、JS/Node.js、Ruby、Rust 等源码安全审计 |
| `pentest` | 授权渗透测试/红队打点 |
| `ai-security` | 提示词注入、越狱、RAG、工具、记忆与智能体安全检测 |
| `incident-response` | 事件响应、时间线、IOC、遏制与恢复 |
| `malware-analysis` | 恶意样本静态优先分析、IOC 和处置建议 |
| `osint` | 来源、置信度、线索和不确定性跟踪 |
| `ctf` | CTF 题目分流、求解和 writeup |

示例：

```bash
t-rex --work-mode coding
t-rex --work-mode bugfix
t-rex --work-mode code-review
t-rex --work-mode code-audit
t-rex --work-mode incident-response
```

交互中切换：

```text
/workmode coding
```

### Code Audit 模式

面向授权源码安全审计，支持 PHP、Java/JSP、.NET/ASP、JavaScript/Node.js、Ruby、Rust 和混合栈。该模式会要求 agent 先建立架构、路由、入口、认证授权、数据存储、后台任务和信任边界，再按语言/框架查询 `CodeAuditCatalog`，用 source-to-sink 数据流确认问题。

审计输出的漏洞必须包含 POC。Web/API 类漏洞的主 POC 固定使用 Burp 风格原始 HTTP：`Burp Request` 和 `Burp Response`。如果只能从源码推断请求/响应，报告会标明 reconstructed；如果无法推导可信 Burp 请求/响应，应标记为 `needs-dynamic-validation`，不要直接当 confirmed 漏洞。

```text
/workmode code-audit
/audit list php
/audit list java
/audit list nodejs
/audit search deserialization
/audit show AUD-PHP-DESER
/report code-audit
```

`CodeAuditCatalog` 的方法论参考 OWASP ASVS/Code Review Guide、CWE、Semgrep、CodeQL 等开源规则体系，但内置内容保持 payload-free，不会把高风险攻击载荷注入系统提示词。

### Pentest 模式

适合授权渗透测试、红队打点、Web/API/应用/基础设施安全评估。该模式会要求：

- 先确认授权范围、测试窗口、规则和排除项。
- 使用 Todo 维护阶段 checklist。
- 优先低噪声枚举，再做验证。
- 明确区分 confirmed、likely、false positive、untested。
- 每个漏洞记录影响、复现、证据、修复建议和复测方法。
- POC 必须包含 Burp 风格原始 HTTP 请求包和响应包。
- 使用 `SecurityCatalog` 建立与目标匹配的检查面，覆盖身份/MFA、会话、越权、注入、文件、SSRF、协议、GraphQL/WebSocket、业务逻辑、云、供应链、客户端及 AI 组件。
- 使用 `AssessmentPlan` 记录目标画像、角色、规则、排除项和 `test_level`，支持 `passive`、`low-noise`、`active`、`intrusive` 四档主动测试级别。
- 使用 `SecurityCoverage` 记录 tested/not-tested/blocked/not-applicable 覆盖矩阵，并可通过 `SecurityCoverage(action=seed, target_features=[...])` 从 `SecurityCatalog` 自动生成目标相关检查项。
- `Mission`、`Scope`、`Finding`、`Evidence`、`AssessmentPlan`、`SecurityCoverage`、`SecurityTest` 和 `Todo` 会随 session 持久化，恢复会话后继续保持测试闭环。
- `/report pentest` 会在写报告前输出质量门禁提示，集中指出 confirmed 漏洞缺 Burp POC、影响、修复建议、复测方法，以及未测/阻塞覆盖项缺原因的问题。
- Pentest 模式下如果设置了 `AssessmentPlan.test_level`，Shell 命令会按测试级别做低噪声策略约束；例如 `low-noise` 会阻止大范围扫描、爆破和 WAF/CDN 绕过类动作，除非明确提升授权级别。

示例：

```bash
t-rex --work-mode pentest --max-tool-rounds 32
```

交互中切换：

```text
/workmode pentest
/assessment level low-noise
/coverage seed swagger login websocket
/coverage gaps
```

POC 期望格式示例：

```text
Burp Request
GET /api/user?id=1001 HTTP/1.1
Host: target.example
Cookie: session=REDACTED
Accept: application/json


Burp Response
HTTP/1.1 200 OK
Content-Type: application/json

{"id":1001,"email":"redacted@example.com"}
```

### AI Security 模式

面向大模型应用、RAG、工作流编排、MCP/插件和自主智能体的授权安全评估。攻击分类参考根目录的《提示词攻击手册-目录清晰版.md》，运行时只加载分类与验证目标，不会把整份高风险语料注入系统提示。

重点覆盖：

- 直接提示词注入、角色/权限伪装、越狱、世界观改写、多语言、编码混淆和多轮渐进攻击。
- 文档、网页、邮件、OCR、RAG、工具结果和 Agent 间消息中的间接注入。
- 系统提示词/上下文泄露、RAG 越权、跨用户/租户泄露、敏感信息与日志泄露。
- 工具参数注入、越权调用、过度自主性、权限确认绕过、混淆代理和链式工具提权。
- 记忆投毒、跨会话污染、MCP/插件/多智能体信任、输出处理、幻觉决策和资源耗尽。

每个用例由 `SecurityTest` 记录攻击向量、入口通道、轮次、预期安全行为、实际行为、pass/fail/blocked/not-run、证据和关联漏洞。失败用例再提升为 `Finding`，避免把单次异常直接当成已确认漏洞。

```text
/workmode ai-security
/vulns list ai-agent
/vulns show LLM-INDIRECT
/sectest add RAG 文档间接提示词注入
/sectest list
/report ai-security
```

## 报告系统

```text
/report session
/report pentest
/report ai-security
/report code-audit
```

- `/report session` 导出普通会话报告。
- `/report pentest` 从 Mission/Scope/AssessmentPlan/SecurityCoverage/Finding/Evidence 生成渗透测试报告，包含执行摘要、范围、方法、覆盖矩阵、质量门禁、风险表、漏洞详情、Burp Request/Response、证据文件、覆盖缺口、残余风险和后续步骤。
- `/report ai-security` 生成 AI/Agent 安全报告，包含系统画像、测试覆盖矩阵、逐项预期/实际结果、提示词或会话证据引用、工具调用证据、漏洞、限制和复测计划。
- `/report code-audit` 生成源码安全审计报告，包含审计范围、方法、语言/框架覆盖、source-to-sink 证明、Burp Request/Response POC、结构化发现、证据文件、限制和复测建议。

## 权限模式

```text
/mode default
/mode plan
/mode bypass
```

- `default`：敏感操作需要确认。
- `plan`：只读/规划模式，阻止写入和执行类操作。
- `bypass`：跳过确认，自动批准工具调用。仅在可信、受控环境下使用。

默认权限配置：

| 工具 | 作用 | 默认权限 |
| --- | --- | --- |
| `Shell` | 使用当前系统原生命令行执行命令 | ask |
| `Bash` | `Shell` 的兼容别名 | ask |
| `Read` | 读取工作区文件 | allow |
| `Write` | 创建或覆盖工作区文件 | ask |
| `Edit` | 精确替换文件内容 | ask |
| `Glob` | 按 glob 查找文件 | allow |
| `Grep` | 按正则搜索文件内容 | allow |
| `Remember` | 写入长期项目记忆或用户偏好 | allow |
| `Todo` | 维护当前会话 checklist | allow |
| `Mission` | 维护任务目标、阶段、完成条件和交付物 | allow |
| `Scope` | 维护授权范围 | allow |
| `AssessmentPlan` | 维护目标画像、测试级别、规则、角色和排除项 | allow |
| `SecurityCoverage` | 维护渗透测试覆盖矩阵和未测/阻塞原因 | allow |
| `Finding` | 维护结构化发现/漏洞 | allow |
| `Evidence` | 保存完整证据到 `.cache/evidence/` | allow |
| `SecurityCatalog` | 查询完整漏洞与 AI Agent 测试目录 | allow |
| `SecurityTest` | 维护 AI/Agent 安全测试矩阵 | allow |
| `CodeAuditCatalog` | 查询多语言源码安全审计检查目录 | allow |
| `Skills_List` / `Skills_Read` / `Skills_ReadResource` | 发现和读取本地 Skills | allow |
| `Skills_Install` | 从本地路径安装 skill 或创建 SKILL.md | ask |
| `MCP_Install` | 添加或更新 MCP server 配置 | ask |
| `mcp__server__tool` | 已连接 MCP server 暴露的工具 | ask |

Shell 沙箱会拦截常见高风险模式，例如 `cd ..`、工作区外路径重定向、Windows 删除/移动外部路径等。Shell/Write/Edit 默认写安全审计日志到 `.cache/audit.jsonl`。

## 交互命令

```text
/help
/tokens
/history
/paste [instruction]
/writepaste <file_path>
/compact
/memory list
/memory add preference keep answers concise
/skills list
/skills read <id>
/mcp
/mcp reconnect
/mission show
/scope list
/assessment show
/coverage seed swagger login websocket
/coverage gaps
/finding list
/evidence list
/vulns list ai-agent
/audit list nodejs
/sectest list
/report session|pentest|ai-security|code-audit
/test
/todo
/mode default|plan|bypass
/agent normal|plan|goal
/plan <request>
/goal <objective>
/workmode coding|code-audit|pentest|ai-security
/quit
```

多行粘贴：

- `/paste [instruction]` 进入多行输入，粘贴长文档后单独输入 `/end`，整段内容会作为一条消息提交给 agent。
- `/writepaste <file_path>` 进入多行写入，粘贴内容后单独输入 `/end`，内容会直接写入工作区内目标文件。

## 安装 MCP 和 Skills

Agent 在需要额外能力时可以调用安装工具，但默认会先走权限确认：

- `MCP_Install` 支持 `stdio`、`http`、`streamable-http`、`sse` 类型，会写入 T-REX 配置；安装后使用 `/mcp reconnect` 重新连接。
- `Skills_Install` 支持复制工作区内已有的 `SKILL.md`/skill 目录，或直接创建新的 `SKILL.md`，目标目录是 `config.json` 同级的 `skills/<name>`。
- 需要从网络下载 MCP 包或 skill 仓库时，Agent 仍需通过 `Shell` 执行 `npm`、`git` 等命令，并按权限策略确认。

运行中插话示例：

```text
先停一下，改成写一份漏洞验证报告
不要继续这个思路，先检查登录 cookie 是否保存成功
把刚才的结果总结成中文
```

这些输入会中断当前模型流，并作为新的用户指令继续执行。

## 十二生肖启动动画

配置项：

```json
{
  "ui": {
    "startupStyle": "full",
    "startupArt": "auto",
    "startupSize": "normal",
    "thinkingAnimation": true,
    "theme": "classic"
  }
}
```

- `startupStyle`: `full`、`compact`、`off`
- `startupArt`: `auto` 或固定生肖：`rat`、`ox`、`tiger`、`rabbit`、`dragon`、`snake`、`horse`、`goat`、`monkey`、`rooster`、`dog`、`pig`
- `startupSize`: `compact`、`normal`、`large`

`auto` 模式按每小时 12 个 5 分钟区间轮换：

| 分钟 | 生肖 |
| --- | --- |
| 00-04 | rat |
| 05-09 | ox |
| 10-14 | tiger |
| 15-19 | rabbit |
| 20-24 | dragon |
| 25-29 | snake |
| 30-34 | horse |
| 35-39 | goat |
| 40-44 | monkey |
| 45-49 | rooster |
| 50-54 | dog |
| 55-59 | pig |

## 配置示例

默认配置位置：

- Windows: `%LOCALAPPDATA%\T-Rex\config.json`
- macOS: `~/Library/Application Support/T-Rex/config.json`
- Linux: `~/.config/t-rex/config.json`

默认全局 Skills 根目录与 `config.json` 放在同一目录：

- Windows: `%LOCALAPPDATA%\T-Rex\skills`
- macOS: `~/Library/Application Support/T-Rex/skills`
- Linux: `~/.config/t-rex/skills`

### 国内 API URL 配置说明

国内平台统一推荐使用 `"provider": "openai-compatible"`。`baseURL` 按国内平台控制台给出的 API 根地址填写，例如 DeepSeek 使用 `https://api.deepseek.com`，MiniMax 使用 `https://api.minimax.chat/v1`，通义千问使用 `https://dashscope.aliyuncs.com/compatible-mode/v1`。不要把 Markdown 链接粘贴进 `config.json`。

```json
{
  "ai": {
    "provider": "anthropic",
    "model": "claude-sonnet-4-20250514",
    "maxTokens": 8192,
    "contextWindow": 200000,
    "maxToolRounds": 16,
    "allowTextToolCalls": false,
    "workMode": "coding"
  },
  "project": {
    "path": "C:/work/my-project",
    "allowedPaths": ["src", "tests"],
    "deniedPaths": [".env", ".git", ".cache"],
    "maxFileSize": 102400,
    "toolOutputMaxBytes": 65536
  },
  "permissions": {
    "mode": "default",
    "allow": ["Read", "Glob", "Grep", "Skills_List", "Skills_Read", "Skills_ReadResource", "Remember", "Todo", "Mission", "Scope", "Finding", "Evidence", "SecurityCatalog", "SecurityTest", "CodeAuditCatalog"],
    "ask": ["Shell", "Bash", "Write", "Edit", "mcp__*"],
    "deny": [],
    "shellRiskProfile": "strict",
    "shellAllowlist": ["git status", "go test ./..."]
  },
  "ui": {
    "startupStyle": "full",
    "startupArt": "auto",
    "startupSize": "normal",
    "toolOutput": "compact",
    "thinkingAnimation": true,
    "theme": "classic"
  },
  "audit": {
    "enabled": true,
    "path": ".cache/audit.jsonl"
  }
}
```

配置语义：`allowTextToolCalls` 默认关闭，只有兼容旧协议时才建议打开；`allowedPaths`/`deniedPaths` 作用于内置文件工具和 Shell 的可识别路径，拒绝规则优先；`maxFileSize` 同时限制 Read、Write、Edit 和搜索。`shellRiskProfile` 为 `default` 时高风险命令单独确认，`strict` 要求高风险命令命中 `shellAllowlist`，`allowlist` 则要求所有 Shell 命令命中列表；`allowManagedPermissionRulesOnly` 会禁止持久化授权和 bypass。

检查配置：

```bash
t-rex config doctor
```

## 国内模型 API URL 参考

T-REX 的国内平台配置统一使用 `openai-compatible` 格式。`model` 会原样发送给平台，不做固定白名单校验；模型名请以各平台控制台当前展示为准。`baseURL` 必须是普通 JSON 字符串，不要粘贴 Markdown 链接，例如应写 `"baseURL": "https://api.deepseek.com"`，不要写成 `"baseURL": "[https://api.deepseek.com",](https://api.deepseek.com",)`。

除 `provider: "anthropic"` / `"claude"` 外，T-REX 都会按 OpenAI-compatible Chat Completions 调用。国内平台建议统一写：

```json
{
  "ai": {
    "provider": "openai-compatible",
    "model": "平台模型名",
    "apiKey": "平台 API Key",
    "baseURL": "平台国内 API 地址",
    "maxTokens": 8192,
    "temperature": 1.0,
    "timeoutSeconds": 300,
    "maxRetries": 3
  }
}
```

### DeepSeek

```json
{
  "ai": {
    "provider": "openai-compatible",
    "model": "deepseek-v4-pro",
    "apiKey": "your-deepseek-api-key",
    "baseURL": "https://api.deepseek.com",
    "maxTokens": 8192,
    "temperature": 1.0,
    "timeoutSeconds": 300,
    "maxRetries": 3
  }
}
```

参考：https://api-docs.deepseek.com/zh-cn/

### MiniMax

```json
{
  "ai": {
    "provider": "openai-compatible",
    "model": "MiniMax-M3",
    "apiKey": "your-minimax-api-key",
    "baseURL": "https://api.minimax.chat/v1",
    "maxTokens": 8192,
    "temperature": 1.0,
    "timeoutSeconds": 300,
    "maxRetries": 3
  }
}
```

参考：https://platform.minimaxi.com/

### 通义千问 / 阿里云百炼 DashScope

```json
{
  "ai": {
    "provider": "openai-compatible",
    "model": "qwen-plus",
    "apiKey": "your-dashscope-api-key",
    "baseURL": "https://dashscope.aliyuncs.com/compatible-mode/v1",
    "maxTokens": 8192,
    "temperature": 1.0,
    "timeoutSeconds": 300,
    "maxRetries": 3
  }
}
```

参考：https://help.aliyun.com/zh/model-studio/compatibility-of-openai-with-dashscope

### 月之暗面 / Kimi

```json
{
  "ai": {
    "provider": "openai-compatible",
    "model": "moonshot-v1-8k",
    "apiKey": "your-moonshot-api-key",
    "baseURL": "https://api.moonshot.cn/v1",
    "maxTokens": 8192,
    "temperature": 1.0,
    "timeoutSeconds": 300,
    "maxRetries": 3
  }
}
```

参考：https://platform.moonshot.cn/docs

### 智谱 GLM

```json
{
  "ai": {
    "provider": "openai-compatible",
    "model": "glm-4-plus",
    "apiKey": "your-bigmodel-api-key",
    "baseURL": "https://open.bigmodel.cn/api/paas/v4",
    "maxTokens": 8192,
    "temperature": 1.0,
    "timeoutSeconds": 300,
    "maxRetries": 3
  }
}
```

参考：https://open.bigmodel.cn/dev/api

### 火山方舟 / Doubao

```json
{
  "ai": {
    "provider": "openai-compatible",
    "model": "doubao-model-endpoint-id",
    "apiKey": "your-volcengine-api-key",
    "baseURL": "https://ark.cn-beijing.volces.com/api/v3",
    "maxTokens": 8192,
    "temperature": 1.0,
    "timeoutSeconds": 300,
    "maxRetries": 3
  }
}
```

参考：https://www.volcengine.com/docs/82379

### 硅基流动 SiliconFlow

```json
{
  "ai": {
    "provider": "openai-compatible",
    "model": "provider-model-id",
    "apiKey": "your-siliconflow-api-key",
    "baseURL": "https://api.siliconflow.cn/v1",
    "maxTokens": 8192,
    "temperature": 1.0,
    "timeoutSeconds": 300,
    "maxRetries": 3
  }
}
```

参考：https://docs.siliconflow.cn/

### 百度千帆

```json
{
  "ai": {
    "provider": "openai-compatible",
    "model": "ernie-4.0-turbo-8k",
    "apiKey": "your-qianfan-api-key",
    "baseURL": "https://qianfan.baidubce.com/v2",
    "maxTokens": 8192,
    "temperature": 1.0,
    "timeoutSeconds": 300,
    "maxRetries": 3
  }
}
```

参考：https://cloud.baidu.com/doc/WENXINWORKSHOP/index.html

### 国内网关 / 代理 / 私有化部署

如果你使用的是公司内部网关、One API、New API、FastGPT、LiteLLM 国内部署版等，只要它暴露 OpenAI-compatible Chat Completions，就按同样格式配置：

```json
{
  "ai": {
    "provider": "openai-compatible",
    "model": "gateway-model-name",
    "apiKey": "gateway-api-key",
    "baseURL": "https://your-cn-gateway.example.com/v1",
    "maxTokens": 8192,
    "temperature": 1.0,
    "timeoutSeconds": 300,
    "maxRetries": 3
  }
}
```

### 404 排查

- 返回 HTML `404 Not Found` / `nginx` 通常说明域名通了，但 API 路径错了。
- 国内 OpenAI-compatible 平台优先填写 API 根地址，例如 `https://api.deepseek.com`、`https://api.minimax.chat/v1`、`https://dashscope.aliyuncs.com/compatible-mode/v1`。
- T-REX 会自动给没有路径的 API 根地址补 `/v1`，并调用 `/chat/completions`。
- 如果你的国内网关要求完整 endpoint，可以直接把 `baseURL` 写成 `https://your-cn-gateway.example.com/v1/chat/completions`，T-REX 不会重复追加。
- `apiKey` 和 `baseURL` 必须是普通 JSON 字符串，不要粘贴 Markdown 链接。

## 开发验证

```bash
go test ./...
go build -buildvcs=false ./...
```

## License

MIT
