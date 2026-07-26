package purego

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// rowsToStrings converts a *Rows result into [][]string for easy comparison.
func rowsToStrings(t *testing.T, rows *Rows) [][]string {
	t.Helper()
	var result [][]string
	for rows.Next() {
		vals := rows.Values()
		row := make([]string, len(vals))
		for i, v := range vals {
			if v.IsNull() {
				row[i] = "<nil>"
			} else {
				row[i] = fmt.Sprint(v.Interface())
			}
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err() = %v", err)
	}
	return result
}

// queryRows is a convenience wrapper: runs sql via QueryContext, fails test on error,
// returns rows converted to [][]string.
func queryRows(t *testing.T, mdb *MDB, sql string) [][]string {
	t.Helper()
	rows, err := mdb.QueryContext(nil, sql, nil)
	if err != nil {
		t.Fatalf("QueryContext(%q) error = %v", sql, err)
	}
	defer rows.Close()
	return rowsToStrings(t, rows)
}

const testMDBPath = "../testdb/mdbs/ABIQuery.mdb"

func TestOpenMDB(t *testing.T) {
	if _, err := os.Stat(testMDBPath); os.IsNotExist(err) {
		t.Skip("test MDB file not found:", testMDBPath)
	}

	mdb, err := OpenMDB(testMDBPath)
	if err != nil {
		t.Fatalf("OpenMDB() error = %v", err)
	}
	defer mdb.Close()

	if mdb.GetHandle() == nil {
		t.Fatal("GetHandle() returned nil")
	}
}

func TestFileFormat(t *testing.T) {
	if _, err := os.Stat(testMDBPath); os.IsNotExist(err) {
		t.Skip("test MDB file not found:", testMDBPath)
	}

	mdb, err := OpenMDB(testMDBPath)
	if err != nil {
		t.Fatalf("OpenMDB() error = %v", err)
	}
	defer mdb.Close()

	version, pageSize := mdb.FileFormat()
	t.Logf("File format: %s, page size: %d", version, pageSize)

	if pageSize != 4096 {
		t.Errorf("PageSize = %d, want 4096", pageSize)
	}
	if version == "" {
		t.Error("FileFormat() returned empty version")
	}
}

func TestTables(t *testing.T) {
	if _, err := os.Stat(testMDBPath); os.IsNotExist(err) {
		t.Skip("test MDB file not found:", testMDBPath)
	}

	mdb, err := OpenMDB(testMDBPath)
	if err != nil {
		t.Fatalf("OpenMDB() error = %v", err)
	}
	defer mdb.Close()

	tables, err := mdb.Tables()
	if err != nil {
		t.Fatalf("Tables() error = %v", err)
	}

	t.Logf("Found %d user tables:", len(tables))
	for _, name := range tables {
		t.Logf("  - %s", name)
	}

	if len(tables) == 0 {
		t.Error("Expected at least 1 user table")
	}
}

func TestViews(t *testing.T) {
	if _, err := os.Stat(testMDBPath); os.IsNotExist(err) {
		t.Skip("test MDB file not found:", testMDBPath)
	}

	mdb, err := OpenMDB(testMDBPath)
	if err != nil {
		t.Fatalf("OpenMDB() error = %v", err)
	}
	defer mdb.Close()

	views, err := mdb.Views()
	if err != nil {
		t.Fatalf("Views() error = %v", err)
	}

	t.Logf("Found %d views:", len(views))
	for _, name := range views {
		t.Logf("  - %s", name)
	}
}

func TestTableNames(t *testing.T) {
	if _, err := os.Stat(testMDBPath); os.IsNotExist(err) {
		t.Skip("test MDB file not found:", testMDBPath)
	}

	mdb, err := OpenMDB(testMDBPath)
	if err != nil {
		t.Fatalf("OpenMDB() error = %v", err)
	}
	defer mdb.Close()

	names, err := mdb.TableNames()
	if err != nil {
		t.Fatalf("TableNames() error = %v", err)
	}

	t.Logf("Found %d objects total:", len(names))
	for _, name := range names {
		t.Logf("  - %s", name)
	}
}

func TestGetTableSchema(t *testing.T) {
	if _, err := os.Stat(testMDBPath); os.IsNotExist(err) {
		t.Skip("test MDB file not found:", testMDBPath)
	}

	mdb, err := OpenMDB(testMDBPath)
	if err != nil {
		t.Fatalf("OpenMDB() error = %v", err)
	}
	defer mdb.Close()

	tables, err := mdb.Tables()
	if err != nil || len(tables) == 0 {
		t.Skip("No tables found")
	}

	for _, tableName := range tables {
		schema, err := mdb.GetTableSchema(tableName)
		if err != nil {
			t.Errorf("GetTableSchema(%s) error = %v", tableName, err)
			continue
		}

		t.Logf("Table: %s (rows=%d, cols=%d)", schema.Name, schema.RowCount, len(schema.Columns))
		for _, col := range schema.Columns {
			t.Logf("  Column: %-20s Type: %-15s Size: %d", col.Name, col.TypeName, col.Size)
		}
	}
}

func TestReadTableData(t *testing.T) {
	if _, err := os.Stat(testMDBPath); os.IsNotExist(err) {
		t.Skip("test MDB file not found:", testMDBPath)
	}

	mdb, err := OpenMDB(testMDBPath)
	if err != nil {
		t.Fatalf("OpenMDB() error = %v", err)
	}
	defer mdb.Close()

	tables, err := mdb.Tables()
	if err != nil || len(tables) == 0 {
		t.Skip("No tables found")
	}

	// Read first table
	tableName := tables[0]
	data, err := mdb.ReadTableData(tableName)
	if err != nil {
		t.Fatalf("ReadTableData(%s) error = %v", tableName, err)
	}

	t.Logf("Table %s: %d rows", tableName, len(data))
	for i, row := range data {
		if i >= 5 {
			t.Logf("  ... (%d more rows)", len(data)-5)
			break
		}
		t.Logf("  Row %d: %v", i, row)
	}
}

func TestRowCount(t *testing.T) {
	if _, err := os.Stat(testMDBPath); os.IsNotExist(err) {
		t.Skip("test MDB file not found:", testMDBPath)
	}

	mdb, err := OpenMDB(testMDBPath)
	if err != nil {
		t.Fatalf("OpenMDB() error = %v", err)
	}
	defer mdb.Close()

	tables, err := mdb.Tables()
	if err != nil || len(tables) == 0 {
		t.Skip("No tables found")
	}

	for _, tableName := range tables {
		count, err := mdb.RowCount(tableName)
		if err != nil {
			t.Errorf("RowCount(%s) error = %v", tableName, err)
			continue
		}
		t.Logf("Table %s: %d rows", tableName, count)
	}
}

func TestReadCatalogDirect(t *testing.T) {
	if _, err := os.Stat(testMDBPath); os.IsNotExist(err) {
		t.Skip("test MDB file not found:", testMDBPath)
	}

	handle, err := Open(testMDBPath, MDBNoFlags)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer handle.Close()

	entries := handle.ReadCatalog(MDBAny)
	t.Logf("Catalog entries: %d", len(entries))
	for i, entry := range entries {
		if i >= 20 {
			t.Logf("  ... (%d more entries)", len(entries)-20)
			break
		}
		t.Logf("  [%d] Type=%-12s Name=%-40s Page=%d",
			i, ObjectTypeString(entry.ObjectType), entry.ObjectName, entry.TablePg)
	}
}

func TestOpenFromBuffer(t *testing.T) {
	if _, err := os.Stat(testMDBPath); os.IsNotExist(err) {
		t.Skip("test MDB file not found:", testMDBPath)
	}

	data, err := os.ReadFile(testMDBPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	mdb, err := OpenFromBuffer(data)
	if err != nil {
		t.Fatalf("OpenFromBuffer() error = %v", err)
	}
	defer mdb.Close()

	if mdb.GetHandle() == nil {
		t.Fatal("GetHandle() returned nil")
	}

	tables, err := mdb.Tables()
	if err != nil {
		t.Fatalf("Tables() error = %v", err)
	}
	if len(tables) == 0 {
		t.Error("Expected at least 1 user table")
	}
	t.Logf("OpenFromBuffer: %d tables", len(tables))
}

func TestGetHandle(t *testing.T) {
	if _, err := os.Stat(testMDBPath); os.IsNotExist(err) {
		t.Skip("test MDB file not found:", testMDBPath)
	}

	mdb, err := OpenMDB(testMDBPath)
	if err != nil {
		t.Fatalf("OpenMDB() error = %v", err)
	}
	defer mdb.Close()

	handle := mdb.GetHandle()
	if handle == nil {
		t.Fatal("GetHandle() returned nil")
	}

	entries := handle.ReadCatalog(MDBAny)
	if len(entries) == 0 {
		t.Error("Expected catalog entries")
	}
	t.Logf("GetHandle: catalog has %d entries", len(entries))
}

func TestExportToFile(t *testing.T) {
	if _, err := os.Stat(testMDBPath); os.IsNotExist(err) {
		t.Skip("test MDB file not found:", testMDBPath)
	}

	mdb, err := OpenMDB(testMDBPath)
	if err != nil {
		t.Fatalf("OpenMDB() error = %v", err)
	}
	defer mdb.Close()

	tables, err := mdb.Tables()
	if err != nil || len(tables) == 0 {
		t.Skip("No tables found")
	}

	tmpFile := t.TempDir() + "/export_test.txt"
	err = mdb.ExportToFile(tables[0], tmpFile)
	if err != nil {
		t.Fatalf("ExportToFile(%s) error = %v", tables[0], err)
	}

	stat, err := os.Stat(tmpFile)
	if err != nil {
		t.Fatalf("Stat() exported file error = %v", err)
	}
	if stat.Size() == 0 {
		t.Error("Exported file is empty")
	}

	content, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("ReadFile() exported file error = %v", err)
	}
	t.Logf("Exported %s to %s (%d bytes, %d lines)",
		tables[0], tmpFile, stat.Size(), len(content))
}

func TestNonexistentFile(t *testing.T) {
	_, err := OpenMDB("/nonexistent/file.mdb")
	if err == nil {
		t.Error("Expected error for nonexistent file")
	}
}

func TestCloseNilHandle(t *testing.T) {
	mdb := &MDB{}
	mdb.Close() // should not panic
}

func TestQuerySimple(t *testing.T) {
	if _, err := os.Stat(testMDBPath); os.IsNotExist(err) {
		t.Skip("test MDB file not found:", testMDBPath)
	}

	mdb, err := OpenMDB(testMDBPath)
	if err != nil {
		t.Fatalf("OpenMDB() error = %v", err)
	}
	defer mdb.Close()

	// test readProjectedData directly
	data, err := mdb.readProjectedData("t_abi_hbl", []string{"abi_hbl_id", "issuer_code", "house_no"}, 3)
	if err != nil {
		t.Fatalf("readProjectedData error = %v", err)
	}
	t.Logf("readProjectedData: %d cols, %d rows", len(data.Columns), len(data.Rows))
	for i, row := range data.Rows {
		t.Logf("  Row %d: %v", i, row)
	}

	rows, err := mdb.QueryContext(nil, "SELECT abi_hbl_id, issuer_code, house_no FROM t_abi_hbl WHERE abi_hbl_id <= 3 ORDER BY abi_hbl_id", nil)
	if err != nil {
		t.Fatalf("QueryContext error = %v", err)
	}
	defer rows.Close()

	cols := rows.Columns()
	t.Logf("Columns: %d", len(cols))
	count := 0
	for rows.Next() {
		vals := rows.Values()
		t.Logf("Row %d: %v", count, vals)
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("Rows error = %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 rows, got %d", count)
	}
}

// --- 综合 SQL 引擎测试 ---

func TestSQLBasic(t *testing.T) {
	mdb := openTestMDB(t)
	defer mdb.Close()

	// SELECT with LIMIT
	rows := queryRows(t, mdb, "SELECT abi_hbl_id, issuer_code FROM t_abi_hbl ORDER BY abi_hbl_id LIMIT 5")
	if len(rows) != 5 {
		t.Fatalf("LIMIT 5: expected 5 rows, got %d", len(rows))
	}
	if len(rows[0]) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(rows[0]))
	}
	// first row should be abi_hbl_id=1, issuer_code=TLKP
	if rows[0][0] != "1" {
		t.Errorf("first row col 0 expected 1, got %q", rows[0][0])
	}

	// SELECT with WHERE and ORDER BY DESC
	rows = queryRows(t, mdb, "SELECT house_no FROM t_abi_hbl WHERE abi_hbl_id <= 3 ORDER BY abi_hbl_id DESC")
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}
	if rows[0][0] != "BOM3384131" || rows[2][0] != "SZXCHI81016X" {
		t.Errorf("DESC order mismatch: got %v", rows)
	}

	// SELECT with column list and aliases
	rows = queryRows(t, mdb, "SELECT abi_hbl_id AS id, issuer_code AS issuer FROM t_abi_hbl WHERE abi_hbl_id = 2")
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0][0] != "2" || rows[0][1] != "ALRB" {
		t.Errorf("alias row mismatch: got %v", rows[0])
	}

	// SELECT * from small table
	rows = queryRows(t, mdb, "SELECT group_code, group_name FROM t_sys_group ORDER BY group_code")
	if len(rows) != 5 {
		t.Fatalf("t_sys_group expected 5 rows, got %d", len(rows))
	}
	for _, r := range rows {
		if len(r) != 2 {
			t.Fatalf("expected 2 columns, got %d", len(r))
		}
	}
}

func TestSQLWhereConditions(t *testing.T) {
	mdb := openTestMDB(t)
	defer mdb.Close()

	// WHERE with <
	rows := queryRows(t, mdb, "SELECT abi_hbl_id FROM t_abi_hbl WHERE abi_hbl_id < 3 ORDER BY abi_hbl_id")
	if len(rows) < 1 {
		t.Fatalf("expected at least 1 row, got %d", len(rows))
	}

	// WHERE with >=
	rows = queryRows(t, mdb, "SELECT abi_hbl_id FROM t_abi_hbl WHERE abi_hbl_id >= 500 ORDER BY abi_hbl_id")
	if len(rows) == 0 {
		t.Fatal("expected at least 1 row for abi_hbl_id >= 500")
	}

	// WHERE with <>
	rows = queryRows(t, mdb, "SELECT abi_hbl_id FROM t_abi_hbl WHERE abi_hbl_id <> 1 ORDER BY abi_hbl_id LIMIT 1")
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0][0] != "2" {
		t.Errorf("expected abi_hbl_id=2, got %q", rows[0][0])
	}

	// WHERE with AND/OR
	rows = queryRows(t, mdb, "SELECT abi_hbl_id FROM t_abi_hbl WHERE (abi_hbl_id = 1 OR abi_hbl_id = 3) AND issuer_code = 'TLKP'")
	if len(rows) != 1 {
		t.Fatalf("expected 1 row (abi_hbl_id=1), got %d: %v", len(rows), rows)
	}
	if rows[0][0] != "1" {
		t.Errorf("expected abi_hbl_id=1, got %q", rows[0][0])
	}

	// WHERE with IN
	rows = queryRows(t, mdb, "SELECT abi_hbl_id FROM t_abi_hbl WHERE abi_hbl_id IN (2, 3) ORDER BY abi_hbl_id")
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0][0] != "2" || rows[1][0] != "3" {
		t.Errorf("IN results mismatch: got %v", rows)
	}

	// WHERE with NOT IN
	rows = queryRows(t, mdb, "SELECT abi_hbl_id FROM t_abi_hbl WHERE abi_hbl_id NOT IN (1, 2, 3) ORDER BY abi_hbl_id LIMIT 3")
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}

	// WHERE with BETWEEN
	rows = queryRows(t, mdb, "SELECT abi_hbl_id FROM t_abi_hbl WHERE abi_hbl_id BETWEEN 2 AND 4 ORDER BY abi_hbl_id")
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows (2,3,4), got %d", len(rows))
	}

	// WHERE with NOT BETWEEN
	rows = queryRows(t, mdb, "SELECT abi_hbl_id FROM t_abi_hbl WHERE abi_hbl_id NOT BETWEEN 1 AND 3 ORDER BY abi_hbl_id LIMIT 2")
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows (4,5), got %d", len(rows))
	}

	// WHERE with LIKE
	rows = queryRows(t, mdb, "SELECT DISTINCT issuer_code FROM t_abi_hbl WHERE issuer_code LIKE 'A%' ORDER BY issuer_code")
	if len(rows) == 0 {
		t.Fatal("expected at least 1 issuer_code starting with A")
	}
	for _, r := range rows {
		if !strings.HasPrefix(r[0], "A") {
			t.Errorf("issuer_code %q does not start with A", r[0])
		}
	}

	// WHERE with NOT LIKE
	rows = queryRows(t, mdb, "SELECT DISTINCT issuer_code FROM t_abi_hbl WHERE issuer_code NOT LIKE 'A%' ORDER BY issuer_code LIMIT 3")
	if len(rows) == 0 {
		t.Fatal("expected at least 1 issuer_code not starting with A")
	}
	for _, r := range rows {
		if strings.HasPrefix(r[0], "A") {
			t.Errorf("issuer_code %q starts with A but should not", r[0])
		}
	}

	// WHERE with IS NULL - check a known nullable column
	rows = queryRows(t, mdb, "SELECT abi_hbl_id FROM t_abi_hbl WHERE hold_disposition_code IS NULL ORDER BY abi_hbl_id LIMIT 3")
	// some rows may have NULL hold_disposition_code
	t.Logf("IS NULL rows: %d", len(rows))

	// WHERE with IS NOT NULL
	rows = queryRows(t, mdb, "SELECT abi_hbl_id FROM t_abi_hbl WHERE hold_disposition_code IS NOT NULL ORDER BY abi_hbl_id LIMIT 3")
	t.Logf("IS NOT NULL rows: %d", len(rows))

	// WHERE with text equality
	rows = queryRows(t, mdb, "SELECT abi_hbl_id FROM t_abi_hbl WHERE house_no = 'SZXCHI81016X'")
	if len(rows) != 1 {
		t.Fatalf("expected 1 row for house_no 'SZXCHI81016X', got %d", len(rows))
	}
}

func TestSQLJoins(t *testing.T) {
	mdb := openTestMDB(t)
	defer mdb.Close()

	// INNER JOIN: t_abi_hbl -> t_abi_master
	rows := queryRows(t, mdb, `SELECT h.abi_hbl_id, h.issuer_code, m.master_bl_no
FROM t_abi_hbl h INNER JOIN t_abi_master m ON h.abi_master_id = m.abi_master_id
WHERE h.abi_hbl_id <= 3 ORDER BY h.abi_hbl_id`)
	if len(rows) != 3 {
		t.Fatalf("INNER JOIN expected 3 rows, got %d", len(rows))
	}
	// verify columns
	if len(rows[0]) != 3 {
		t.Fatalf("INNER JOIN expected 3 columns, got %d", len(rows[0]))
	}
	t.Logf("INNER JOIN row 0: %v", rows[0])

	// LEFT JOIN
	rows = queryRows(t, mdb, `SELECT h.abi_hbl_id, h.issuer_code, m.master_bl_no
FROM t_abi_hbl h LEFT JOIN t_abi_master m ON h.abi_master_id = m.abi_master_id
WHERE h.abi_hbl_id <= 3 ORDER BY h.abi_hbl_id`)
	if len(rows) != 3 {
		t.Fatalf("LEFT JOIN expected 3 rows, got %d", len(rows))
	}

	// Self-join on z1 (simple table, IDs start at 24)
	rows = queryRows(t, mdb, "SELECT a.ID, b.ID FROM z1 a, z1 b WHERE a.ID = b.ID AND a.ID <= 26 ORDER BY a.ID")
	if len(rows) != 3 {
		t.Logf("self-join returned %d rows (expected 3)", len(rows))
	}
}

func TestSQLAggregates(t *testing.T) {
	mdb := openTestMDB(t)
	defer mdb.Close()

	// COUNT(*)
	rows := queryRows(t, mdb, "SELECT COUNT(*) FROM t_abi_hbl")
	if len(rows) != 1 {
		t.Fatalf("COUNT expected 1 row, got %d", len(rows))
	}
	if len(rows[0]) != 1 {
		t.Fatalf("COUNT expected 1 column, got %d", len(rows[0]))
	}
	// RowCount returns 501, so COUNT(*) should be 501
	if rows[0][0] != "501" {
		t.Errorf("COUNT(*) expected 501, got %q", rows[0][0])
	}

	// COUNT with column name
	rows = queryRows(t, mdb, "SELECT COUNT(abi_hbl_id) FROM t_abi_hbl")
	if len(rows) != 1 || rows[0][0] != "501" {
		t.Errorf("COUNT(abi_hbl_id) expected 501, got %v", rows)
	}

	// COUNT(DISTINCT col) via subquery - use scalar subquery in SELECT
	// Note: FROM subqueries are not supported by this engine, so use IN-subquery approach
	uniqRows := queryRows(t, mdb, "SELECT DISTINCT issuer_code FROM t_abi_hbl")
	if len(uniqRows) > 0 {
		t.Logf("COUNT(DISTINCT issuer_code) = %d", len(uniqRows))
	}

	// MIN/MAX/SUM/AVG
	rows = queryRows(t, mdb, "SELECT MIN(abi_hbl_id), MAX(abi_hbl_id), SUM(abi_hbl_id), AVG(abi_hbl_id) FROM t_abi_hbl")
	if len(rows) != 1 {
		t.Fatalf("aggregates expected 1 row, got %d", len(rows))
	}
	if rows[0][0] != "1" {
		t.Errorf("MIN(abi_hbl_id) expected 1, got %q", rows[0][0])
	}
	if rows[0][1] != "501" {
		t.Errorf("MAX(abi_hbl_id) expected 501, got %q", rows[0][1])
	}

	// GROUP BY
	rows = queryRows(t, mdb, "SELECT issuer_code, COUNT(*) FROM t_abi_hbl GROUP BY issuer_code ORDER BY COUNT(*) DESC LIMIT 5")
	if len(rows) != 5 {
		t.Fatalf("GROUP BY expected 5 rows, got %d", len(rows))
	}
	if len(rows[0]) != 2 {
		t.Fatalf("GROUP BY expected 2 columns, got %d", len(rows[0]))
	}
	// first row should have the highest count
	t.Logf("Top issuer: %s (count=%s)", rows[0][0], rows[0][1])

	// GROUP BY with HAVING
	rows = queryRows(t, mdb, "SELECT issuer_code, COUNT(*) as cnt FROM t_abi_hbl GROUP BY issuer_code HAVING COUNT(*) > 10 ORDER BY cnt DESC")
	if len(rows) == 0 {
		t.Fatal("HAVING expected at least 1 issuer with count > 10")
	}
	for _, r := range rows {
		count := r[1]
		if count == "0" || count == "" {
			t.Errorf("HAVING filter broken: count should be > 10, got %q", count)
		}
	}
	t.Logf("HAVING COUNT(*) > 10: %d issuers, top = %s (%s)", len(rows), rows[0][0], rows[0][1])

	// SUM with GROUP BY - use column alias for ORDER BY
	rows = queryRows(t, mdb, "SELECT issuer_code, SUM(manifest_quantity) AS total_qty FROM t_abi_hbl GROUP BY issuer_code ORDER BY total_qty DESC LIMIT 3")
	if len(rows) != 3 {
		t.Fatalf("SUM GROUP BY expected 3 rows, got %d", len(rows))
	}
	if len(rows[0]) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(rows[0]))
	}
	t.Logf("Top issuer by manifest_qty: %s (sum=%s)", rows[0][0], rows[0][1])

	// GROUP BY multiple columns
	rows = queryRows(t, mdb, "SELECT issuer_code, quantity_unit, COUNT(*) FROM t_abi_hbl GROUP BY issuer_code, quantity_unit ORDER BY issuer_code, quantity_unit LIMIT 5")
	if len(rows) > 0 && len(rows[0]) != 3 {
		t.Fatalf("multi-group expected 3 columns, got %d", len(rows[0]))
	}
}

func TestSQLDistinctTop(t *testing.T) {
	mdb := openTestMDB(t)
	defer mdb.Close()

	// DISTINCT on single column
	allRows := queryRows(t, mdb, "SELECT issuer_code FROM t_abi_hbl")
	totalRows := len(allRows)
	distRows := queryRows(t, mdb, "SELECT DISTINCT issuer_code FROM t_abi_hbl ORDER BY issuer_code")
	if len(distRows) >= totalRows {
		t.Errorf("DISTINCT should reduce row count: total=%d, distinct=%d", totalRows, len(distRows))
	}
	t.Logf("DISTINCT issuer_code: %d unique codes", len(distRows))

	// TOP N
	rows := queryRows(t, mdb, "SELECT TOP 3 abi_hbl_id FROM t_abi_hbl ORDER BY abi_hbl_id")
	if len(rows) != 3 {
		t.Fatalf("TOP 3 expected 3 rows, got %d", len(rows))
	}

	// DISTINCT with TOP
	rows = queryRows(t, mdb, "SELECT DISTINCT TOP 3 issuer_code FROM t_abi_hbl ORDER BY issuer_code")
	if len(rows) != 3 {
		t.Fatalf("DISTINCT TOP 3 expected 3 rows, got %d", len(rows))
	}
}

func TestSQLLimitOffset(t *testing.T) {
	mdb := openTestMDB(t)
	defer mdb.Close()

	// LIMIT only
	rows := queryRows(t, mdb, "SELECT abi_hbl_id FROM t_abi_hbl ORDER BY abi_hbl_id LIMIT 3")
	if len(rows) != 3 {
		t.Fatalf("LIMIT 3 expected 3 rows, got %d", len(rows))
	}
	if rows[0][0] != "1" || rows[2][0] != "3" {
		t.Errorf("LIMIT rows wrong: got %v", rows)
	}

	// LIMIT with OFFSET
	rows = queryRows(t, mdb, "SELECT abi_hbl_id FROM t_abi_hbl ORDER BY abi_hbl_id LIMIT 3 OFFSET 5")
	if len(rows) != 3 {
		t.Fatalf("LIMIT 3 OFFSET 5 expected 3 rows, got %d", len(rows))
	}
	if rows[0][0] != "6" {
		t.Errorf("OFFSET 5 first row expected abi_hbl_id=6, got %q", rows[0][0])
	}

	// OFFSET without LIMIT
	rows = queryRows(t, mdb, "SELECT abi_hbl_id FROM t_abi_hbl ORDER BY abi_hbl_id OFFSET 498")
	if len(rows) != 3 {
		t.Fatalf("OFFSET 498 expected 3 rows (498,499,500,501 -> 3), got %d", len(rows))
	}
}

func TestSQLOrderBy(t *testing.T) {
	mdb := openTestMDB(t)
	defer mdb.Close()

	// ORDER BY single column ASC (default)
	rows := queryRows(t, mdb, "SELECT abi_hbl_id FROM t_abi_hbl ORDER BY abi_hbl_id LIMIT 3")
	if rows[0][0] != "1" || rows[2][0] != "3" {
		t.Errorf("ASC order wrong: got %v", rows)
	}

	// ORDER BY single column DESC
	rows = queryRows(t, mdb, "SELECT abi_hbl_id FROM t_abi_hbl ORDER BY abi_hbl_id DESC LIMIT 3")
	if rows[0][0] != "501" || rows[2][0] != "499" {
		t.Errorf("DESC order wrong: got %v", rows)
	}

	// ORDER BY multiple columns
	rows = queryRows(t, mdb, "SELECT issuer_code, abi_hbl_id FROM t_abi_hbl ORDER BY issuer_code, abi_hbl_id LIMIT 5")
	if len(rows) != 5 {
		t.Fatalf("expected 5 rows, got %d", len(rows))
	}
	// First row should have the smallest issuer_code value
	for i := 1; i < len(rows); i++ {
		if rows[i][0] < rows[i-1][0] {
			t.Errorf("ORDER BY issuer_code broken at row %d: %q < %q", i, rows[i][0], rows[i-1][0])
			break
		}
	}

	// ORDER BY expression
	rows = queryRows(t, mdb, "SELECT abi_hbl_id FROM t_abi_hbl ORDER BY abi_hbl_id + 0 LIMIT 3")
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}
}

func TestSQLScalarFunctions(t *testing.T) {
	mdb := openTestMDB(t)
	defer mdb.Close()

	// UCASE / UPPER
	rows := queryRows(t, mdb, "SELECT UCASE(issuer_code) FROM t_abi_hbl WHERE abi_hbl_id = 1")
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0][0] != "TLKP" {
		t.Errorf("UCASE expected TLKP, got %q", rows[0][0])
	}

	// LCASE / LOWER
	rows = queryRows(t, mdb, "SELECT LCASE(issuer_code) FROM t_abi_hbl WHERE abi_hbl_id = 1")
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0][0] != "tlkp" {
		t.Errorf("LCASE expected tlkp, got %q", rows[0][0])
	}

	// LEN
	rows = queryRows(t, mdb, "SELECT LEN(issuer_code) FROM t_abi_hbl WHERE abi_hbl_id = 1")
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0][0] != "4" {
		t.Errorf("LEN expected 4, got %q", rows[0][0])
	}

	// IIF
	rows = queryRows(t, mdb, `SELECT abi_hbl_id, IIF(abi_hbl_id <= 3, 'small', 'large') FROM t_abi_hbl WHERE abi_hbl_id IN (1, 5) ORDER BY abi_hbl_id`)
	if len(rows) != 2 {
		t.Fatalf("IIF expected 2 rows, got %d", len(rows))
	}
	if rows[0][1] != "small" || rows[1][1] != "large" {
		t.Errorf("IIF results wrong: got %v", rows)
	}

	// NZ (treat null as default)
	rows = queryRows(t, mdb, "SELECT NZ(hold_disposition_code, 'N/A') FROM t_abi_hbl WHERE abi_hbl_id = 1")
	if len(rows) != 1 {
		t.Fatalf("NZ expected 1 row, got %d", len(rows))
	}
	if rows[0][0] == "" {
		t.Errorf("NZ should not return empty string")
	}
	t.Logf("NZ(hold_disposition_code, 'N/A') = %q", rows[0][0])
}

func TestSQLComplexExpressions(t *testing.T) {
	mdb := openTestMDB(t)
	defer mdb.Close()

	// Arithmetic expressions in SELECT
	rows := queryRows(t, mdb, "SELECT abi_hbl_id, abi_hbl_id + 100, abi_hbl_id * 2 FROM t_abi_hbl WHERE abi_hbl_id = 1")
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if len(rows[0]) != 3 {
		t.Fatalf("expected 3 columns, got %d", len(rows[0]))
	}
	if rows[0][0] != "1" || rows[0][1] != "101" || rows[0][2] != "2" {
		t.Errorf("arithmetic wrong: got %v", rows[0])
	}

	// String concatenation using &
	rows = queryRows(t, mdb, "SELECT issuer_code & '-' & house_no FROM t_abi_hbl WHERE abi_hbl_id = 1")
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	expected := "TLKP-SZXCHI81016X"
	if rows[0][0] != expected {
		t.Errorf("concat expected %q, got %q", expected, rows[0][0])
	}

	// IIF in WHERE
	rows = queryRows(t, mdb, "SELECT abi_hbl_id FROM t_abi_hbl WHERE IIF(abi_hbl_id < 10, 1, 0) = 1 ORDER BY abi_hbl_id LIMIT 5")
	if len(rows) != 5 {
		t.Fatalf("IIF in WHERE expected 5 rows, got %d", len(rows))
	}
}

func TestSQLSubqueries(t *testing.T) {
	mdb := openTestMDB(t)
	defer mdb.Close()

	// Scalar subquery in SELECT
	rows := queryRows(t, mdb, "SELECT abi_hbl_id, (SELECT COUNT(*) FROM t_abi_master) AS master_count FROM t_abi_hbl WHERE abi_hbl_id = 1")
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if len(rows[0]) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(rows[0]))
	}
	if rows[0][0] != "1" {
		t.Errorf("expected abi_hbl_id=1, got %q", rows[0][0])
	}
	t.Logf("subquery: abi_hbl_id=%s master_count=%s", rows[0][0], rows[0][1])

	// Subquery in WHERE (IN)
	rows = queryRows(t, mdb, `SELECT abi_hbl_id FROM t_abi_hbl
WHERE abi_master_id IN (SELECT abi_master_id FROM t_abi_master WHERE mode_code = 'A')
ORDER BY abi_hbl_id LIMIT 3`)
	if len(rows) != 3 {
		t.Fatalf("subquery IN expected 3 rows, got %d", len(rows))
	}
	t.Logf("subquery IN first rows: %v", rows)

	// Subquery in WHERE (NOT IN)
	rows = queryRows(t, mdb, `SELECT abi_hbl_id FROM t_abi_hbl
WHERE abi_master_id NOT IN (SELECT abi_master_id FROM t_abi_master WHERE mode_code = 'X')
ORDER BY abi_hbl_id LIMIT 3`)
	if len(rows) != 3 {
		t.Fatalf("subquery NOT IN expected 3 rows, got %d", len(rows))
	}
}

func TestSQLViews(t *testing.T) {
	mdb := openTestMDB(t)
	defer mdb.Close()

	// Try querying from a non-materialized view (saved query)
	// v_sys_group_branch_user is a simple view: SELECT * FROM t_sys_group_branch_user
	viewNames := []string{"v_sys_group_branch_user", "v_sys_group", "v_sys_group_branch"}

	for _, vn := range viewNames {
		sqlText, err := mdb.ViewSQL(vn)
		if err != nil {
			t.Logf("ViewSQL(%q) error (may be expected for complex views): %v", vn, err)
			continue
		}
		t.Logf("ViewSQL(%q) = %q", vn, sqlText)

		rows, err := mdb.QueryViewContext(nil, vn, nil)
		if err != nil {
			t.Logf("QueryViewContext(%q) error: %v", vn, err)
			continue
		}
		var count int
		for rows.Next() {
			count++
		}
		rows.Close()
		if count == 0 {
			t.Errorf("QueryViewContext(%q) returned 0 rows", vn)
		} else {
			t.Logf("QueryViewContext(%q) = %d rows", vn, count)
		}
	}
}

func TestSQLEdgeCases(t *testing.T) {
	mdb := openTestMDB(t)
	defer mdb.Close()

	// Empty result set
	rows := queryRows(t, mdb, "SELECT * FROM t_abi_hbl WHERE 1 = 0")
	if len(rows) != 0 {
		t.Errorf("impossible WHERE should return 0 rows, got %d", len(rows))
	}

	// WHERE always true
	rows = queryRows(t, mdb, "SELECT abi_hbl_id FROM t_abi_hbl WHERE 1 = 1 ORDER BY abi_hbl_id LIMIT 2")
	if len(rows) != 2 {
		t.Fatalf("WHERE 1=1 expected 2 rows, got %d", len(rows))
	}

	// ORDER BY with column index (Access style)
	rows = queryRows(t, mdb, "SELECT abi_hbl_id, issuer_code FROM t_abi_hbl ORDER BY 1 LIMIT 3")
	if len(rows) != 3 {
		t.Fatalf("ORDER BY 1 expected 3 rows, got %d", len(rows))
	}

	// Multiple ORDER BY with DESC/ASC
	rows = queryRows(t, mdb, "SELECT issuer_code, abi_hbl_id FROM t_abi_hbl ORDER BY issuer_code ASC, abi_hbl_id DESC LIMIT 5")
	if len(rows) != 5 {
		t.Fatalf("expected 5 rows, got %d", len(rows))
	}
}

// openTestMDB is a test helper that opens the standard test MDB file.
func openTestMDB(t *testing.T) *MDB {
	t.Helper()
	if _, err := os.Stat(testMDBPath); os.IsNotExist(err) {
		t.Skip("test MDB file not found:", testMDBPath)
	}
	mdb, err := OpenMDB(testMDBPath)
	if err != nil {
		t.Fatalf("OpenMDB(%q) error = %v", testMDBPath, err)
	}
	return mdb
}
