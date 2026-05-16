# paseo-relay

[Paseo](https://github.com/getpaseo/paseo) 的独立 Go 中继服务器，完全兼容原版 Paseo 中继协议（v1 和 v2）。

中继在 Paseo 守护进程（运行在你的机器上）和移动端/桌面端客户端之间桥接 WebSocket 连接。它只转发加密流量，不检查内容——所有端到端加密由守护进程和客户端处理。

## 为什么

官方 Paseo 中继运行在 Cloudflare Workers 上。这个项目让你可以在自己的基础设施上自建中继，不再依赖 Cloudflare。

## 特性

- 完全兼容原版中继的 v1 和 v2 协议
- 多路复用连接——每个会话支持多个客户端
- 帧缓冲（200 帧），支持守护进程延迟加入
- 两阶段 nudge/reset 无响应守护进程检测
- TLS 证书通过 SIGHUP 热重载（零停机证书轮换）
- 优雅关机，10 秒超时
- 空闲会话自动清理（5 分钟后回收）
- 除 WebSocket 和 YAML 库外无其他外部依赖
- 单个静态二进制文件，易于部署

## 安装

```bash
go build -o paseo-relay .
```

或使用 Make：

```bash
make build
```

### Docker

```bash
docker build -t paseo-relay .
docker run -p 8080:8080 paseo-relay

# 启用 TLS
docker run -p 443:8080 \
  -v /path/to/certs:/certs:ro \
  paseo-relay \
  --tls-cert /certs/cert.pem --tls-key /certs/key.pem
```

## 使用

```bash
# 默认端口 8080 启动
./paseo-relay

# 自定义监听地址
./paseo-relay --listen 0.0.0.0:9090

# 启用 TLS
./paseo-relay --tls-cert /path/to/cert.pem --tls-key /path/to/key.pem

# 使用配置文件
./paseo-relay --config /path/to/config.yaml  # 参见 config.example.yaml
```

### 配置

配置来源（优先级：命令行参数 > 环境变量 > 配置文件 > 默认值）：

| 参数 | 环境变量 | 默认值 | 说明 |
|---|---|---|---|
| `--listen` | `PASEO_LISTEN` | `0.0.0.0:8080` | 监听地址 |
| `--tls-cert` | `PASEO_TLS_CERT` | — | TLS 证书路径 |
| `--tls-key` | `PASEO_TLS_KEY` | — | TLS 私钥路径 |
| `--log-level` | `PASEO_LOG_LEVEL` | `info` | 日志级别：debug、info、warn、error |
| `--config` | — | — | 配置文件路径 |

完整注释示例见 [config.example.yaml](config.example.yaml)。

配置文件示例：

```yaml
listen: "0.0.0.0:8080"
log_level: "info"
tls:
  enabled: false
  cert: ""
  key: ""
```

### TLS 证书热重载

发送 `SIGHUP` 信号即可不重启重新加载证书：

```bash
kill -HUP $(pgrep paseo-relay)
```

配合 certbot 使用：`certbot renew --deploy-hook "kill -HUP $(cat /run/paseo-relay.pid)"`

## 协议

| 端点 | 说明 |
|---|---|
| `GET /health` | 健康检查，返回 `{"status":"ok"}` |
| `GET /ws` | WebSocket 升级端点 |

### WebSocket 参数

| 参数 | 必填 | 说明 |
|---|---|---|
| `serverId` | 是 | 标识守护进程会话 |
| `role` | 是 | `server` 或 `client` |
| `v` | 否 | 协议版本：`1` 或 `2`（默认 `1`） |
| `connectionId` | 否 | 每个客户端的路由 ID（v2 数据通道必填） |

### v2 连接流程

```
守护进程                        中继                          客户端
  │                               │                              │
  │  WS /ws?role=server&v=2       │                              │
  │  (控制通道)                    │                              │
  │──────────────────────────────►│                              │
  │  ◄── {type:"sync",...}        │                              │
  │                               │  WS /ws?role=client&v=2      │
  │                               │◄─────────────────────────────│
  │  ◄── {type:"connected",...}   │                              │
  │                               │                              │
  │  WS /ws?role=server&          │                              │
  │  connectionId=abc&v=2         │                              │
  │──────────────────────────────►│                              │
  │                               │  (E2EE 握手通过中继进行      │
  │                               │   中继无法读取内容)           │
  │  ◄───── 加密数据 ────────────►│◄───── 加密数据 ────────────►│
```

## 配置 Paseo 守护进程使用你的中继

编辑运行 Paseo 守护进程的机器上的 `~/.paseo/config.json`：

```json
{
  "version": 1,
  "daemon": {
    "relay": {
      "enabled": true,
      "endpoint": "your-relay.example.com:443",
      "publicEndpoint": "your-relay.example.com:443",
      "useTls": true
    }
  }
}
```

也可以通过环境变量设置（优先级高于配置文件）：

```bash
export PASEO_RELAY_ENDPOINT="your-relay.example.com:443"
export PASEO_RELAY_PUBLIC_ENDPOINT="your-relay.example.com:443"
export PASEO_RELAY_USE_TLS=true
```

- `endpoint` — 守护进程连接中继的地址（**只填 host:port，不要加 `https://` 前缀**）
- `publicEndpoint` — 嵌入配对二维码/链接中的地址，格式规则同上。如果守护进程和客户端通过不同地址访问中继（例如内网 IP vs 公网域名），则需要单独设置此项
- `useTls` — 如果中继前面有 TLS 反向代理（Nginx、Caddy 等），**必须设为 `true`**。守护进程对非官方端点默认 `false`，会导致明文请求发到 HTTPS 端口而报 400 错误

修改后需要重启守护进程。

## 反向代理（Nginx 示例）

中继本身使用纯 HTTP 的 WebSocket 协议。生产环境中，建议在前面放一个反向代理来处理 TLS 并设置足够长的超时——WebSocket 连接是长连接。

```nginx
server {
    listen 443 ssl;
    server_name your-relay.example.com;

    ssl_certificate     /path/to/cert.pem;
    ssl_certificate_key /path/to/key.pem;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # WebSocket 连接是长连接，使用较大的超时值
        proxy_read_timeout 3600s;
        proxy_send_timeout 3600s;
    }
}
```

## 上游同步

基于 [getpaseo/paseo](https://github.com/getpaseo/paseo) 中继服务端（`packages/relay/src/cloudflare-adapter.ts`）实现。

| 日期 | 上游 Commit | 备注 |
|---|---|---|
| 2026-05-13 | [`d24087c1`](https://github.com/getpaseo/paseo/commit/d24087c1) | 修复 relay E2EE 重连竞态；添加 legacy JSON ping 兼容日志 |

与最新上游对比：

```bash
git clone https://github.com/getpaseo/paseo.git /tmp/paseo
diff <(git show d24087c1:packages/relay/src/cloudflare-adapter.ts) /tmp/paseo/packages/relay/src/cloudflare-adapter.ts
```

## 开发

```bash
make build        # 编译二进制
make test         # 运行测试
make fmt          # 格式化代码
make vet          # 运行 go vet
make clean        # 清理二进制
```

## 贡献

参见 [CONTRIBUTING.md](CONTRIBUTING.md)。

## 许可证

[MIT](LICENSE)
