package mdbgo

import (
	"os"
	"testing"
)

// TestExportFormContentsEfcAI0825 验证包含较早内嵌 CFB 签名的真实数据库仍能导出全部窗体。
func TestExportFormContentsEfcAI0825(t *testing.T) {
	const dbPath = "testdb/mdbs/efc-ai-0825.mdb"
	if _, err := os.Stat(dbPath); err != nil {
		t.Skipf("测试数据库不存在: %s, err=%v", dbPath, err)
	}

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	defer func() { _ = db.Close() }()

	contents, err := db.ExportFormContents()
	if err != nil {
		t.Fatalf("导出窗体失败: %v", err)
	}
	if len(contents) == 0 {
		t.Fatal("没有导出任何窗体")
	}
}
