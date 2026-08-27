package mdbgo

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestReadLongMemoOrderByText 使用 DMS 真实 MDB 验证普通表扫描和 Query 都能读取完整的排序 Memo 长值。
func TestReadLongMemoOrderByText(t *testing.T) {
	// 该回归依赖包含 ABI Entry 查询元数据的真实 DMS MDB，未显式指定时不绑定默认测试库内容。
	dbPath := strings.TrimSpace(os.Getenv("MDBGO_TEST_DB"))
	if dbPath == "" {
		t.Skip("MDBGO_TEST_DB is not set")
	}

	// 只读打开真实 MDB，并在测试结束时释放底层数据库句柄。
	db, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	// 这些值覆盖短内联 Memo，以及此前会丢失的较长 ABI Entry 排序 Memo。
	wants := map[string]string{
		"v_doc_list_dis":             "t_doc.doc_id",
		"v_abi_entry_line":           "t_abi_entry.entry_no, t_abi_entry_line.line_no",
		"v_abi_entry_line_duty":      "t_abi_entry_line.line_no, t_abi_entry_line_duty.duty_seq_no",
		"v_abi_entry_6_filing_event": "t_log_event_io.mbl_id DESC , t_log_event_io.abi_entry_id, t_log_event_io.log_event_io_id",
	}

	// ReadTable 是批量导出 preloadAll 使用的路径，必须保留所有目标排序值。
	data, err := db.ReadTable("t_zzw_w03_query")
	if err != nil {
		t.Fatal(err)
	}
	readTableOrders := queryOrderValues(t, data)
	for queryName, want := range wants {
		if got := readTableOrders[queryName]; got != want {
			t.Errorf("ReadTable query=%s order_by_text=%q, want %q", queryName, got, want)
		}
	}

	// Query 是单窗体按需导出使用的路径，逐条查询可同时覆盖字段投影和 WHERE 过滤。
	for queryName, want := range wants {
		queryName := queryName
		want := want
		t.Run(queryName, func(t *testing.T) {
			sqlText := fmt.Sprintf("SELECT * FROM [t_zzw_w03_query] WHERE query_name = '%s'", strings.ReplaceAll(queryName, "'", "''"))
			queryData, queryErr := db.Query(sqlText)
			if queryErr != nil {
				t.Fatal(queryErr)
			}
			if got := queryOrderValues(t, queryData)[queryName]; got != want {
				t.Errorf("Query order_by_text=%q, want %q", got, want)
			}
		})
	}
}

// queryOrderValues 从 t_zzw_w03_query 结果中提取并规范化查询名与排序文本的对应关系。
func queryOrderValues(t *testing.T, data *TableData) map[string]string {
	t.Helper()
	if data == nil {
		t.Fatal("t_zzw_w03_query result is nil")
	}

	// 结果列名不区分大小写，索引初始化为 -1 以区分首列和缺失列。
	queryNameIndex := -1
	orderByIndex := -1
	for index, column := range data.Columns {
		switch {
		case strings.EqualFold(column, "query_name"):
			queryNameIndex = index
		case strings.EqualFold(column, "order_by_text"):
			orderByIndex = index
		}
	}
	if queryNameIndex < 0 || orderByIndex < 0 {
		t.Fatalf("columns=%v, want query_name and order_by_text", data.Columns)
	}

	// strings.Fields 去除 Access Memo 尾部换行，并保留字段顺序和 ASC/DESC 语义。
	orders := make(map[string]string)
	for _, row := range data.Rows {
		if queryNameIndex >= len(row) || orderByIndex >= len(row) {
			continue
		}
		orders[strings.TrimSpace(row[queryNameIndex])] = strings.Join(strings.Fields(row[orderByIndex]), " ")
	}
	return orders
}
