# Domain VPN Router

Domain VPN Router 是一个 Windows 本地域名分流代理。它在本机启动 HTTP/HTTPS 代理，接管系统代理设置，并按 `config.yaml` 的域名规则选择出口。

适合当前场景：
- 公司域名：GlobalProtect 已连接时走 GP 网卡；GP 未连接时走本地物理网卡。
- 外网域名：只转发到 Clash Verge 的本地 mixed-port。
- 其他域名：只走本地物理网卡直连。
- Clash Verge 和 GlobalProtect 可以同时打开，本工具不会在自动模式下互相关闭它们。

HTTPS 请求只读取 `CONNECT host:port` 里的域名和端口，不解密网页内容。

## 功能

- 默认以 Windows 托盘程序运行。
- 本地代理监听 `127.0.0.1:18080`。
- 启动时备份当前 Windows 系统代理，并切到本工具。
- 正常退出时自动恢复原系统代理。
- 支持异常退出后的手动代理恢复命令。
- 支持域名精确匹配和通配符后缀匹配，例如 `openai.com`、`*.openai.com`。
- 直连规则优先级最高，公司域名优先于外网域名。
- 不保存、不处理 VPN 账号密码；GlobalProtect 登录仍由官方客户端完成。

## 工作原理

浏览器或应用访问 HTTPS 网站时，会向本地代理发送类似请求：

```text
CONNECT chatgpt.com:443 HTTP/1.1
```

工具读取目标域名并按规则分流：
- 命中 `rules.direct_domains`：绑定本地物理网卡直连。
- 命中 `rules.company_domains`：如果检测到 GlobalProtect 网卡 IP，则绑定 GP 网卡；否则绑定本地物理网卡直连。
- 命中 `rules.foreign_domains`：转发到 `proxy.foreign_proxy`，也就是 Clash Verge mixed-port。
- 未命中任何规则：绑定本地物理网卡直连。

## 安装

需要 Go 1.26 或更新版本。

```powershell
go build -ldflags="-H windowsgui" -o domain-vpn-router-gui.exe ./cmd/router
```

## 配置

复制示例配置：

```powershell
Copy-Item config.example.yaml config.yaml
```

关键配置示例：

```yaml
proxy:
  listen: 127.0.0.1:18080
  direct_bind_ip:
  foreign_proxy: 127.0.0.1:7897

vpn:
  clash_verge:
    exe: C:\Program Files\Clash Verge\Clash Verge.exe
    process: Clash Verge
    adapter_keywords:
      - Mihomo
      - Meta Tunnel
      - Clash
  globalprotect:
    exe: C:\Program Files\Palo Alto Networks\GlobalProtect\PanGPA.exe
    process: PanGPA
    adapter_keywords:
      - PANGP
      - GlobalProtect

rules:
  company_domains:
    - company.example.com
    - "*.corp.example.com"
  foreign_domains:
    - github.com
    - "*.github.com"
  direct_domains:
    - localhost
    - "*.local"
```

`proxy.foreign_proxy` 必须填写 Clash Verge 的本地 mixed-port。端口以你的 Clash Verge 配置为准，常见值是 `127.0.0.1:7890`、`127.0.0.1:7897`。

## Clash Verge 与 GP 同时打开

如果 Clash Verge 开启 TUN、全局增强模式或 fake-ip，可能会影响 GlobalProtect 自身登录和握手。建议在 Clash Verge 规则里把以下流量设为 `DIRECT`：
- GlobalProtect Portal 域名。
- GlobalProtect Gateway 域名或 IP。
- `PanGPA.exe`、`PanGPS.exe`、`GlobalProtect.exe` 进程流量，如果 Clash Verge 支持进程规则。

本工具只负责浏览器/应用经过系统代理后的分流；GP 客户端自己的建连流量需要 Clash Verge 规则配合直连。

## 使用

启动：

```powershell
.\domain-vpn-router-gui.exe
```

异常退出后恢复系统代理：

```powershell
.\domain-vpn-router-gui.exe restore-proxy
```

手动测试代理：

```powershell
curl.exe -x http://127.0.0.1:18080 https://github.com
curl.exe -x http://127.0.0.1:18080 https://company.example.com
```

## 注意事项

- 真实公司域名、个人本机 IP、内部网段不要提交到公开仓库；`config.yaml` 默认应加入 `.gitignore`。
- 如果本应直连的流量被 GP 或 Clash 接管，请配置 `proxy.direct_bind_ip` 为本地物理网卡 IPv4。
- 旧配置里的 `vpn.tyty` 仍可被读取为兼容别名，但新配置请使用 `vpn.clash_verge`。