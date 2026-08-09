//go:build integration
// +build integration

// Package data integration tests for growth workspace lease.
//
// 启动方式（需 docker MySQL）：
//
//	docker run --rm -d --name sath_lease_it \
//	  -e MYSQL_ROOT_PASSWORD=root -p 3308:3306 mysql:8.0
//	# 等待启动，然后建库执行 migrations
//	mysql -h 127.0.0.1 -P 3308 -uroot -proot -e "CREATE DATABASE sath_it;"
//	mysql -h 127.0.0.1 -P 3308 -uroot -proot sath_it < portal/scripts/init_mysql.sql
//	mysql -h 127.0.0.1 -P 3308 -uroot -proot sath_it < portal/migrations/001_create_chat_growth_states.sql
//	mysql -h 127.0.0.1 -P 3308 -uroot -proot sath_it < portal/migrations/002_create_growth_workspace_leases.sql
//	mysql -h 127.0.0.1 -P 3308 -uroot -proot sath_it < portal/migrations/003_add_growth_retry_count.sql
//
//	export SATH_IT_MYSQL_DSN="root:root@tcp(127.0.0.1:3308)/sath_it?parseTime=True&loc=Local&charset=utf8mb4"
//	go test -tags=integration ./internal/data/... -run TestGrowthLease -v
//
// 未设置 SATH_IT_MYSQL_DSN 时整个测试 t.Skip，避免误跑。
package data

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"backend/internal/data/model"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func mustOpenIT(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("SATH_IT_MYSQL_DSN")
	if dsn == "" {
		t.Skip("SATH_IT_MYSQL_DSN not set; skipping growth lease integration test")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open mysql: %v", err)
	}
	// 防止旧数据干扰：每次清空 leases。
	if err := db.Exec("TRUNCATE TABLE growth_workspace_leases").Error; err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return db
}

func TestGrowthLease_AcquireReleaseSameHolder(t *testing.T) {
	db := mustOpenIT(t)
	repo := &growthRepo{db: db}
	ctx := context.Background()
	ws := "/tmp/it-ws-1"
	ttl := 5 * time.Second

	ok, err := repo.TryAcquireLease(ctx, ws, "h1", ttl)
	if err != nil || !ok {
		t.Fatalf("first acquire: ok=%v err=%v", ok, err)
	}
	// Same holder may renew.
	ok, err = repo.TryAcquireLease(ctx, ws, "h1", ttl)
	if err != nil || !ok {
		t.Fatalf("renew: ok=%v err=%v", ok, err)
	}
	// Different holder rejected while not expired.
	ok, _ = repo.TryAcquireLease(ctx, ws, "h2", ttl)
	if ok {
		t.Fatal("h2 should be rejected while h1 holds lease")
	}
	if err := repo.ReleaseLease(ctx, ws, "h1"); err != nil {
		t.Fatalf("release: %v", err)
	}
	// After release, h2 takes it.
	ok, err = repo.TryAcquireLease(ctx, ws, "h2", ttl)
	if err != nil || !ok {
		t.Fatalf("h2 after release: ok=%v err=%v", ok, err)
	}
}

func TestGrowthLease_ExpiredLeaseTakeover(t *testing.T) {
	db := mustOpenIT(t)
	repo := &growthRepo{db: db}
	ctx := context.Background()
	ws := "/tmp/it-ws-2"

	ok, err := repo.TryAcquireLease(ctx, ws, "h1", 1*time.Second)
	if err != nil || !ok {
		t.Fatalf("first acquire: %v", err)
	}
	// Wait past TTL.
	time.Sleep(1500 * time.Millisecond)
	ok, err = repo.TryAcquireLease(ctx, ws, "h2", 5*time.Second)
	if err != nil || !ok {
		t.Fatal("expired lease should be takeable by another holder")
	}
	var row model.GrowthWorkspaceLease
	if err := db.Where("workspace_key = ?", ws).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.HolderID != "h2" {
		t.Fatalf("holder=%q want h2", row.HolderID)
	}
}

func TestGrowthLease_ConcurrentSingleWinner(t *testing.T) {
	db := mustOpenIT(t)
	repo := &growthRepo{db: db}
	ws := "/tmp/it-ws-3"
	const goroutines = 16
	var wg sync.WaitGroup
	var winners int64
	var mu sync.Mutex
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		holderID := "concurrent-" + (string(rune('A' + i)))
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			ok, err := repo.TryAcquireLease(ctx, ws, holderID, 5*time.Second)
			if err != nil {
				return
			}
			if ok {
				mu.Lock()
				winners++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if winners != 1 {
		t.Fatalf("concurrent acquire winners=%d want 1", winners)
	}
}
