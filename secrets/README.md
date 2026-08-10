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
