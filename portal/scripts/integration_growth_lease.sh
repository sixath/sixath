#!/usr/bin/env bash
# 启动一次性 docker MySQL，自动建库 + 执行 growth phase2 迁移，导出 DSN 后执行集成测试。
# 使用：
#   bash portal/scripts/integration_growth_lease.sh
#
# 退出时容器保留以便排查，可手动 `docker rm -f sath_lease_it` 清理。

set -euo pipefail

CONTAINER=${SATH_IT_CONTAINER:-sath_lease_it}
PORT=${SATH_IT_PORT:-3308}
ROOT_PWD=${SATH_IT_ROOT_PWD:-root}
DB=${SATH_IT_DB:-sath_it}

if ! docker ps --format '{{.Names}}' | grep -q "^${CONTAINER}$"; then
  echo "starting docker mysql container ${CONTAINER} on :${PORT}..."
  docker run --rm -d --name "${CONTAINER}" \
    -e "MYSQL_ROOT_PASSWORD=${ROOT_PWD}" \
    -p "${PORT}:3306" \
    mysql:8.0 >/dev/null

  echo "waiting for mysql to accept connections..."
  for i in $(seq 1 60); do
    if docker exec "${CONTAINER}" mysqladmin ping -uroot -p"${ROOT_PWD}" >/dev/null 2>&1; then
      break
    fi
    sleep 1
  done
fi

echo "applying schema..."
docker exec -i "${CONTAINER}" mysql -uroot -p"${ROOT_PWD}" \
  -e "CREATE DATABASE IF NOT EXISTS ${DB} DEFAULT CHARSET=utf8mb4;"
docker exec -i "${CONTAINER}" mysql -uroot -p"${ROOT_PWD}" "${DB}" < portal/scripts/init_mysql.sql 2>/dev/null || true
for sql in 001_create_chat_growth_states.sql 002_create_growth_workspace_leases.sql 003_add_growth_retry_count.sql; do
  echo "  applying ${sql}"
  docker exec -i "${CONTAINER}" mysql -uroot -p"${ROOT_PWD}" "${DB}" < "portal/migrations/${sql}" || true
done

export SATH_IT_MYSQL_DSN="root:${ROOT_PWD}@tcp(127.0.0.1:${PORT})/${DB}?parseTime=True&loc=Local&charset=utf8mb4"
echo "running integration tests..."
cd portal && go test -tags=integration ./internal/data/... -run TestGrowthLease -v
