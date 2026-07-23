package mdbgo

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseAccessSQLFeatures(t *testing.T) {
	t.Parallel()
	sqlText := `
PARAMETERS [minimum] Long;
SELECT DISTINCT TOP 10 a.[abi_hbl_id] AS id,
       IIf(a.[house_no] Like "S*", UCase(a.[house_no]), "other") AS label,
       Count(*) AS matches
FROM ([t_abi_hbl] AS a INNER JOIN [t_abi_hbl] AS b
      ON a.[abi_hbl_id] = b.[abi_hbl_id])
WHERE a.[manifest_quantity] BETWEEN [minimum] AND 1000
GROUP BY a.[abi_hbl_id], a.[house_no]
HAVING Count(*) >= 1
ORDER BY id DESC;
`
	stmt, err := parseAccessSQL(sqlText)
	if err != nil {
		t.Fatalf("parseAccessSQL failed: %v", err)
	}
	if !stmt.distinct || stmt.top != 10 || len(stmt.joins) != 1 || len(stmt.group) != 2 || len(stmt.order) != 1 {
		t.Fatalf("unexpected parsed statement: %+v", stmt)
	}
	if _, ok := stmt.params["minimum"]; !ok {
		t.Fatalf("parameter not restored: %#v", stmt.params)
	}
}

func TestGoQueryEngineJoinAggregateAndParameters(t *testing.T) {
	dbPath := requireDBFile(t)
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	rows, err := db.QueryContext(context.Background(), `
SELECT a.[abi_hbl_id] AS id, UCase(a.[house_no]) AS house, Count(*) AS matches
FROM [t_abi_hbl] AS a
INNER JOIN [t_abi_hbl] AS b ON a.[abi_hbl_id] = b.[abi_hbl_id]
WHERE a.[manifest_quantity] >= [minimum]
GROUP BY a.[abi_hbl_id], a.[house_no]
HAVING Count(*) >= 1
ORDER BY id DESC
LIMIT 5`, map[string]any{"minimum": 1})
	if err != nil {
		t.Fatalf("QueryContext failed: %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatalf("expected at least one row: %v", rows.Err())
	}
	values := rows.Values()
	if len(values) != 3 || values[0].Kind != ValueInt || values[2].Int < 1 {
		t.Fatalf("unexpected values: %#v", values)
	}
}

func TestBatchedScannerPreservesNull(t *testing.T) {
	dbPath := requireDBFile(t)
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	rows, err := db.QueryContext(context.Background(),
		"SELECT [hold_date] FROM [t_abi_hbl] LIMIT 1", nil)
	if err != nil {
		t.Fatalf("QueryContext failed: %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("expected a fixture row")
	}
	values := rows.Values()
	if len(values) != 1 || values[0].Kind != ValueNull {
		t.Fatalf("expected database NULL, got %#v", values)
	}

	table, err := db.ReadTable("t_abi_hbl")
	if err != nil {
		t.Fatalf("ReadTable failed: %v", err)
	}
	nullColumn := -1
	for i, name := range table.Columns {
		if strings.EqualFold(name, "hold_date") {
			nullColumn = i
			break
		}
	}
	if nullColumn < 0 || len(table.Nulls) == 0 || !table.Nulls[0][nullColumn] {
		t.Fatalf("ReadTable did not preserve hold_date NULL")
	}
}

func TestQueryContextCancellation(t *testing.T) {
	dbPath := requireDBFile(t)
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := db.QueryContext(ctx, "SELECT * FROM [t_abi_hbl]", nil); err != context.Canceled {
		t.Fatalf("canceled query error=%v", err)
	}
}

func TestConcurrentQueriesUseIndependentHandles(t *testing.T) {
	dbPath := requireDBFile(t)
	db, err := OpenWithOptions(dbPath, OpenOptions{MaxConcurrentQueries: 4})
	if err != nil {
		t.Fatalf("OpenWithOptions failed: %v", err)
	}
	defer db.Close()

	first, releaseFirst, err := db.acquireQuerySession(context.Background())
	if err != nil {
		t.Fatalf("acquire first query session: %v", err)
	}
	defer releaseFirst()
	second, releaseSecond, err := db.acquireQuerySession(context.Background())
	if err != nil {
		t.Fatalf("acquire second query session: %v", err)
	}
	defer releaseSecond()
	if first.ptr == second.ptr {
		t.Fatal("concurrent query sessions unexpectedly share one mdbtools handle")
	}
}

func TestConcurrentQueriesSameDB(t *testing.T) {
	dbPath := requireDBFile(t)
	db, err := OpenWithOptions(dbPath, OpenOptions{MaxConcurrentQueries: 4})
	if err != nil {
		t.Fatalf("OpenWithOptions failed: %v", err)
	}
	defer db.Close()

	const workers = 12
	start := make(chan struct{})
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			<-start
			rows, err := db.QueryContext(context.Background(),
				"SELECT [abi_hbl_id], [house_no] FROM [t_abi_hbl] ORDER BY [abi_hbl_id] LIMIT 25", nil)
			if err != nil {
				errs <- fmt.Errorf("worker %d: %w", worker, err)
				return
			}
			defer rows.Close()
			values := rows.All()
			if len(values) == 0 {
				errs <- fmt.Errorf("worker %d: empty result", worker)
			}
		}(worker)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestConcurrentQueryPoolHonorsContext(t *testing.T) {
	dbPath := requireDBFile(t)
	db, err := OpenWithOptions(dbPath, OpenOptions{MaxConcurrentQueries: 1})
	if err != nil {
		t.Fatalf("OpenWithOptions failed: %v", err)
	}
	defer db.Close()

	_, release, err := db.acquireQuerySession(context.Background())
	if err != nil {
		t.Fatalf("occupy query session: %v", err)
	}
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = db.QueryContext(ctx, "SELECT TOP 1 * FROM [t_abi_hbl]", nil)
	if err != context.DeadlineExceeded {
		t.Fatalf("waiting query error=%v, want context deadline exceeded", err)
	}
}

func TestCloseWaitsForActiveQueryHandle(t *testing.T) {
	dbPath := requireDBFile(t)
	db, err := OpenWithOptions(dbPath, OpenOptions{MaxConcurrentQueries: 1})
	if err != nil {
		t.Fatalf("OpenWithOptions failed: %v", err)
	}
	_, release, err := db.acquireQuerySession(context.Background())
	if err != nil {
		t.Fatalf("acquire query session: %v", err)
	}

	closeDone := make(chan error, 1)
	go func() {
		closeDone <- db.Close()
	}()
	deadline := time.Now().Add(time.Second)
	for {
		db.stateMu.Lock()
		closed := db.closed
		db.stateMu.Unlock()
		if closed {
			break
		}
		if time.Now().After(deadline) {
			release()
			t.Fatal("Close did not mark DB closed")
		}
		runtime.Gosched()
	}
	select {
	case err := <-closeDone:
		release()
		t.Fatalf("Close returned before active query handle was released: %v", err)
	default:
	}

	release()
	if err := <-closeDone; err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if _, err := db.QueryContext(context.Background(), "SELECT TOP 1 * FROM [t_abi_hbl]", nil); err == nil {
		t.Fatal("query after Close unexpectedly succeeded")
	}
}

func TestGoQueryEngineSubqueries(t *testing.T) {
	dbPath := requireDBFile(t)
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	rows, err := db.QueryContext(context.Background(), `
SELECT a.[abi_hbl_id]
FROM [t_abi_hbl] AS a
WHERE a.[abi_hbl_id] IN (
    SELECT b.[abi_hbl_id] FROM [t_abi_hbl] AS b
    WHERE b.[manifest_quantity] >= 1
)
AND EXISTS (
    SELECT c.[abi_hbl_id] FROM [t_abi_hbl] AS c
    WHERE c.[abi_hbl_id] = a.[abi_hbl_id]
)
ORDER BY a.[abi_hbl_id]`, nil)
	if err != nil {
		t.Fatalf("subquery failed: %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatalf("subquery returned no rows: %v", rows.Err())
	}
}

func TestGoQueryEngineUnionAndFunctions(t *testing.T) {
	dbPath := requireDBFile(t)
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	rows, err := db.QueryContext(context.Background(), `
SELECT TOP 1 UCase([house_no]) AS value FROM [t_abi_hbl]
UNION ALL
SELECT TOP 1 Format([accepted_date], "yyyy-mm-dd") AS value FROM [t_abi_hbl]`, nil)
	if err != nil {
		t.Fatalf("UNION/function query failed: %v", err)
	}
	defer rows.Close()
	if got := len(rows.All()); got != 2 {
		t.Fatalf("UNION ALL rows=%d, want 2", got)
	}

	ordered, err := db.QueryContext(context.Background(), `
SELECT TOP 1 1 AS n FROM [t_abi_hbl]
UNION ALL
SELECT TOP 1 2 AS n FROM [t_abi_hbl] ORDER BY n DESC`, nil)
	if err != nil {
		t.Fatalf("ordered UNION failed: %v", err)
	}
	defer ordered.Close()
	values := ordered.All()
	if len(values) != 2 || values[0][0].Int != 2 || values[1][0].Int != 1 {
		t.Fatalf("ordered UNION values=%#v", values)
	}
}

func TestQueryViewContextExecutesSimpleSavedQuery(t *testing.T) {
	dbPath := requireDBFile(t)
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	const viewName = "~sq_cf_abia_master~sq_csub_abia_master_list_2_event"
	sqlText, err := db.ViewSQL(viewName)
	if err != nil {
		t.Skipf("saved query unavailable: %v", err)
	}
	rows, err := db.QueryViewContext(context.Background(), viewName, map[string]any{"__abi_master_id": 1})
	if err != nil {
		t.Fatalf("QueryViewContext failed for SQL:\n%s\nerror: %v", sqlText, err)
	}
	defer rows.Close()
	if len(rows.Columns()) == 0 {
		t.Fatal("saved query returned no columns")
	}
}

func TestQueryContextExecutesComplexSavedQuery(t *testing.T) {
	dbPath := requireDBFile(t)
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	const viewName = "v_abia_master_query_general"
	sqlText, err := db.ViewSQL(viewName)
	if err != nil {
		t.Skipf("saved query unavailable: %v", err)
	}
	rows, err := db.QueryContext(context.Background(), "SELECT TOP 1 * FROM ["+viewName+"]", nil)
	if err != nil {
		t.Fatalf("complex saved query failed:\n%s\nerror: %v", sqlText, err)
	}
	defer rows.Close()
	if len(rows.Columns()) == 0 {
		t.Fatal("complex saved query returned no columns")
	}
}

func TestQueryContextExecutesNestedJoinSavedQuery(t *testing.T) {
	dbPath := requireDBFile(t)
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()
	const viewName = "s_abha_house_review"
	if _, err := db.ViewSQL(viewName); err != nil {
		t.Skipf("saved query unavailable: %v", err)
	}
	rows, err := db.QueryContext(context.Background(), "SELECT TOP 1 * FROM ["+viewName+"]", nil)
	if err != nil {
		t.Fatalf("nested-join saved query failed: %v", err)
	}
	defer rows.Close()
	if len(rows.Columns()) == 0 {
		t.Fatal("nested-join saved query returned no columns")
	}
}

func TestAllRestoredSelectViewsParseInGoEngine(t *testing.T) {
	dbPath := requireDBFile(t)
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()
	views, err := db.Views()
	if err != nil {
		t.Fatal(err)
	}
	parsed := 0
	for _, name := range views {
		sqlText, err := db.ViewSQL(name)
		if err != nil {
			if strings.Contains(err.Error(), "only SELECT views are supported") {
				continue
			}
			t.Errorf("ViewSQL(%q): %v", name, err)
			continue
		}
		if _, err := parseAccessSQL(sqlText); err != nil {
			t.Errorf("parse saved query %q: %v\n%s", name, err, sqlText)
			continue
		}
		parsed++
	}
	if parsed == 0 {
		t.Fatal("no saved SELECT queries parsed")
	}
}

func TestQueryPageContextAndPager(t *testing.T) {
	dbPath := requireDBFile(t)
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	tableName := ""
	orderColumn := ""
	for _, candidate := range []string{"t_abi_master_event", "t_zzw_w02_table_field", "t_abi_hbl"} {
		data, err := db.readTableBatched(context.Background(), candidate, nil, 2)
		if err != nil || len(data.Rows) < 2 || len(data.Columns) == 0 {
			continue
		}
		tableName = candidate
		orderColumn = data.Columns[0]
		break
	}
	if tableName == "" {
		t.Skip("fixture has no table with at least two rows")
	}
	sqlText := fmt.Sprintf("SELECT * FROM %s ORDER BY %s", quoteIdentifier(tableName), quoteIdentifier(orderColumn))
	first, err := db.QueryPageContext(context.Background(), sqlText, nil, PageRequest{Size: 1})
	if err != nil {
		t.Fatalf("first page failed: %v", err)
	}
	if len(first.Rows) != 1 {
		t.Fatalf("first page rows=%d", len(first.Rows))
	}
	if first.NextCursor == "" {
		t.Fatal("first page did not include a next cursor")
	}
	next, err := db.QueryPageContext(context.Background(), sqlText, nil, PageRequest{Size: 1, After: first.NextCursor})
	if err != nil {
		t.Fatalf("next page failed: %v", err)
	}
	if next.PrevCursor == "" {
		t.Fatal("next page did not include previous cursor")
	}
	previous, err := db.QueryPageContext(context.Background(), sqlText, nil, PageRequest{Size: 1, Before: next.PrevCursor})
	if err != nil {
		t.Fatalf("previous page failed: %v", err)
	}
	if len(previous.Rows) != 1 || rowKey(previous.Rows[0]) != rowKey(first.Rows[0]) {
		t.Fatalf("previous page did not return the first row")
	}
	if _, err := db.QueryPageContext(
		context.Background(),
		sqlText+" LIMIT 1",
		nil,
		PageRequest{Size: 1, After: first.NextCursor},
	); err == nil || !strings.Contains(err.Error(), "SQL or parameters changed") {
		t.Fatalf("cursor/query mismatch error=%v", err)
	}
	if _, err := db.QueryPageContext(context.Background(), "SELECT * FROM "+quoteIdentifier(tableName), nil, PageRequest{}); err != nil {
		t.Fatalf("single-table inferred ordering failed: %v", err)
	}
	if _, err := db.QueryPageContext(context.Background(),
		"SELECT a.* FROM [t_abi_hbl] AS a INNER JOIN [t_abi_hbl] AS b ON a.abi_hbl_id=b.abi_hbl_id",
		nil, PageRequest{}); err == nil || !strings.Contains(err.Error(), "ORDER BY") {
		t.Fatalf("unordered joined page error=%v", err)
	}

	pager, err := db.PreparePagerContext(context.Background(), sqlText, nil, 1)
	if err != nil {
		t.Fatalf("PreparePagerContext failed: %v", err)
	}
	path := pager.path
	page, err := pager.Page(context.Background(), 1)
	if err != nil {
		t.Fatalf("Pager.Page failed: %v", err)
	}
	if len(page.Rows) != 1 || pager.RowCount() < 1 || path == "" {
		t.Fatalf("unexpected pager result: rows=%d count=%d path=%q", len(page.Rows), pager.RowCount(), path)
	}
	if err := pager.Close(); err != nil {
		t.Fatalf("Pager.Close failed: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("spool was not removed: path=%q err=%v", path, err)
	}
}

func BenchmarkGoQueryEngineFirstPage(b *testing.B) {
	dbPath := strings.TrimSpace(os.Getenv("MDBGO_TEST_DB"))
	if dbPath == "" {
		dbPath = defaultTestDB
	}
	if _, err := os.Stat(dbPath); err != nil {
		b.Skipf("database fixture unavailable: %v", err)
	}
	db, err := Open(dbPath)
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()
	const query = "SELECT [abi_hbl_id], [house_no] FROM [t_abi_hbl] LIMIT 100"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rows, err := db.QueryContext(context.Background(), query, nil)
		if err != nil {
			b.Fatal(err)
		}
		_ = rows.Close()
	}
}

func BenchmarkGoQueryEngineParallel(b *testing.B) {
	dbPath := strings.TrimSpace(os.Getenv("MDBGO_TEST_DB"))
	if dbPath == "" {
		dbPath = defaultTestDB
	}
	if _, err := os.Stat(dbPath); err != nil {
		b.Skipf("database fixture unavailable: %v", err)
	}
	db, err := OpenWithOptions(dbPath, OpenOptions{MaxConcurrentQueries: 4})
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()
	const query = "SELECT [abi_hbl_id], [house_no] FROM [t_abi_hbl] LIMIT 100"
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			rows, err := db.QueryContext(context.Background(), query, nil)
			if err != nil {
				b.Error(err)
				return
			}
			_ = rows.Close()
		}
	})
}

func BenchmarkLegacyQueryFirstPage(b *testing.B) {
	dbPath := strings.TrimSpace(os.Getenv("MDBGO_TEST_DB"))
	if dbPath == "" {
		dbPath = defaultTestDB
	}
	if _, err := os.Stat(dbPath); err != nil {
		b.Skipf("database fixture unavailable: %v", err)
	}
	db, err := Open(dbPath)
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()
	const query = "SELECT [abi_hbl_id], [house_no] FROM [t_abi_hbl] LIMIT 100"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := db.queryLegacy(query); err != nil {
			b.Fatal(err)
		}
	}
}
