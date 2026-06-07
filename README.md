# Domain VPN Router

Domain VPN Router 是一个 Windows 本地域名分流代理。它在本机启动 HTTP/HTTPS 代理，临时接管系统代理设置，并按目标域名自动选择当前应使用的 VPN 客户端。

适合这种场景：

- 访问国外网站时启动并使用 Tyty。
- 访问公司内网站时启动并使用 GlobalProtect。
- 其他网站保持本地直连。
- Tyty 和 GlobalProtect 不适合同时开启，需要按访问目标切换当前网络模式。

HTTPS 请求只读取 `CONNECT host:port` 里的域名和端口，不解密网页内容。

## 功能

- 本地代理监听 `127.0.0.1:18080`。
- 启动时备份当前 Windows 系统代理，并把系统代理切到本工具。
- 正常退出时自动恢复原系统代理。
- 支持异常退出后的手动代理恢复命令。
- 支持域名精确匹配和通配符后缀匹配，例如 `openai.com`、`*.openai.com`。
- 公司域名优先于国外域名，直连规则优先级最高。
- 不保存、不处理 VPN 账号密码；登录仍交给官方客户端。

## 工作原理

浏览器或应用访问 HTTPS 网站时，会向本地代理发送类似下面的请求：

```text
CONNECT chatgpt.com:443 HTTP/1.1
```

工具读取目标域名 `chatgpt.com`，然后按 `config.yaml` 判断：

- 命中 `rules.company_domains`：启动 GlobalProtect，并等待 PANGP 网卡可用。
- 命中 `rules.foreign_domains`：启动 Tyty，并等待 Mihomo/Meta Tunnel 网卡可用。
- 命中 `rules.direct_domains` 或没有命中任何规则：本地直连。

## 安装

需要 Go 1.26 或更新版本。

```powershell
git clone https://github.com/<your-name>/domain-vpn-router.git
cd domain-vpn-router
go build -o domain-vpn-router.exe ./cmd/router
```

## 配置

复制示例配置：

```powershell
Copy-Item config.example.yaml config.yaml
```

然后修改 `config.yaml`：

```yaml
rules:
  company_domains:
    - internal.example.com
    - "*.corp.example.com"

  foreign_domains:
    - chatgpt.com
    - "*.chatgpt.com"
    - openai.com
    - "*.openai.com"
    - cursor.sh
    - "*.cursor.sh"
```

如果 Tyty 的 TUN 网卡会成为默认路由，可以设置本地直连绑定 IP：

```yaml
proxy:
  direct_bind_ip: 192.168.1.100
```

这个值应填写你的本地物理网卡 IPv4 地址；不确定时可以留空。

## 使用

启动：

```powershell
.\domain-vpn-router.exe
```

正常退出：

```text
Ctrl+C
```

异常退出后恢复系统代理：

```powershell
.\domain-vpn-router.exe restore-proxy
```

手动测试代理：

```powershell
curl.exe -x http://127.0.0.1:18080 https://github.com
curl.exe -x http://127.0.0.1:18080 https://company.example.com
```

运行窗口会输出类似日志：

```text
访问目标=chatgpt.com:443 动作=foreign 规则=chatgpt.com
访问目标=internal.example.com:443 动作=company 规则=internal.example.com
访问目标=example.cn:443 动作=direct 规则=default
```

## Tyty 规则导入

如果 Tyty 使用 mihomo/Clash.Meta 内核，可以从运行时规则 API 提取 `proxy=Tyty` 的域名规则，再复制到 `foreign_domains`。

Tyty 常见命名管道地址：

```text
\\.\pipe\MihomoParty\mihomo
```

当前项目不会自动修改 Tyty 内部配置，只读取本工具自己的 `config.yaml`。

## 注意事项

- 本工具适合“按访问目标切换当前 VPN”的场景，不是同时连接两个 VPN 的并行分流器。
- 不建议把真实公司域名、个人本机 IP、内部网段提交到公开仓库；`config.yaml` 默认已加入 `.gitignore`。
- 如果系统代理没有恢复，运行 `restore-proxy` 命令。
