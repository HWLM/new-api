//go:build ignore

// 手动修补一条卡住的任务：把 status/progress/finish_time/data/private_data.result_url
// 更新成上游实际状态（我们前面已经确认 mvt-30320f02283e464f 早已 completed）。
//
// 只影响 id=12 且当前 status='NOT_START'（CAS 保护，防止和轮询节点冲突）。
package main

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const (
	taskID         = int64(12)
	upstreamURL    = "https://model.service-inference.ai/v1/video/files/mvt-30320f02283e464f"
	completedAtUTC = "2026-07-20T09:47:39.923Z" // 从上游拿到的 completed_at
	upstreamBody   = `{"task":{"id":"mvt-30320f02283e464f","status":"completed","model":"dreamina-seedance-2-0-hc","duration_seconds":4,"outputs":["https://model.service-inference.ai/v1/video/files/mvt-30320f02283e464f"],"error":null,"created_at":"2026-07-20T09:41:10.273Z","completed_at":"2026-07-20T09:47:39.923Z","usage":{"completion_tokens":196425,"total_tokens":196425}}}`
)

func loadDSN() string {
	f, err := os.Open(".env")
	if err != nil {
		log.Fatalf("open .env: %v", err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		k, v, _ := strings.Cut(line, "=")
		if strings.TrimSpace(k) == "SQL_DSN" {
			return strings.TrimSpace(v)
		}
	}
	log.Fatal("SQL_DSN not found in .env")
	return ""
}

func main() {
	dsn := loadDSN()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. 读取当前 private_data
	var privDataRaw string
	var currentStatus string
	err = db.QueryRowContext(ctx,
		`SELECT COALESCE(private_data::text,''), status FROM tasks WHERE id=$1`, taskID).
		Scan(&privDataRaw, &currentStatus)
	if err != nil {
		log.Fatalf("read task %d: %v", taskID, err)
	}
	fmt.Printf("before: status=%s\n", currentStatus)
	fmt.Printf("before: private_data=%s\n", privDataRaw)

	if currentStatus != "NOT_START" {
		log.Fatalf("task %d 当前 status=%s，不是 NOT_START，取消操作以防覆盖其他并发更新", taskID, currentStatus)
	}

	// 2. 合并 result_url
	var priv map[string]any
	if err := json.Unmarshal([]byte(privDataRaw), &priv); err != nil {
		log.Fatalf("parse private_data: %v", err)
	}
	priv["result_url"] = upstreamURL
	newPriv, err := json.Marshal(priv)
	if err != nil {
		log.Fatalf("marshal private_data: %v", err)
	}

	// 3. 完成时间：用上游 completed_at
	finishTime := time.Now().Unix()
	if t, err := time.Parse(time.RFC3339, completedAtUTC); err == nil {
		finishTime = t.Unix()
	}

	// 4. CAS 更新：只有当前还是 NOT_START 才更新
	res, err := db.ExecContext(ctx, `
		UPDATE tasks
		   SET status = 'SUCCESS',
		       progress = '100%',
		       finish_time = $2,
		       start_time = CASE WHEN start_time = 0 THEN submit_time ELSE start_time END,
		       updated_at = $3,
		       private_data = $4,
		       data = $5
		 WHERE id = $1 AND status = 'NOT_START'`,
		taskID, finishTime, time.Now().Unix(), string(newPriv), upstreamBody)
	if err != nil {
		log.Fatalf("update task: %v", err)
	}
	affected, _ := res.RowsAffected()
	fmt.Printf("rows_affected=%d\n", affected)
	if affected == 0 {
		log.Fatal("没更新到行（可能被并发改过）")
	}

	// 5. 回读确认
	var newStatus, newProgress, newPrivOut string
	var newFinish int64
	err = db.QueryRowContext(ctx,
		`SELECT status, progress, finish_time, COALESCE(private_data::text,'') FROM tasks WHERE id=$1`, taskID).
		Scan(&newStatus, &newProgress, &newFinish, &newPrivOut)
	if err != nil {
		log.Fatalf("re-read: %v", err)
	}
	fmt.Printf("after: status=%s progress=%s finish_time=%d (%s)\n",
		newStatus, newProgress, newFinish, time.Unix(newFinish, 0).Format("2006-01-02 15:04:05"))
	fmt.Printf("after: private_data=%s\n", newPrivOut)
}
