# Secrets for Docker Compose (file-based)

Copy each `*.txt.example` to the matching `*.txt` (deploy scripts do this automatically).

| File | Purpose |
|------|---------|
| `mysql_root_password.txt` | MySQL root + portal DSN password |
| `runtime_token.txt` | Gateway ↔ Portal Runtime token |
| `bootstrap_password.txt` | Admin login password (with `BOOTSTRAP_ADMIN_EMAIL`) |
| `bootstrap_token.txt` | ACL bootstrap API token |
| `neo4j_password.txt` | Neo4j auth (only with `--profile neo4j`) |

**Do not commit real `*.txt` files.** Replace example values before any shared/production deploy.

**Windows / WSL：** 文件必须是单行、无 `\r`。若在 Windows 下用记事本编辑，容易变成 `root\r\n`，MySQL 初始化会把 `\r` 当成密码的一部分，而 Portal 会去掉换行，于是出现 `Access denied for user 'root'@'...'`。部署脚本会自动去掉 CR/LF；手工修复：

```bash
for f in secrets/*.txt; do printf '%s' "$(tr -d '\r\n' < "$f")" > "$f"; done
```

若 MySQL 数据卷已用错误密码初始化过，需要清卷重建（会丢库内数据）：

```bash
docker compose down
docker volume rm sixath_mysql_data
./deploy/deploy-wsl.sh --build
```
