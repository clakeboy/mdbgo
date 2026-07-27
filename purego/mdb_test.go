package purego

import (
	"os"
	"testing"
)

// openTestMDB is a test helper that opens the standard test MDB file.

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
	data, nulls, err := mdb.ReadTableData(tableName)
	if err != nil {
		t.Fatalf("ReadTableData(%s) error = %v", tableName, err)
	}
	_ = nulls

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

	schema, err := mdb.GetTableSchema("t_abi_hbl")
	if err != nil {
		t.Fatalf("GetTableSchema error = %v", err)
	}
	t.Logf("Schema: %d columns, %d rows", len(schema.Columns), schema.RowCount)

	data, nulls, err := mdb.ReadTableData("t_abi_hbl")
	if err != nil {
		t.Fatalf("ReadTableData error = %v", err)
	}
	_ = nulls
	t.Logf("ReadTableData: %d rows", len(data))
	for i, row := range data {
		if i >= 3 {
			break
		}
		t.Logf("  Row %d: %v", i, row)
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
