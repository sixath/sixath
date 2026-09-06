# WSL 一键部署使用文档

面向 **Windows 公司环境（禁用 Docker Desktop）**：在 WSL2 Ubuntu 内安装 Docker Engine，一键拉起 MySQL + Portal + Gateway + Web。

相关脚本：

| 脚本 | 作用 |
|------|------|
| `deploy/install-docker-wsl.sh` | 在 Ubuntu 内安装 Docker Engine + Compose 插件（首次一次） |
| `deploy/deploy-wsl.sh` | WSL 内一键部署（推荐入口） |
| `deploy/deploy-wsl.ps1` | 从 Windows PowerShell 唤起 WSL 部署 |
| `deploy/wsl-up.cmd` | 资源管理器双击起栈（等价 `-Build`） |
| `deploy/repair-wsl.ps1` | 修复 WSL `E_UNEXPECTED` 等启动故障（管理员） |
| `deploy/expose-wsl-ports.ps1` | 局域网入站：portproxy + 防火墙（管理员） |
| `deploy/win10-network.ps1` | Win10 网络一键：portproxy/防火墙 + 出站代理说明（管理员） |
| `deploy/deploy.sh` | 通用 Compose 部署（被 `deploy-wsl.sh` 调用） |

密钥说明见 [`secrets/README.md`](../secrets/README.md)。整体 Compose 设计见 [`docs/superpowers/specs/2026-08-10-docker-compose-prod-design.md`](../docs/superpowers/specs/2026-08-10-docker-compose-prod-design.md)。

---

## 1. 前置条件

1. **Windows 10/11**，已启用 WSL2  
2. 已安装发行版（推荐 Ubuntu）：
   ```powershell
   wsl --install -d Ubuntu
   ```
   装完后**重启**，再执行 `wsl -d Ubuntu` 完成用户名/密码创建。  
3. **不需要** Docker Desktop  
4. 磁盘：镜像构建约需数 GB；Docker 数据默认在 WSL 虚拟盘（通常落在 C 盘的 `ext4.vhdx`）  
5. 网络：能访问 Docker Hub / Ubuntu apt（或已配置公司镜像）

验证 WSL：

```powershell
wsl -l -v
wsl -d Ubuntu
```

若出现「灾难性故障 / `Wsl/Service/E_UNEXPECTED`」，见 [§6.1](#61-wsl-灾难性故障-e_unexpected)。

---

## 2. 首次安装 Docker Engine（只需一次）

在 **Ubuntu** 终端：

```bash
# 进入仓库（Windows E 盘示例）
cd /mnt/e/workspace/github/sixath/sixath

bash deploy/install-docker-wsl.sh
```

脚本会：

- 安装 `docker-ce`、Compose 插件  
- 把当前用户加入 `docker` 组  
- 尽量通过 systemd / `dockerd` 拉起守护进程  

然后 **关掉 Ubuntu 窗口再开一次**（让 `docker` 组生效）。本发行版若无 `newgrp`，请用：

```powershell
wsl --shutdown
wsl -d Ubuntu
```

验证：

```bash
docker ps
docker compose version
```

临时也可用：`sudo docker ps` 或 `sg docker -c 'docker ps'`。

---

## 3. 一键部署

### 3.1 在 Ubuntu 内（推荐）

```bash
cd /mnt/e/workspace/github/sixath/sixath
./deploy/deploy-wsl.sh --build
```

### 3.2 从 Windows PowerShell

```powershell
cd E:\workspace\github\sixath\sixath
.\deploy\deploy-wsl.ps1 -Build -Distro Ubuntu
```

### 3.3 双击

运行 `deploy\wsl-up.cmd`。

### 脚本会自动做的事

1. 检查 / 尝试启动 Docker 守护进程  
2. 去掉 shell 脚本 CRLF（NTFS 仓库常见）  
3. 若仓库在 `/mnt/*`，把 `PORTAL_DATA_DIR` 指到 `~/sixath-data/portal`（Linux 盘，skills 更稳）  
4. 补齐 `.env`、`secrets/*.txt`（并从 example 去掉 CR）  
5. `docker compose up`（可选 `--build`）→ 等待 healthy → smoke  

### 常用参数

| Ubuntu | PowerShell | 含义 |
|--------|------------|------|
| `--build` | `-Build` | 构建镜像后启动 |
| `--with-neo4j` | `-WithNeo4j` | 启用 Neo4j profile |
| `--with-tls` | `-WithTls` | 启用 Caddy TLS（需 `.env` 中真实 `DOMAIN`） |
| `--down` | `-Down` | 停栈（保留卷） |
| `--smoke-only` | `-SmokeOnly` | 仅健康检查 |

示例：

```bash
./deploy/deploy-wsl.sh --build --with-neo4j
./deploy/deploy-wsl.sh --down
./deploy/deploy-wsl.sh --smoke-only
```

---

## 4. 访问与登录

默认端口（可用 `.env` 覆盖）：

| 服务 | 地址 |
|------|------|
| Web UI | http://127.0.0.1:18080 |
| Gateway | http://127.0.0.1:18088 |
| Portal HTTP | http://127.0.0.1:18000 |
| Portal gRPC | localhost:19000 |
| MySQL | localhost:13306 |

登录：

- 邮箱：`.env` 中 `BOOTSTRAP_ADMIN_EMAIL`（默认 `admin@example.com`）  
- 密码：`secrets/bootstrap_password.txt`  

生产前务必改掉所有 `secrets/*.txt` 示例值。

---

## 5. 数据与路径说明

| 数据 | 位置 | 说明 |
|------|------|------|
| 源码 | 仓库目录（可为 `/mnt/e/...`） | 仅代码 |
| Docker 镜像 / 构建缓存 | WSL Ubuntu 虚拟盘 | 默认占 C 盘 vhdx |
| MySQL | Compose 卷 `sixath_mysql_data` | `docker compose down -v` 会清空 |
| Agent skills / workspace | `PORTAL_DATA_DIR`（默认 `~/sixath-data/portal`） | 容器重建不丢 skill |

更推荐把仓库 clone 到 Linux 文件系统后再部署，例如：

```bash
mkdir -p ~/src && cd ~/src
git clone <repo-url> sixath
cd sixath
./deploy/deploy-wsl.sh --build
```

---

## 6. Win10 网络（继续用 Win10，无 mirrored）

Win10 **不能** 用 `networkingMode=mirrored`（需 Win11）。WSL/容器在 NAT 后面，和 Windows「同一网卡」做不到，用下面两段补齐。

| 方向 | 做法 |
|------|------|
| **入站**（别的电脑访问 Web） | `portproxy` + 防火墙：Windows LAN IP → WSL eth0 |
| **出站**（容器访问公司内网如 `10.19.x`） | Windows 上跑 TCP 代理；容器访问 `http://<vEthernet-WSL-IP>:<端口>` |

### 6.1 一键刷新入站（管理员 PowerShell）

```powershell
cd E:\workspace\github\sixath\sixath
powershell -ExecutionPolicy Bypass -File .\deploy\win10-network.ps1
# 或仅入站：
powershell -ExecutionPolicy Bypass -File .\deploy\expose-wsl-ports.ps1
```

然后其它机器打开：`http://<Windows局域网IP>:18080`（例：`http://10.86.32.78:18080`）。

**注意：** `wsl --shutdown` / 重启后 WSL eth0 IP 会变，需再跑一次。

建议 `%USERPROFILE%\.wslconfig` 保持：

```ini
[wsl2]
vmIdleTimeout=-1
```

（避免 WSL 空闲休眠导致 portproxy 空转。）

### 6.2 出站：公司内网（例 ES `10.19.240.122:29200`）

1. 在 **Windows** 起用户态转发（示例脚本 `C:\Users\Admin\tools\run-es-proxy.ps1`）：本机 `0.0.0.0:29200` → `10.19.240.122:29200`  
2. 查 Windows 侧 WSL 网关（常见 `192.168.176.1`）：

```powershell
Get-NetIPAddress -AddressFamily IPv4 | Where-Object { $_.InterfaceAlias -like '*WSL*' }
```

3. **容器 / Agent 工具**里不要直连 `10.19.x`，改用：

```text
http://192.168.176.1:29200
```

（以你机器上 vEthernet (WSL) 的实际 IP 为准。）

### 6.3 Win11 可选

公司策略允许时可改用 mirrored（本仓库 Win10 环境不要开）：

```ini
[wsl2]
networkingMode=mirrored
dnsTunneling=true
```

---

## 7. 运维命令

```bash
cd /mnt/e/workspace/github/sixath/sixath   # 按实际路径

docker compose ps
docker compose logs -f portal
docker compose logs -f gateway
docker compose logs -f web

./deploy/deploy-wsl.sh --smoke-only
./deploy/deploy-wsl.sh --down          # 停服务，保留 MySQL / skills
docker compose down -v                 # 连 MySQL 卷一起删（危险）
```

改配置 / secrets 后通常：

```bash
./deploy/deploy-wsl.sh --build
```

---

## 8. 排障

### 7.1 WSL 灾难性故障 `E_UNEXPECTED`

管理员 PowerShell：

```powershell
cd E:\workspace\github\sixath\sixath
powershell -ExecutionPolicy Bypass -File .\deploy\repair-wsl.ps1
```

或手动：

```powershell
dism.exe /online /enable-feature /featurename:Microsoft-Windows-Subsystem-Linux /all /norestart
dism.exe /online /enable-feature /featurename:VirtualMachinePlatform /all /norestart
wsl --update
wsl --shutdown
wsl -d Ubuntu
```

仍失败可重装发行版（会清空 Ubuntu 内数据，不影响 E 盘仓库）：

```powershell
wsl --unregister Ubuntu
wsl --install -d Ubuntu
```

### 7.2 `docker: permission denied ... docker.sock`

用户组未生效：

```powershell
wsl --shutdown
wsl -d Ubuntu
docker ps
```

或：`sudo docker ps` / `sg docker -c 'docker ps'`。

### 7.3 `set: pipefail` / 脚本乱码报错

CRLF 问题。用最新脚本重跑，或：

```bash
tr -d '\r' < deploy/deploy-wsl.sh | bash -s -- --build
```

### 7.4 `.env: compose.neo4j.yml: command not found`

`COMPOSE_FILE` 含 `|` 被 bash `source` 当成管道。请使用已引用的 `.env.example`（值带双引号），并确保走 `deploy-wsl.sh` / 新版 `deploy.sh`（安全加载 `.env`）。

### 7.5 Portal：`Access denied for user 'root'@'...'`

多为 `secrets/mysql_root_password.txt` 含 `\r`，MySQL 首次初始化密码与 Portal 不一致。

```bash
for f in secrets/*.txt; do printf '%s' "$(tr -d '\r\n' < "$f")" > "$f"; done
docker compose down
docker volume rm sixath_mysql_data    # 会丢库数据
./deploy/deploy-wsl.sh --build
```

详见 [`secrets/README.md`](../secrets/README.md)。

### 7.6 `dockerd did not become ready`

```bash
sudo systemctl start docker
# 或
sudo service docker start
docker ps
```

确认 `/etc/wsl.conf` 含：

```ini
[boot]
systemd=true
```

然后从 Windows：`wsl --shutdown` 再进 Ubuntu。

---

### 8.8 stdio MCP（npx）跨平台

Command 推荐填 `npx`（允许列表：`npx` / `node` / `npm`）。

| 环境 | 说明 |
|------|------|
| Windows 本机 Portal | 自动解析 `npx.cmd`；也可设 `SATH_MCP_STDIO_NPX=C:\Program Files\nodejs\npx.cmd` |
| macOS / Linux 本机 | 依赖 PATH 或 `/usr/local/bin`、Homebrew 路径探测 |
| Docker Compose Portal | 镜像已含 Node 20，`npx` 可直接用；改 Dockerfile 后需 `--build` 重建 |

```bash
# 重建 portal 镜像后验证
docker compose exec portal npx -v
```

---

## 9. 推荐流程（清单）

- [ ] `wsl -d Ubuntu` 可正常进入  
- [ ] `bash deploy/install-docker-wsl.sh` 成功  
- [ ] 新开终端后 `docker ps` 无 permission 错误  
- [ ] `./deploy/deploy-wsl.sh --build` smoke 通过  
- [ ] 浏览器打开 http://127.0.0.1:18080 可登录  
- [ ] 生产前已修改 `secrets/*.txt` 与 `BOOTSTRAP_ADMIN_EMAIL`  
