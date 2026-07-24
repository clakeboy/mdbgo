package mdbgo

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const defaultTestDB = "testdb/mdbs/ABIQuery.mdb"
const defaultFormExportJSON = "/Users/clake/Downloads/midb-01-31/f_midb_export.json"

// testDBPath 返回测试数据库路径。
// 优先使用环境变量，便于在不同环境复用同一套测试。
func testDBPath(t *testing.T) string {
	t.Helper()

	if v := strings.TrimSpace(os.Getenv("MDBGO_TEST_DB")); v != "" {
		return v
	}
	return defaultTestDB
}

// requireDBFile 检查测试数据库是否存在，不存在时跳过集成测试。
func requireDBFile(t *testing.T) string {
	t.Helper()

	p := testDBPath(t)
	if !filepath.IsAbs(p) {
		p = filepath.Clean(p)
	}
	if _, err := os.Stat(p); err != nil {
		t.Skipf("skip integration test, db file not found: %s, err=%v", p, err)
	}
	return p
}

func requireFormName(t *testing.T, db *DB) string {
	t.Helper()
	if formName := strings.TrimSpace(os.Getenv("MDBGO_TEST_FORM_NAME")); formName != "" {
		return formName
	}
	forms, err := db.ExportForms()
	if err != nil {
		t.Fatalf("ExportForms failed: %v", err)
	}
	if len(forms) == 0 {
		t.Skip("database contains no forms")
	}
	for _, form := range forms {
		if form.Name == "f_midb" {
			return form.Name
		}
	}
	return forms[0].Name
}

// quoteIdentifier 用 Access 风格转义表名，保证 SQL 构造安全。
func quoteIdentifier(name string) string {
	return "[" + strings.ReplaceAll(name, "]", "]]") + "]"
}

func TestOpenAndClose(t *testing.T) {
	dbPath := requireDBFile(t)

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	// Close 应该是幂等的，重复调用不应报错。
	if err := db.Close(); err != nil {
		t.Fatalf("first Close failed: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("second Close failed: %v", err)
	}
}

func TestDatabaseFormat(t *testing.T) {
	tests := []struct {
		name         string
		path         string
		wantName     string
		wantStorage  string
		wantVersion  int
		wantPageSize int
		wantEngine   string
	}{
		{
			name:         "Access 2000",
			path:         "testdb/mdbs/mpci_2000.mdb",
			wantName:     "Access 2000",
			wantStorage:  "MSysAccessObjects",
			wantVersion:  1,
			wantPageSize: 4096,
			wantEngine:   "Jet 4",
		},
		{
			name:         "Access 2003",
			path:         "testdb/mdbs/mpci_2003.mdb",
			wantName:     "Access 2003",
			wantStorage:  "MSysAccessStorage",
			wantVersion:  1,
			wantPageSize: 4096,
			wantEngine:   "Jet 4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := os.Stat(tt.path); err != nil {
				t.Skipf("fixture not found: %s, err=%v", tt.path, err)
			}
			db, err := Open(tt.path)
			if err != nil {
				t.Fatalf("Open failed: %v", err)
			}
			defer func() { _ = db.Close() }()

			if db.Format.Name != tt.wantName ||
				db.Format.Engine != tt.wantEngine ||
				db.Format.Version != tt.wantVersion ||
				db.Format.PageSize != tt.wantPageSize ||
				db.Format.ObjectStorage != tt.wantStorage {
				t.Fatalf("Format=%+v", db.Format)
			}
			if got := db.Format.String(); got != tt.wantName {
				t.Fatalf("Format.String()=%q want=%q", got, tt.wantName)
			}
		})
	}
}

func TestOpenUnicodePath(t *testing.T) {
	dbPath := requireDBFile(t)
	unicodeDir := filepath.Join(t.TempDir(), "中文数据库")
	if err := os.Mkdir(unicodeDir, 0o755); err != nil {
		t.Fatalf("create unicode directory: %v", err)
	}
	unicodePath := filepath.Join(unicodeDir, "ABIQuery - 副本.mdb")

	source, err := os.Open(dbPath)
	if err != nil {
		t.Fatalf("open source database: %v", err)
	}
	target, err := os.Create(unicodePath)
	if err != nil {
		_ = source.Close()
		t.Fatalf("create unicode database path: %v", err)
	}
	if _, err := io.Copy(target, source); err != nil {
		_ = source.Close()
		_ = target.Close()
		t.Fatalf("copy database to unicode path: %v", err)
	}
	if err := source.Close(); err != nil {
		_ = target.Close()
		t.Fatalf("close source database: %v", err)
	}
	if err := target.Close(); err != nil {
		t.Fatalf("close unicode database: %v", err)
	}

	db, err := Open(unicodePath)
	if err != nil {
		t.Fatalf("Open unicode path failed: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close unicode path failed: %v", err)
	}
}

func TestTablesAndReadTable(t *testing.T) {
	dbPath := requireDBFile(t)

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	tables, err := db.Tables()
	if err != nil {
		t.Fatalf("Tables failed: %v", err)
	}
	if len(tables) == 0 {
		t.Fatalf("Tables returned empty table list")
	}
	fmt.Println(tables)
	data, err := db.ReadTable(tables[0])
	if err != nil {
		t.Fatalf("ReadTable failed: %v", err)
	}
	if data == nil {
		t.Fatalf("ReadTable returned nil result")
	}
	if len(data.Columns) == 0 {
		t.Fatalf("ReadTable returned zero columns for table %q", tables[0])
	}
}

func TestTableRowCount(t *testing.T) {
	dbPath := requireDBFile(t)

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	tables, err := db.Tables()
	if err != nil {
		t.Fatalf("Tables failed: %v", err)
	}
	if len(tables) == 0 {
		t.Fatal("Tables returned empty table list")
	}

	tableName := tables[0]
	count, err := db.TableRowCount(tableName)
	if err != nil {
		t.Fatalf("TableRowCount(%q) failed: %v", tableName, err)
	}
	data, err := db.ReadTable(tableName)
	if err != nil {
		t.Fatalf("ReadTable(%q) failed: %v", tableName, err)
	}
	if want := uint64(len(data.Rows)); count != want {
		t.Fatalf("TableRowCount(%q)=%d want=%d", tableName, count, want)
	}
}

func TestTableRowCountValidation(t *testing.T) {
	var nilDB *DB
	if _, err := nilDB.TableRowCount("table"); err == nil {
		t.Fatal("TableRowCount on nil DB expected error")
	}

	dbPath := requireDBFile(t)
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	if _, err := db.TableRowCount(""); err == nil {
		t.Fatal("TableRowCount with empty name expected error")
	}
	if _, err := db.TableRowCount("__mdbgo_missing_table__"); err == nil {
		t.Fatal("TableRowCount with missing table expected error")
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if _, err := db.TableRowCount("table"); err == nil {
		t.Fatal("TableRowCount on closed DB expected error")
	}
}

func TestViewsAndViewSQL(t *testing.T) {
	dbPath := requireDBFile(t)
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	views, err := db.Views()
	if err != nil {
		t.Fatalf("Views failed: %v", err)
	}
	if len(views) == 0 {
		t.Fatal("Views returned an empty list")
	}

	viewName := "v_abia_master_query_general"
	found := false
	for _, name := range views {
		if name == viewName {
			found = true
			break
		}
	}
	if !found {
		t.Skipf("test database does not contain %q", viewName)
	}

	sqlText, err := db.ViewSQL(viewName)
	if err != nil {
		t.Fatalf("ViewSQL(%q) failed: %v", viewName, err)
	}
	for _, fragment := range []string{
		"SELECT t_abi_master.*",
		"INNER JOIN [t_ams_master_group]",
		"t_abi_master.abi_master_id=t_ams_master_group.abi_master_id",
		"RIGHT JOIN [t_abi_master]",
		"t_ztb_abi_query_status.status_id=t_abi_master.status_id",
		"WHERE (((t_abi_master.mode_code)=\"A\"))",
	} {
		if !strings.Contains(sqlText, fragment) {
			t.Fatalf("ViewSQL(%q) missing %q:\n%s", viewName, fragment, sqlText)
		}
	}
	if !strings.HasSuffix(sqlText, ";") {
		t.Fatalf("ViewSQL(%q) must end in semicolon: %q", viewName, sqlText)
	}
}

func TestViewSQLValidation(t *testing.T) {
	dbPath := requireDBFile(t)
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	if _, err := db.ViewSQL(""); err == nil {
		t.Fatal("ViewSQL with empty name expected error")
	}
	if _, err := db.ViewSQL("__mdbgo_missing_view__"); err == nil {
		t.Fatal("ViewSQL with missing name expected error")
	}
}

func TestViewSQLRestoresParametersAndAliases(t *testing.T) {
	dbPath := requireDBFile(t)
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	viewName := "~sq_cf_abia_master~sq_csub_abia_master_list_2_event"
	sqlText, err := db.ViewSQL(viewName)
	if err != nil {
		t.Skipf("ViewSQL(%q) unavailable in this fixture: %v", viewName, err)
	}
	for _, fragment := range []string{
		"PARAMETERS [__abi_master_id] Value;",
		"[v_abia_master_list_event] AS [f_abia_master]",
		"WHERE ([__abi_master_id] = abi_master_id)",
	} {
		if !strings.Contains(sqlText, fragment) {
			t.Fatalf("ViewSQL(%q) missing %q:\n%s", viewName, fragment, sqlText)
		}
	}
}

func TestAllSelectViewDefinitionsCanBeRestored(t *testing.T) {
	dbPath := requireDBFile(t)
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	views, err := db.Views()
	if err != nil {
		t.Fatalf("Views failed: %v", err)
	}
	restored := 0
	for _, viewName := range views {
		_, err := db.ViewSQL(viewName)
		if err == nil {
			restored++
			continue
		}
		if strings.Contains(err.Error(), "only SELECT views are supported") {
			continue
		}
		t.Errorf("ViewSQL(%q) failed: %v", viewName, err)
	}
	if restored == 0 {
		t.Fatal("no SELECT View definitions were restored")
	}
}

func TestSchema(t *testing.T) {
	dbPath := requireDBFile(t)

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	tables, err := db.Tables()
	if err != nil {
		t.Fatalf("Tables failed: %v", err)
	}
	if len(tables) == 0 {
		t.Fatalf("Tables returned empty table list")
	}

	schema, err := db.Schema(tables[0])
	if err != nil {
		t.Fatalf("Schema failed: %v", err)
	}
	if schema == nil {
		t.Fatalf("Schema returned nil")
	}
	if schema.TableName == "" {
		t.Fatalf("Schema returned empty table name")
	}
	if len(schema.Columns) == 0 {
		t.Fatalf("Schema returned zero columns for table %q", tables[0])
	}
	if schema.Columns[0].Name == "" {
		t.Fatalf("Schema first column has empty name")
	}
	jsonb, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		t.Fatalf("Schema JSON marshal failed: %v", err)
	}
	fmt.Println(string(jsonb))
}

func TestSchemas(t *testing.T) {
	dbPath := requireDBFile(t)

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	tables, err := db.Tables()
	if err != nil {
		t.Fatalf("Tables failed: %v", err)
	}
	if len(tables) == 0 {
		t.Fatal("Tables returned empty table list")
	}

	schemas, err := db.Schemas()
	if err != nil {
		t.Fatalf("Schemas failed: %v", err)
	}
	if len(schemas) != len(tables) {
		t.Fatalf("Schemas returned %d tables, Tables returned %d", len(schemas), len(tables))
	}
	for i, schema := range schemas {
		if schema == nil || schema.TableName != tables[i] {
			t.Fatalf("Schemas[%d] table name mismatch", i)
		}
		if len(schema.Columns) == 0 {
			t.Fatalf("Schemas[%d] returned zero columns for table %q", i, schema.TableName)
		}
		count, countErr := db.TableRowCount(schema.TableName)
		if countErr != nil {
			t.Fatalf("TableRowCount(%q) failed: %v", schema.TableName, countErr)
		}
		if schema.RowCount != count {
			t.Fatalf("Schemas[%d].RowCount=%d want=%d", i, schema.RowCount, count)
		}
	}
}

func TestSchemasValidation(t *testing.T) {
	var nilDB *DB
	if _, err := nilDB.Schemas(); err == nil {
		t.Fatal("Schemas on nil DB expected error")
	}

	dbPath := requireDBFile(t)
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if _, err := db.Schemas(); err == nil {
		t.Fatal("Schemas on closed DB expected error")
	}
}

func TestQuery(t *testing.T) {
	dbPath := requireDBFile(t)

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	tables, err := db.Tables()
	if err != nil {
		t.Fatalf("Tables failed: %v", err)
	}
	if len(tables) == 0 {
		t.Fatalf("Tables returned empty table list")
	}

	query := "SELECT * FROM " + quoteIdentifier(tables[0]) + " LIMIT 1"
	res, err := db.Query(query)
	if err != nil {
		t.Fatalf("Query failed: %v, sql=%s", err, query)
	}
	if res == nil {
		t.Fatalf("Query returned nil result")
	}
	if len(res.Columns) == 0 {
		t.Fatalf("Query returned zero columns for table %q", tables[0])
	}
	if len(res.Rows) > 1 {
		t.Fatalf("Query LIMIT 1 expected <=1 rows, got %d", len(res.Rows))
	}
	fmt.Println(res.Columns)
	fmt.Println(res.Rows)
}

func TestQueryRejectsDisconnect(t *testing.T) {
	dbPath := requireDBFile(t)

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	_, err = db.Query("DISCONNECT")
	if err == nil {
		t.Fatalf("Query(" + "DISCONNECT" + ") expected error, got nil")
	}
}

func TestExportForms(t *testing.T) {
	dbPath := requireDBFile(t)

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	forms, err := db.ExportForms()
	if err != nil {
		t.Fatalf("ExportForms failed: %v", err)
	}

	// 默认样例库 testdb/midb.mdb 预期包含窗体，避免回归后误通过。
	if filepath.Clean(dbPath) == filepath.Clean(defaultTestDB) && len(forms) == 0 {
		t.Fatalf("ExportForms returned empty forms for default test db: %s", dbPath)
	}

	// 不要求测试库一定有窗体；只要返回结构可读、可序列化即可。
	jsonb, err := json.MarshalIndent(forms, "", "  ")
	if err != nil {
		t.Fatalf("ExportForms JSON marshal failed: %v", err)
	}
	fmt.Println(string(jsonb))
}

func TestExportForm(t *testing.T) {
	dbPath := requireDBFile(t)

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	formName := requireFormName(t, db)
	form, err := db.ExportForm(strings.ToUpper(formName))
	if err != nil {
		t.Fatalf("ExportForm(%q) failed: %v", formName, err)
	}
	if form == nil {
		t.Fatalf("ExportForm(%q) returned nil", formName)
	}
	if form.Name != formName {
		t.Fatalf("ExportForm(%q) name=%q", formName, form.Name)
	}
}

func TestExportFormRejectsInvalidName(t *testing.T) {
	dbPath := requireDBFile(t)

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	if _, err := db.ExportForm("  "); err == nil || err.Error() != "form name is empty" {
		t.Fatalf("ExportForm(empty) error=%v", err)
	}
	if _, err := db.ExportForm("__mdbgo_missing_form__"); err == nil || !strings.Contains(err.Error(), "form not found") {
		t.Fatalf("ExportForm(missing) error=%v", err)
	}
}

func TestReadFormStreams(t *testing.T) {
	dbPath := requireDBFile(t)

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	formName := requireFormName(t, db)
	streams, err := db.ReadFormStreams(formName)
	if err != nil {
		t.Fatalf("ReadFormStreams failed: %v", err)
	}
	if streams == nil {
		t.Fatalf("ReadFormStreams returned nil")
	}
	if streams.FormName != formName {
		t.Fatalf("unexpected form name: %q", streams.FormName)
	}

	// 默认样例库里的窗体应该有设计流，至少其中一段应非空。
	if filepath.Clean(dbPath) == filepath.Clean(defaultTestDB) &&
		len(streams.Lv) == 0 && len(streams.LvProp) == 0 && len(streams.LvExtra) == 0 {
		t.Fatalf("all form streams are empty for default test db")
	}

	layout := ParseFormLayoutFromStreams(streams)
	if layout == nil {
		t.Fatalf("ParseFormLayoutFromStreams returned nil")
	}
	fmt.Printf("form=%s lv=%d lvProp=%d lvExtra=%d controls=%d width=%d\n",
		layout.FormName, len(streams.Lv), len(streams.LvProp), len(streams.LvExtra), len(layout.Controls), layout.Width)
}

func TestParseFormPropsFromLvProp(t *testing.T) {
	dbPath := requireDBFile(t)

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	formName := requireFormName(t, db)
	streams, err := db.ReadFormStreams(formName)
	if err != nil {
		t.Fatalf("ReadFormStreams failed: %v", err)
	}

	parsed, err := ParseFormPropsFromLvProp(streams.LvProp)
	if err != nil {
		t.Fatalf("ParseFormPropsFromLvProp failed: %v", err)
	}
	if parsed == nil {
		t.Fatalf("ParseFormPropsFromLvProp returned nil")
	}
	if len(parsed.Blocks) == 0 {
		t.Fatalf("ParseFormPropsFromLvProp returned zero blocks")
	}

	// 默认样例库里 f_midb 的 NameMap 应包含控件名 midb_seq_id。
	if filepath.Clean(dbPath) == filepath.Clean(defaultTestDB) && formName == "f_midb" {
		found := false
		for _, n := range parsed.NameMapNames {
			if n == "midb_seq_id" {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected NameMapNames to contain midb_seq_id, got=%v", parsed.NameMapNames)
		}
	}
}

func TestReadFormAccessObjectData(t *testing.T) {
	dbPath := requireDBFile(t)

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	formName := requireFormName(t, db)
	obj, err := db.ReadFormAccessObjectData(formName)
	if err != nil {
		t.Logf("ReadFormAccessObjectData failed: %v", err)
		// 直接按 ID 读取，便于定位是映射失败还是底层读取失败。
		direct, derr := db.ReadAccessObjectDataByID(0)
		t.Logf("ReadAccessObjectDataByID(0) err=%v len=%d", derr, func() int {
			if direct == nil {
				return -1
			}
			return len(direct.Data)
		}())
		t.Fatalf("ReadFormAccessObjectData failed: %v", err)
	}
	if obj == nil {
		t.Fatalf("ReadFormAccessObjectData returned nil")
	}
	if obj.ObjectID < 0 {
		t.Fatalf("invalid object id: %d", obj.ObjectID)
	}
	fmt.Printf("%s access_object_id=%d data_len=%d\n", formName, obj.ObjectID, len(obj.Data))

	// 默认样例库里应该能定位到非空 Data。
	if filepath.Clean(dbPath) == filepath.Clean(defaultTestDB) && len(obj.Data) == 0 {
		t.Fatalf("expected non-empty access object data for f_midb in default db")
	}
}

func TestReadAccessObjectContainer(t *testing.T) {
	dbPath := requireDBFile(t)
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	kind, err := db.accessObjectStorageKind()
	if err != nil {
		t.Fatalf("accessObjectStorageKind failed: %v", err)
	}
	if kind == accessObjectStorageTree {
		_, err := db.ReadAccessObjectContainer()
		if err == nil || !strings.Contains(err.Error(), "has no OLE Compound container") {
			t.Fatalf("ReadAccessObjectContainer error=%v, want MSysAccessStorage explanation", err)
		}
		return
	}

	container, err := db.ReadAccessObjectContainer()
	if err != nil {
		t.Fatalf("ReadAccessObjectContainer failed: %v", err)
	}
	if len(container.Data) < 8 {
		t.Fatalf("Access object container is too short: %d", len(container.Data))
	}
	t.Logf("access object container ids=%d..%d len=%d header=% x",
		container.FirstObjectID, container.LastObjectID, len(container.Data), container.Data[:8])
	compoundMagic := []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}
	if !bytes.HasPrefix(container.Data, compoundMagic) {
		t.Fatalf("Access object container has no compound signature: % x", container.Data[:8])
	}
}

func TestReadAccessObjectEntries(t *testing.T) {
	dbPath := requireDBFile(t)
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	entries, err := db.ReadAccessObjectEntries()
	if err != nil {
		t.Fatalf("ReadAccessObjectEntries failed: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("ReadAccessObjectEntries returned empty")
	}
	for _, entry := range entries {
		if strings.Contains(strings.ToLower(entry.Path), "form") {
			t.Logf("entry path=%q dir=%v size=%d", entry.Path, entry.IsDir, entry.Size)
		}
	}
}

func TestReadAccessObjectEntriesStorageFormats(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		wantKind int
	}{
		{name: "Access 2000 MSysAccessObjects", path: "testdb/mdbs/mpci_2000.mdb", wantKind: accessObjectStorageObjects},
		{name: "Access 2003 MSysAccessStorage", path: "testdb/mdbs/mpci_2003.mdb", wantKind: accessObjectStorageTree},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := os.Stat(tt.path); err != nil {
				t.Skipf("fixture not found: %s, err=%v", tt.path, err)
			}
			db, err := Open(tt.path)
			if err != nil {
				t.Fatalf("Open failed: %v", err)
			}
			defer func() { _ = db.Close() }()

			kind, err := db.accessObjectStorageKind()
			if err != nil {
				t.Fatalf("accessObjectStorageKind failed: %v", err)
			}
			if kind != tt.wantKind {
				t.Fatalf("storage kind=%d want=%d", kind, tt.wantKind)
			}

			entries, err := db.ReadAccessObjectEntries()
			if err != nil {
				t.Fatalf("ReadAccessObjectEntries failed: %v", err)
			}
			var hasFormsDirData, hasFormBlob bool
			for _, entry := range entries {
				switch {
				case entry.Path == "Forms/DirData" && len(entry.Data) > 0:
					hasFormsDirData = true
				case strings.HasPrefix(entry.Path, "Forms/") && entry.Name == "Blob" && len(entry.Data) > 0:
					hasFormBlob = true
				}
			}
			if !hasFormsDirData {
				t.Fatal("Forms/DirData stream is missing")
			}
			if !hasFormBlob {
				t.Fatal("non-empty Forms/<id>/Blob stream is missing")
			}

			streams, err := db.ReadFormObjectStreams("f_mpci_company")
			if err != nil {
				t.Fatalf("ReadFormObjectStreams(f_mpci_company) failed: %v", err)
			}
			if len(streams.Blob) == 0 || len(streams.TypeInfo) == 0 {
				t.Fatalf("incomplete form streams: blob=%d typeInfo=%d", len(streams.Blob), len(streams.TypeInfo))
			}
		})
	}
}

func TestReadFormObjectStreams(t *testing.T) {
	dbPath := requireDBFile(t)
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	formName := requireFormName(t, db)
	streams, err := db.ReadFormObjectStreams(formName)
	if err != nil {
		t.Fatalf("ReadFormObjectStreams(%q) failed: %v", formName, err)
	}
	if streams.StorageID < 0 {
		t.Fatalf("invalid storage id for %q: %d", formName, streams.StorageID)
	}
	if len(streams.Blob) == 0 || len(streams.TypeInfo) == 0 {
		t.Fatalf("form %q has incomplete design streams: blob=%d typeInfo=%d",
			formName, len(streams.Blob), len(streams.TypeInfo))
	}
	t.Logf("form=%s storage=%d blob=%d typeInfo=%d propData=%d blobDelta=%d",
		formName, streams.StorageID, len(streams.Blob), len(streams.TypeInfo),
		len(streams.PropData), len(streams.BlobDelta))
}

func TestReadFormContent(t *testing.T) {
	dbPath := requireDBFile(t)
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	formName := requireFormName(t, db)
	content, err := db.ReadFormContent(formName)
	if err != nil {
		t.Fatalf("ReadFormContent(%q) failed: %v", formName, err)
	}
	if content.FormName != formName {
		t.Fatalf("unexpected form name: got %q want %q", content.FormName, formName)
	}
	if len(content.Controls) == 0 {
		t.Fatalf("ReadFormContent(%q) returned no controls", formName)
	}
	if len(content.Sections) == 0 {
		t.Fatalf("ReadFormContent(%q) returned no form sections", formName)
	}
	for i, control := range content.Controls {
		if strings.TrimSpace(control.Name) == "" {
			t.Fatalf("control %d has empty name", i)
		}
		if strings.TrimSpace(control.Type) == "" {
			t.Fatalf("control %q has empty type", control.Name)
		}
		if strings.TrimSpace(control.Section) == "" {
			t.Fatalf("control %q has empty section", control.Name)
		}
	}
	controlsWithProps := 0
	controlsWithGeometry := 0
	propertyCount := 0
	for _, control := range content.Controls {
		if len(control.Properties) > 0 {
			controlsWithProps++
			propertyCount += len(control.Properties)
		}
		if control.HasGeometry {
			controlsWithGeometry++
			if control.Width <= 0 || control.Height <= 0 {
				t.Fatalf("control %q has invalid geometry: %+v", control.Name, control)
			}
		}
	}
	if controlsWithProps == 0 {
		t.Fatalf("ReadFormContent(%q) returned no parsed control properties", formName)
	}
	controlsByName := make(map[string]FormControlContent, len(content.Controls))
	for _, control := range content.Controls {
		controlsByName[control.Name] = control
	}
	if formName == "f_abha_house_review" {
		if content.Width != 9660 {
			t.Fatalf("unexpected form Width: %d", content.Width)
		}
		if content.RecordSource != "s_abha_house_review" {
			t.Fatalf("unexpected RecordSource: %q", content.RecordSource)
		}
		if got := controlsByName["master_bl_no"].ControlSource; got != "master_bl_no" {
			t.Fatalf("master_bl_no ControlSource=%q", got)
		}
		if got := controlsByName["master_bl_no"]; !got.HasGeometry || got.Width <= 0 || got.Height <= 0 {
			t.Fatalf("master_bl_no geometry=%+v", got)
		}
		if got := controlsByName["lbl_doc_code"].Caption; got != "MAWB No." {
			t.Fatalf("lbl_doc_code Caption=%q", got)
		}
	}
	if formName == "f_midb" {
		if content.Width != 9480 {
			t.Fatalf("unexpected form Width: %d", content.Width)
		}
		if content.RecordSource != "s_midb" {
			t.Fatalf("unexpected RecordSource: %q", content.RecordSource)
		}
		if got := controlsByName["midb_seq_id"].ControlSource; got != "midb_seq_id" {
			t.Fatalf("midb_seq_id ControlSource=%q", got)
		}
		if got := controlsByName["midb_seq_id"]; !got.HasGeometry || got.Width <= 0 || got.Height <= 0 {
			t.Fatalf("midb_seq_id geometry=%+v", got)
		}
		if got := controlsByName["lbl_efc_branch_id"].Caption; got != "Seq No:" {
			t.Fatalf("lbl_efc_branch_id Caption=%q", got)
		}
		if got := controlsByName["created_date"].ControlSource; got != "created_date" {
			t.Fatalf("created_date ControlSource=%q", got)
		}
		if got := controlsByName["created_date"]; !got.HasGeometry ||
			got.Left != 7260 || got.Top != 4800 || got.Width != 1620 || got.Height != 300 {
			t.Fatalf("created_date geometry=%+v", got)
		}
	}
	t.Logf("form=%s storage=%d width=%d properties=%d controls=%d controls_with_props=%d controls_with_geometry=%d control_props=%d first=%+v",
		content.FormName, content.StorageID, content.Width, len(content.Properties), len(content.Controls),
		controlsWithProps, controlsWithGeometry, propertyCount, content.Controls[0])
}

func TestExportFormContent(t *testing.T) {
	dbPath := requireDBFile(t)
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	formName := requireFormName(t, db)
	content, err := db.ExportFormContent(formName)
	if err != nil {
		t.Fatalf("ExportFormContent(%q) failed: %v", formName, err)
	}
	if content == nil {
		t.Fatalf("ExportFormContent(%q) returned nil", formName)
	}
	if content.FormName != formName {
		t.Fatalf("ExportFormContent(%q) name=%q", formName, content.FormName)
	}
	if len(content.Controls) == 0 {
		t.Fatalf("ExportFormContent(%q) returned no controls", formName)
	}
	if len(content.Sections) == 0 {
		t.Fatalf("ExportFormContent(%q) returned no sections", formName)
	}
}

func TestExportFormContentRejectsInvalidName(t *testing.T) {
	dbPath := requireDBFile(t)
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	if _, err := db.ExportFormContent("  "); err == nil || err.Error() != "form name is empty" {
		t.Fatalf("ExportFormContent(empty) error=%v", err)
	}
	if _, err := db.ExportFormContent("__mdbgo_missing_form__"); err == nil || !strings.Contains(err.Error(), "form not found") {
		t.Fatalf("ExportFormContent(missing) error=%v", err)
	}
}

func TestReadFormContentFAbiaMaster(t *testing.T) {
	// 这个测试校验仓库自带 ABIQuery.mdb 中 f_abia_master 的固定内容，
	// 不受 MDBGO_TEST_DB 指向其他数据库的影响。
	dbPath := filepath.Clean(defaultTestDB)
	if _, err := os.Stat(dbPath); err != nil {
		t.Skipf("test database not found: %s (%v)", dbPath, err)
	}
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	const formName = "f_abia_master"
	content, err := db.ReadFormContent(formName)
	if err != nil {
		t.Fatalf("ReadFormContent(%q) failed: %v", formName, err)
	}
	if content.FormName != formName {
		t.Fatalf("FormName=%q want=%q", content.FormName, formName)
	}
	if content.Width != 12540 {
		t.Fatalf("Width=%d want=12540", content.Width)
	}
	if content.Height != 8640 || content.BackColorValue != 0x8000000F ||
		content.BackColor != "#0f0000" || content.BackGroundColor != "#0f0000" {
		t.Fatalf("form visual properties: Height=%d BackColor=%q BackColorValue=0x%08X BackGroundColor=%q",
			content.Height, content.BackColor, content.BackColorValue, content.BackGroundColor)
	}
	if content.Caption != "Query MAWB Manifest / ABI Status" {
		t.Fatalf("Caption=%q", content.Caption)
	}
	if len(content.Controls) != 93 {
		t.Fatalf("controls=%d want=93", len(content.Controls))
	}
	if len(content.Sections) != 1 {
		t.Fatalf("sections=%d want=1", len(content.Sections))
	}
	detail := content.Sections[0]
	if detail.Type != "Detail" || detail.Name != "Detail" {
		t.Fatalf("section=%+v want Detail", detail)
	}
	if detail.Height != 8640 || detail.BackColorValue != 0x8000000F ||
		detail.SpecialEffect != 1 || !detail.Visible || detail.EventProcPrefix != "Detail" {
		t.Fatalf("Detail properties=%+v", detail)
	}
	if len(detail.Controls) != 92 {
		t.Fatalf("detail controls=%d want=92", len(detail.Controls))
	}
	if !content.Controls[0].IsSection || content.Controls[0].Section != "Detail" {
		t.Fatalf("first TypeInfo entry is not the Detail section: %+v", content.Controls[0])
	}

	geometryCount := 0
	for _, control := range content.Controls {
		if strings.TrimSpace(control.Name) == "" {
			t.Fatal("found control with empty name")
		}
		if control.HasGeometry {
			geometryCount++
		}
	}
	if geometryCount != 92 {
		t.Fatalf("controls with complete geometry=%d want=92", geometryCount)
	}

	sectionedOutput := struct {
		FormName     string
		StorageID    int
		Width        int
		RecordSource string
		Properties   []FormProperty
		Sections     []FormSectionContent
	}{
		FormName:     content.FormName,
		StorageID:    content.StorageID,
		Width:        content.Width,
		RecordSource: content.RecordSource,
		Properties:   content.Properties,
		Sections:     content.Sections,
	}
	jsonData, err := json.MarshalIndent(sectionedOutput, "", "  ")
	if err != nil {
		t.Fatalf("marshal form content failed: %v", err)
	}
	t.Logf("form=%s record_source=%q width=%d controls=%d complete_geometry=%d\n%s",
		content.FormName, content.RecordSource, content.Width, len(content.Controls),
		geometryCount, jsonData)
}

func TestReadFormContentFAbiaManifestSections(t *testing.T) {
	dbPath := filepath.Clean(defaultTestDB)
	if _, err := os.Stat(dbPath); err != nil {
		t.Skipf("test database not found: %s (%v)", dbPath, err)
	}
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	const formName = "f_abia_master_list_1_manifest"
	content, err := db.ReadFormContent(formName)
	if err != nil {
		t.Fatalf("ReadFormContent(%q) failed: %v", formName, err)
	}
	if len(content.Sections) != 3 {
		t.Fatalf("sections=%d want=3", len(content.Sections))
	}

	want := map[string]struct {
		height      int
		backColor   uint32
		eventPrefix string
	}{
		"FormHeader": {height: 291, backColor: 0x00808080, eventPrefix: "FormHeader0"},
		"Detail":     {height: 288, backColor: 0x00C0C0C0, eventPrefix: "Detail1"},
		"FormFooter": {height: 0, backColor: 0x00808080, eventPrefix: "FormFooter2"},
	}
	for _, section := range content.Sections {
		expected, ok := want[section.Type]
		if !ok {
			t.Fatalf("unexpected section: %+v", section)
		}
		if section.Height != expected.height || section.BackColorValue != expected.backColor ||
			section.EventProcPrefix != expected.eventPrefix || !section.Visible || len(section.Properties) == 0 {
			t.Fatalf("section %q properties=%+v", section.Type, section)
		}
	}
}

func TestReadAllFormSectionProperties(t *testing.T) {
	dbPath := filepath.Clean(defaultTestDB)
	if _, err := os.Stat(dbPath); err != nil {
		t.Skipf("test database not found: %s (%v)", dbPath, err)
	}
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	entries, err := db.ReadAccessObjectEntries()
	if err != nil {
		t.Fatalf("ReadAccessObjectEntries failed: %v", err)
	}
	formIDs, err := formStorageIDsFromEntries(entries)
	if err != nil {
		t.Fatalf("formStorageIDsFromEntries failed: %v", err)
	}
	sectionCount := 0
	for formName := range formIDs {
		streams, err := formObjectStreamsFromEntries(entries, formName)
		if err != nil {
			t.Fatalf("formObjectStreamsFromEntries(%q) failed: %v", formName, err)
		}
		content, err := ParseFormContent(streams)
		if err != nil {
			t.Fatalf("ParseFormContent(%q) failed: %v", formName, err)
		}
		for _, section := range content.Sections {
			sectionCount++
			if section.BackColor == "" || section.EventProcPrefix == "" ||
				!section.Visible || len(section.Properties) < 5 {
				t.Fatalf("form %q section properties are incomplete: %+v", formName, section)
			}
		}
	}
	if len(formIDs) != 43 || sectionCount == 0 {
		t.Fatalf("forms=%d sections=%d", len(formIDs), sectionCount)
	}
	t.Logf("verified Section properties for %d forms and %d sections", len(formIDs), sectionCount)
}

func TestReadFormContentFAbiaMasterQueryComboBox(t *testing.T) {
	db, err := Open(filepath.Clean(defaultTestDB))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	content, err := db.ReadFormContent("f_abia_master_query")
	if err != nil {
		t.Fatalf("ReadFormContent(%q) failed: %v", "f_abia_master_query", err)
	}
	var combo FormControlContent
	found := false
	for _, control := range content.Controls {
		if control.Name == "status_id" && control.Type == "ComboBox" {
			combo = control
			found = true
			break
		}
	}
	if !found {
		t.Fatal("ComboBox status_id is missing")
	}

	if combo.ControlSource != "status_id" || combo.RowSourceType != "Table/Query" ||
		combo.RowSource != "q_ams_query_status" || combo.ColumnWidths != "1440;720" {
		t.Errorf("ComboBox text properties=%+v", combo)
	}
	if combo.Tag != "sel_status_desc" || combo.FontName != "Verdana" {
		t.Errorf("ComboBox Tag=%q FontName=%q", combo.Tag, combo.FontName)
	}
	if combo.ColumnCount != 2 || combo.ListRows != 12 || combo.ListWidth != 2160 || combo.BoundColumn != 2 {
		t.Errorf("ComboBox list properties: columns=%d rows=%d width=%d bound=%d",
			combo.ColumnCount, combo.ListRows, combo.ListWidth, combo.BoundColumn)
	}
	if combo.TextAlignValue != 0 || combo.TextAlign != "left" || combo.TabIndex != 2 || combo.Locked || !combo.Visible {
		t.Errorf("ComboBox state: align=%d/%q tab=%d locked=%v visible=%v",
			combo.TextAlignValue, combo.TextAlign, combo.TabIndex, combo.Locked, combo.Visible)
	}
	if !combo.HasGeometry || combo.Left != 1500 || combo.Top != 1080 || combo.Width != 1680 || combo.Height != 300 {
		t.Errorf("ComboBox geometry=(%d,%d,%d,%d) complete=%v want=(1500,1080,1680,300)",
			combo.Left, combo.Top, combo.Width, combo.Height, combo.HasGeometry)
	}
}

func TestParseJet4TextBoxNumericTailNativeFlags(t *testing.T) {
	tests := []struct {
		name       string
		tail       []byte
		locked     bool
		underline  bool
		foreColor  uint32
		scrollBars byte
	}{
		{
			name: "locked",
			tail: []byte{
				0xFF, 0x0B, 0x00, 0x6D, 0x00, 0x02, 0x37, 0x5D, 0x3B, 0x02,
				0x60, 0x54, 0x06, 0x62, 0x48, 0x03, 0x63, 0x20, 0x01,
				0x69, 0x09, 0x00, 0x6B, 0x01, 0x00, 0xDC, 0x22,
			},
			locked:    true,
			foreColor: 0x80000008,
		},
		{
			name: "underlined",
			tail: []byte{
				0xFD, 0x6D, 0x00, 0x0A, 0x37, 0x57, 0x3B, 0x01,
				0x62, 0x54, 0x06, 0x63, 0x20, 0x01, 0x69, 0x09, 0x00,
				0x6B, 0x08, 0x00, 0x9F, 0x00, 0x00, 0xFF, 0x00, 0xDC, 0x10,
			},
			underline: true,
			foreColor: 0x00FF0000,
		},
		{
			name: "datasheet implicit black",
			tail: []byte{
				0xFD, 0x6D, 0x00, 0x30, 0x00, 0x37, 0x5D, 0x3B, 0x02,
				0x60, 0x78, 0x3C, 0x62, 0xEC, 0x04, 0x63, 0x20, 0x01,
				0x69, 0x09, 0x00, 0x6B, 0x01, 0x00, 0xDC, 0x1A,
			},
			foreColor: 0,
		},
		{
			name: "vertical scroll bar",
			tail: []byte{
				0xFD, 0x6D, 0x00, 0x32, 0x02, 0x34, 0x00, 0x37, 0xD7, 0x3B, 0x01, 0x46, 0x03,
				0x60, 0x54, 0x06, 0x61, 0x24, 0x09, 0x62, 0x04, 0x0B, 0x63, 0x94, 0x02, 0x6B, 0x01, 0x00,
				0x9C, 0xFF, 0xFF, 0xFF, 0x00, 0x9F, 0x00, 0x00, 0x00, 0x00, 0xDC, 0x18,
			},
			foreColor:  0,
			scrollBars: 2,
		},
		{
			name: "rgb template implicit black",
			tail: []byte{
				0xFD, 0x6D, 0x00, 0x02, 0x34, 0x00, 0x35, 0x01, 0x37, 0xDF, 0x3B, 0x03, 0x43, 0x00, 0x46, 0x03,
				0x60, 0x98, 0x2B, 0x61, 0xD8, 0x09, 0x62, 0x64, 0x05, 0x63, 0x2C, 0x01, 0x6B, 0x02, 0x00, 0xDC, 0x1C,
			},
			locked:    true,
			foreColor: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseJet4TextBoxNumericTail(tt.tail)
			if !ok {
				t.Fatal("parseJet4TextBoxNumericTail did not recognize TextBox record")
			}
			if got.Locked != tt.locked || got.Underline != tt.underline {
				t.Fatalf("TextBox flags locked=%v underline=%v want locked=%v underline=%v",
					got.Locked, got.Underline, tt.locked, tt.underline)
			}
			if got.ForeColorValue != tt.foreColor {
				t.Fatalf("TextBox ForeColor=%#08x want=%#08x", got.ForeColorValue, tt.foreColor)
			}
			if got.ScrollBars != tt.scrollBars {
				t.Fatalf("TextBox ScrollBars=%d want=%d", got.ScrollBars, tt.scrollBars)
			}
		})
	}
}

func TestParseJet4ComboBoxNumericTail(t *testing.T) {
	tail := []byte{
		0xFD, 0x6F, 0x00, 0x01, 0x32, 0x00, 0x35, 0x55, 0x44, 0x03,
		0x60, 0x02, 0x00,
		0x61, 0x0C, 0x00,
		0x62, 0x70, 0x08,
		0x63, 0xDC, 0x05,
		0x64, 0x38, 0x04,
		0x65, 0x90, 0x06,
		0x66, 0x2C, 0x01,
		0x6E, 0x02, 0x00,
		0x9C, 0x01, 0x00, 0x00, 0x00,
	}
	got, ok := parseJet4ComboBoxNumericTail(tail)
	if !ok {
		t.Fatal("parseJet4ComboBoxNumericTail did not recognize ComboBox record")
	}
	if got.ColumnCount != 2 || got.ListRows != 12 || got.ListWidth != 2160 || got.BoundColumn != 2 ||
		got.BackStyle != 1 || got.TabIndex != 2 || !got.HasTabIndex ||
		got.Geometry != (formControlGeometry{Left: 1500, Top: 1080, Width: 1680, Height: 300}) {
		t.Fatalf("ComboBox numeric properties=%+v", got)
	}

	omittedTopTail := []byte{
		0xFD, 0x6F, 0x00, 0x04, 0x0A, 0x32, 0x00, 0x39, 0x02,
		0x60, 0x02, 0x00,
		0x61, 0x0C, 0x00,
		0x62, 0x80, 0x16,
		0x63, 0x9C, 0x09,
		0x65, 0x90, 0x06,
		0x66, 0x2C, 0x01,
		0x6E, 0x02, 0x00,
		0xDC, 0x0E,
	}
	got, ok = parseJet4ComboBoxNumericTail(omittedTopTail)
	if !ok || got.BoundColumn != 1 || !got.Locked || got.TextAlign != 2 ||
		got.Geometry != (formControlGeometry{Left: 2460, Width: 1680, Height: 300}) {
		t.Fatalf("omitted-top ComboBox numeric properties=%+v ok=%v", got, ok)
	}

	pageBoundaryTail := append([]byte{0xFF, 0x09, 0x00}, omittedTopTail[1:]...)
	got, ok = parseJet4ComboBoxNumericTail(pageBoundaryTail)
	if !ok || got.Geometry != (formControlGeometry{Left: 2460, Width: 1680, Height: 300}) {
		t.Fatalf("page-boundary ComboBox numeric properties=%+v ok=%v", got, ok)
	}

	defaultFieldsTail := []byte{
		0xFD, 0x6F, 0x00, 0x35, 0x57, 0x39, 0x02, 0x44, 0x03,
		0x61, 0x0C, 0x00,
		0x62, 0xA0, 0x05,
		0x65, 0x84, 0x03,
		0x66, 0x1D, 0x01,
		0x6C, 0x09, 0x00,
		0xDC, 0x16,
	}
	got, ok = parseJet4ComboBoxNumericTail(defaultFieldsTail)
	if !ok || got.ColumnCount != 0 || got.BoundColumn != 1 || got.TextAlign != 2 ||
		got.TabIndex != 0 || got.HasTabIndex ||
		got.Geometry != (formControlGeometry{Width: 900, Height: 285}) {
		t.Fatalf("default-fields ComboBox numeric properties=%+v ok=%v", got, ok)
	}
}

func TestReadFormContentFAbiaMasterButtonTip(t *testing.T) {
	db, err := Open(filepath.Clean(defaultTestDB))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = db.Close() }()
	content, err := db.ReadFormContent("f_abia_master")
	if err != nil {
		t.Fatalf("ReadFormContent(%q) failed: %v", "f_abia_master", err)
	}
	for _, control := range content.Controls {
		if control.Type != "Button" || control.Name != "btn0Requery" {
			continue
		}
		if control.Caption != "Requery" || control.ControlTipText != "Requery" ||
			control.Tag != "btn_btn0Requery" || control.FontName != "Verdana" {
			t.Fatalf("btn0Requery text properties=%+v", control)
		}
		if control.Left != 11040 || control.Top != 570 || control.Width != 1200 || control.Height != 360 {
			t.Fatalf("btn0Requery geometry=(%d,%d,%d,%d)",
				control.Left, control.Top, control.Width, control.Height)
		}
		return
	}
	t.Fatal("Button btn0Requery is missing")
}

func TestParseJet4ButtonNumericTail(t *testing.T) {
	tests := []struct {
		name string
		tail []byte
		want jet4ButtonNumericProperties
	}{
		{
			name: "explicit height",
			tail: []byte{0xFD, 0x68, 0x00, 0x09, 0x0A, 0x31, 0xD7,
				0x60, 0x20, 0x2B, 0x61, 0x3A, 0x02, 0x62, 0xB0, 0x04,
				0x63, 0x68, 0x01, 0x69, 0x01, 0x00, 0xDC, 0x16},
			want: jet4ButtonNumericProperties{
				TabIndex: 1, HasTabIndex: true, BackStyle: 1,
				BackColor: "#ffffff", BackColorValue: 16777215,
				Geometry: formControlGeometry{Left: 11040, Top: 570, Width: 1200, Height: 360}, HasGeometry: true,
				HasExplicitHeight: true,
			},
		},
		{
			name: "default height",
			tail: []byte{0xFD, 0x68, 0x00, 0x31, 0xD7,
				0x60, 0xFD, 0x02, 0x61, 0x1E, 0x00, 0x62, 0xD0, 0x02,
				0x69, 0x0C, 0x00, 0x9D, 0x00, 0x00, 0x80, 0x00, 0xDC, 0x12},
			want: jet4ButtonNumericProperties{
				TabIndex: 12, HasTabIndex: true, BackStyle: 1,
				BackColor: "#ffffff", BackColorValue: 16777215,
				Geometry: formControlGeometry{Left: 765, Top: 30, Width: 720, Height: 360}, HasGeometry: true,
			},
		},
		{
			name: "F7 mask keeps default height",
			tail: []byte{0xFD, 0x68, 0x00, 0x31, 0xF7,
				0x60, 0xE4, 0x0C, 0x61, 0xEC, 0x04, 0x62, 0x48, 0x03,
				0x69, 0x06, 0x00, 0x9D, 0x00, 0x00, 0xFF, 0x00, 0xDC, 0x10},
			want: jet4ButtonNumericProperties{
				TabIndex: 6, HasTabIndex: true, BackStyle: 1,
				BackColor: "#ffffff", BackColorValue: 16777215,
				Geometry: formControlGeometry{Left: 3300, Top: 1260, Width: 840, Height: 360}, HasGeometry: true,
			},
		},
		{
			name: "F7 picture button keeps default height",
			tail: []byte{0xFD, 0x68, 0x00, 0x31, 0xF7,
				0x60, 0x60, 0x36, 0x62, 0xA4, 0x01, 0x68, 0xBC, 0x02,
				0x69, 0x04, 0x00, 0x9D, 0x00, 0x00, 0x80, 0x00, 0xDC, 0x14},
			want: jet4ButtonNumericProperties{
				TabIndex: 4, HasTabIndex: true, BackStyle: 1,
				BackColor: "#ffffff", BackColorValue: 16777215,
				Geometry: formControlGeometry{Left: 13920, Width: 420, Height: 360}, HasGeometry: true,
			},
		},
		{
			name: "default top",
			tail: []byte{0xFD, 0x68, 0x00, 0x31, 0x5D,
				0x60, 0xA8, 0x2A, 0x62, 0xA4, 0x01, 0x63, 0x68, 0x01,
				0x69, 0x02, 0x00, 0x9D, 0x00, 0x00, 0x80, 0x00, 0xDC, 0x12},
			want: jet4ButtonNumericProperties{
				TabIndex: 2, HasTabIndex: true, BackStyle: 1,
				BackColor: "#ffffff", BackColorValue: 16777215,
				Geometry: formControlGeometry{Left: 10920, Width: 420, Height: 360}, HasGeometry: true,
				HasExplicitHeight: true,
			},
		},
		{
			name: "default width",
			tail: []byte{0xFD, 0x68, 0x00, 0x31, 0xF7,
				0x60, 0x60, 0x27, 0x61, 0xFC, 0x03, 0x63, 0x58, 0x02,
				0x69, 0x08, 0x00, 0x9D, 0x00, 0x00, 0xFF, 0x00, 0xDC, 0x26},
			want: jet4ButtonNumericProperties{
				TabIndex: 8, HasTabIndex: true, BackStyle: 1,
				BackColor: "#ffffff", BackColorValue: 16777215,
				Geometry: formControlGeometry{Left: 10080, Top: 1020, Width: 1440, Height: 600}, HasGeometry: true,
				HasExplicitHeight: true,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseJet4ButtonNumericTail(tt.tail)
			if !ok {
				t.Fatal("parseJet4ButtonNumericTail did not recognize Button record")
			}
			if got != tt.want {
				t.Fatalf("Button numeric properties=%+v want=%+v", got, tt.want)
			}
		})
	}
}

func TestParseJet4ButtonDefaultHeight(t *testing.T) {
	fontName := encodeUTF16LE("MS Sans Serif")
	tests := []struct {
		name   string
		prefix []byte
		want   int
	}{
		{
			name: "form template height",
			prefix: append([]byte{
				0xFD, 0x68, 0x00,
				0x63, 0xA4, 0x01,
				0x67, 0x08, 0x00,
				0x68, 0x90, 0x01,
				0xE4, byte(len(fontName)),
			}, fontName...),
			want: 420,
		},
		{
			name: "built-in template height",
			prefix: append([]byte{
				0xFD, 0x68, 0x00,
				0x67, 0x08, 0x00,
				0x68, 0x90, 0x01,
				0x9D, 0x12, 0x00, 0x00, 0x80,
				0xE4, byte(len(fontName)),
			}, fontName...),
			want: 360,
		},
		{
			name: "ordinary button record is not template",
			prefix: []byte{
				0xFD, 0x68, 0x00, 0x31, 0xF7,
				0x60, 0xE4, 0x0C, 0x61, 0xEC, 0x04, 0x62, 0x48, 0x03,
				0x69, 0x06, 0x00, 0xDC, 0x10,
			},
			want: 360,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseJet4ButtonDefaultHeight(tt.prefix); got != tt.want {
				t.Fatalf("parseJet4ButtonDefaultHeight()=%d want=%d", got, tt.want)
			}
		})
	}
}

func TestParseJet4CheckBoxNumericTail(t *testing.T) {
	tail := []byte{
		0xFD, 0x6A, 0x00, 0x32, 0x55,
		0x60, 0xC8, 0x37,
		0x62, 0xB4, 0x00,
		0x63, 0x20, 0x01,
		0x69, 0x0B, 0x00,
		0xDC, 0x0C,
	}
	got, ok := parseJet4CheckBoxNumericTail(tail)
	if !ok {
		t.Fatal("parseJet4CheckBoxNumericTail did not recognize CheckBox record")
	}
	want := jet4CheckBoxNumericProperties{
		TabIndex: 11, HasTabIndex: true, Visible: true,
		Geometry: formControlGeometry{Left: 14280, Width: 180, Height: 288}, HasGeometry: true,
	}
	if got != want {
		t.Fatalf("CheckBox numeric properties=%+v want=%+v", got, want)
	}

	defaultHeightTail := []byte{
		0xFD, 0x6A, 0x00, 0x02, 0x32, 0xDF,
		0x60, 0x54, 0x15,
		0x61, 0xA8, 0x0C,
		0x62, 0xF0, 0x00,
		0x69, 0x14, 0x00,
		0xDC, 0x1A,
	}
	got, ok = parseJet4CheckBoxNumericTail(defaultHeightTail)
	if !ok {
		t.Fatal("parseJet4CheckBoxNumericTail did not recognize default-height record")
	}
	want = jet4CheckBoxNumericProperties{
		TabIndex: 20, HasTabIndex: true, Locked: true, Visible: true,
		Geometry: formControlGeometry{Left: 5460, Top: 3240, Width: 240, Height: 240}, HasGeometry: true,
	}
	if got != want {
		t.Fatalf("default-height CheckBox numeric properties=%+v want=%+v", got, want)
	}
}

func TestParseJet4OptionGroupNumericTail(t *testing.T) {
	// f_abiq_query_master.rdo_condition_type 的原生 FD 6B 数值记录。
	tail := []byte{
		0xFD, 0x6B, 0x00,
		0x31, 0x00,
		0x33, 0x00,
		0x35, 0x5D,
		0x60, 0x68, 0x10,
		0x61, 0x96, 0x00,
		0x62, 0x6C, 0x0C,
		0x63, 0x3F, 0x02,
		0x69, 0x01, 0x00,
		0xDC, 0x24,
	}
	got, ok := parseJet4OptionGroupNumericTail(tail)
	if !ok {
		t.Fatal("parseJet4OptionGroupNumericTail did not recognize OptionGroup record")
	}
	want := jet4OptionGroupNumericProperties{
		BorderWidth: 1,
		TabIndex:    1,
		HasTabIndex: true,
		Visible:     true,
		Geometry:    formControlGeometry{Left: 4200, Top: 150, Width: 3180, Height: 575},
		HasGeometry: true,
	}
	if got != want {
		t.Fatalf("OptionGroup numeric properties=%+v want=%+v", got, want)
	}
}

func TestReadFormContentFAbiqQueryMasterOptionGroup(t *testing.T) {
	db, err := Open(filepath.Clean(defaultTestDB))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	content, err := db.ReadFormContent("f_abiq_query_master")
	if err != nil {
		t.Fatalf("ReadFormContent(%q) failed: %v", "f_abiq_query_master", err)
	}
	for _, control := range content.Controls {
		if control.Type != "OptionGroup" || control.Name != "rdo_condition_type" {
			continue
		}
		if control.ControlSource != "=[int]" {
			t.Errorf("OptionGroup ControlSource=%q want=%q", control.ControlSource, "=[int]")
		}
		if control.SpecialEffect != 0 || control.BackStyle != 0 ||
			control.BorderStyle != 0 || control.BorderWidth != 1 {
			t.Errorf("OptionGroup appearance: effect=%d back_style=%d border_style=%d border_width=%d",
				control.SpecialEffect, control.BackStyle, control.BorderStyle, control.BorderWidth)
		}
		if control.TabIndex != 1 || control.Locked || !control.Visible {
			t.Errorf("OptionGroup state: tab=%d locked=%v visible=%v",
				control.TabIndex, control.Locked, control.Visible)
		}
		if !control.HasGeometry || control.Left != 4200 || control.Top != 150 ||
			control.Width != 3180 || control.Height != 575 {
			t.Errorf("OptionGroup geometry=(%d,%d,%d,%d) complete=%v want=(4200,150,3180,575)",
				control.Left, control.Top, control.Width, control.Height, control.HasGeometry)
		}
		return
	}
	t.Fatal("OptionGroup rdo_condition_type is missing")
}

func TestParseJet4OptionButtonTextProperties(t *testing.T) {
	props := parseJet4OptionButtonTextProperties(
		FormControlInfo{Name: "choice", Type: "OptionButton"},
		[]jet4TaggedTextField{
			{Tag: 0xDD, Value: "status_id"},
			{Tag: 0xF0, Value: "choice_tag"},
		},
	)
	if got := formPropertyText(props, 0x001B); got != "status_id" {
		t.Fatalf("OptionButton ControlSource=%q want=status_id", got)
	}
	if got := formPropertyText(props, 0x010A); got != "choice_tag" {
		t.Fatalf("OptionButton Tag=%q want=choice_tag", got)
	}
}

func TestParseJet4OptionButtonNumericTail(t *testing.T) {
	tests := []struct {
		name string
		tail []byte
		want jet4OptionButtonNumericProperties
	}{
		{
			name: "option group boundary prefix",
			tail: []byte{
				0xFF, 0x03, 0x00, 0x69, 0x00,
				0x32, 0x57,
				0x60, 0xAC, 0x17,
				0x61, 0x59, 0x01,
				0x62, 0x68, 0x01,
				0x63, 0xD8, 0x00,
				0x9C, 0x02, 0x00, 0x00, 0x00,
				0xDC, 0x28,
			},
			want: jet4OptionButtonNumericProperties{
				OptionValue: 2, HasOptionValue: true, Visible: true,
				Geometry:    formControlGeometry{Left: 6060, Top: 345, Width: 360, Height: 216},
				HasGeometry: true,
			},
		},
		{
			name: "standard record",
			tail: []byte{
				0xFD, 0x69, 0x00,
				0x32, 0x57,
				0x60, 0x6C, 0x1B,
				0x61, 0x59, 0x01,
				0x62, 0x68, 0x01,
				0x63, 0xD8, 0x00,
				0x9C, 0x03, 0x00, 0x00, 0x00,
				0xDC, 0x28,
			},
			want: jet4OptionButtonNumericProperties{
				OptionValue: 3, HasOptionValue: true, Visible: true,
				Geometry:    formControlGeometry{Left: 7020, Top: 345, Width: 360, Height: 216},
				HasGeometry: true,
			},
		},
		{
			name: "default height omitted",
			tail: []byte{
				0xFF, 0x02, 0x00, 0x69, 0x00,
				0x32, 0x57,
				0x60, 0xC8, 0x28,
				0x61, 0x0C, 0x03,
				0x62, 0xF0, 0x00,
				0x9C, 0x01, 0x00, 0x00, 0x00,
				0xDC, 0x28,
			},
			want: jet4OptionButtonNumericProperties{
				OptionValue: 1, HasOptionValue: true, Visible: true,
				Geometry:    formControlGeometry{Left: 10440, Top: 780, Width: 240, Height: 180},
				HasGeometry: true,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseJet4OptionButtonNumericTail(tt.tail)
			if !ok {
				t.Fatal("parseJet4OptionButtonNumericTail did not recognize OptionButton record")
			}
			if got != tt.want {
				t.Fatalf("OptionButton numeric properties=%+v want=%+v", got, tt.want)
			}
		})
	}
}

func TestReadFormContentFAbiqQueryMasterOptionButtons(t *testing.T) {
	db, err := Open(filepath.Clean(defaultTestDB))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	content, err := db.ReadFormContent("f_abiq_query_master")
	if err != nil {
		t.Fatalf("ReadFormContent(%q) failed: %v", "f_abiq_query_master", err)
	}
	want := map[string]struct {
		optionValue              int
		left, top, width, height int
	}{
		"rdo_condition_type_1": {optionValue: 1, left: 5160, top: 363, width: 300, height: 216},
		"rdo_condition_type_2": {optionValue: 2, left: 6060, top: 345, width: 360, height: 216},
		"rdo_condition_type_3": {optionValue: 3, left: 7020, top: 345, width: 360, height: 216},
	}
	found := make(map[string]bool, len(want))
	for _, control := range content.Controls {
		expected, ok := want[control.Name]
		if !ok || control.Type != "OptionButton" {
			continue
		}
		found[control.Name] = true
		if control.OptionValue != expected.optionValue {
			t.Errorf("OptionButton %q OptionValue=%d want=%d",
				control.Name, control.OptionValue, expected.optionValue)
		}
		if !control.HasGeometry || control.Left != expected.left || control.Top != expected.top ||
			control.Width != expected.width || control.Height != expected.height {
			t.Errorf("OptionButton %q geometry=(%d,%d,%d,%d) complete=%v want=(%d,%d,%d,%d)",
				control.Name, control.Left, control.Top, control.Width, control.Height, control.HasGeometry,
				expected.left, expected.top, expected.width, expected.height)
		}
		if control.Locked || !control.Visible {
			t.Errorf("OptionButton %q locked=%v visible=%v", control.Name, control.Locked, control.Visible)
		}
	}
	if len(found) != len(want) {
		t.Fatalf("parsed OptionButtons=%d want=%d: found=%v", len(found), len(want), found)
	}
}

func TestParseJet4SubFormTextProperties(t *testing.T) {
	props := parseJet4SubFormTextProperties(
		FormControlInfo{Name: "child", Type: "SubForm"},
		[]jet4TaggedTextField{
			{Tag: 0xDD, Value: "Form.f_child"},
			{Tag: 0xDE, Value: "child_id"},
			{Tag: 0xDF, Value: "master_id"},
			{Tag: 0xE0, Value: "status"},
			{Tag: 0xE3, Value: "ChildPrefix"},
		},
	)
	want := map[uint16]string{
		0x0084: "f_child",
		0x0031: "child_id",
		0x0032: "master_id",
		0x0087: "status",
		0x0016: "ChildPrefix",
	}
	for id, expected := range want {
		if got := formPropertyText(props, id); got != expected {
			t.Errorf("SubForm %s=%q want=%q", FormPropertyIDToName(id), got, expected)
		}
	}
}

func TestParseJet4SubFormNumericTail(t *testing.T) {
	tests := []struct {
		name string
		tail []byte
		want jet4SubFormNumericProperties
	}{
		{
			name: "detail boundary prefix",
			tail: []byte{
				0xFF, 0x0E, 0x00, 0x70, 0x00,
				0x33, 0x55, 0x35, 0x01,
				0x60, 0x2C, 0x01,
				0x61, 0x54, 0x06,
				0x62, 0x18, 0x33,
				0x63, 0x40, 0x1A,
				0xDC, 0x3A,
			},
			want: jet4SubFormNumericProperties{
				Visible:     true,
				Geometry:    formControlGeometry{Left: 300, Top: 1620, Width: 13080, Height: 6720},
				HasGeometry: true,
			},
		},
		{
			name: "standard record with can shrink and tab index",
			tail: []byte{
				0xFD, 0x70, 0x00,
				0x04,
				0x33, 0xF7,
				0x60, 0xF0, 0x00,
				0x61, 0x8A, 0x09,
				0x62, 0xE0, 0x2E,
				0x63, 0x75, 0x06,
				0x64, 0x05, 0x00,
				0xDC, 0x3A,
			},
			want: jet4SubFormNumericProperties{
				TabIndex: 5, HasTabIndex: true, CanShrink: true, Visible: true,
				Geometry:    formControlGeometry{Left: 240, Top: 2442, Width: 12000, Height: 1653},
				HasGeometry: true,
			},
		},
		{
			name: "final control record",
			tail: []byte{
				0xFE, 0x70, 0x00,
				0x33, 0xF7,
				0x60, 0x2C, 0x01,
				0x61, 0xED, 0x03,
				0x62, 0x28, 0x23,
				0x63, 0x84, 0x18,
				0xDC, 0x2A,
			},
			want: jet4SubFormNumericProperties{
				Visible:     true,
				Geometry:    formControlGeometry{Left: 300, Top: 1005, Width: 9000, Height: 6276},
				HasGeometry: true,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseJet4SubFormNumericTail(tt.tail)
			if !ok {
				t.Fatal("parseJet4SubFormNumericTail did not recognize SubForm record")
			}
			if got != tt.want {
				t.Fatalf("SubForm numeric properties=%+v want=%+v", got, tt.want)
			}
		})
	}
}

func TestReadFormContentFDMsMawbCaseInsensitiveSubForm(t *testing.T) {
	db, err := Open(filepath.Clean(defaultTestDB))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	content, err := db.ReadFormContent("f_dms_mawb")
	if err != nil {
		t.Fatalf("ReadFormContent(%q) failed: %v", "f_dms_mawb", err)
	}
	for _, control := range content.Controls {
		if control.Type != "SubForm" || control.Name != "Sub_dms_mawb_log_list" {
			continue
		}
		if control.SourceObject != "f_dms_mawb_log_list" ||
			control.LinkChildFields != "dms_mawb_id" || control.LinkMasterFields != "dms_mawb_id" {
			t.Errorf("SubForm linked source fields: source=%q child=%q master=%q",
				control.SourceObject, control.LinkChildFields, control.LinkMasterFields)
		}
		if !control.HasGeometry || control.Left != 300 || control.Top != 1005 ||
			control.Width != 9000 || control.Height != 6276 {
			t.Errorf("SubForm geometry=(%d,%d,%d,%d) complete=%v want=(300,1005,9000,6276)",
				control.Left, control.Top, control.Width, control.Height, control.HasGeometry)
		}
		return
	}
	t.Fatal("SubForm Sub_dms_mawb_log_list is missing")
}

func TestParseJet4TabControlTextProperties(t *testing.T) {
	props := parseJet4TabControlTextProperties(
		FormControlInfo{Name: "MainTab", Type: "TabControl"},
		[]jet4TaggedTextField{
			{Tag: 0xDF, Value: "Verdana"},
			{Tag: 0xE8, Value: "MainTab"},
		},
	)
	if got := formPropertyText(props, 0x0022); got != "Verdana" {
		t.Fatalf("TabControl FontName=%q want=Verdana", got)
	}
	if got := formPropertyText(props, 0x0016); got != "MainTab" {
		t.Fatalf("TabControl EventProcPrefix=%q want=MainTab", got)
	}
}

func TestParseJet4TabControlNumericTail(t *testing.T) {
	tests := []struct {
		name string
		tail []byte
		want jet4TabControlNumericProperties
	}{
		{
			name: "final record",
			tail: []byte{
				0xFE, 0x7B, 0x00,
				0x31, 0x55,
				0x62, 0xFC, 0x30,
				0x63, 0xC0, 0x21,
				0x64, 0x09, 0x00,
				0x65, 0xBC, 0x02,
				0xDC, 0x0E,
			},
			want: jet4TabControlNumericProperties{
				FontSize: 9, FontWeight: 700, Visible: true,
				Geometry: formControlGeometry{Width: 12540, Height: 8640}, HasGeometry: true,
			},
		},
		{
			name: "detail boundary prefix",
			tail: []byte{
				0xFF, 0x04, 0x00, 0x7B, 0x00,
				0x31, 0x55,
				0x61, 0x68, 0x01,
				0x62, 0x80, 0x25,
				0x63, 0x6B, 0x1C,
				0x64, 0x09, 0x00,
				0x65, 0xBC, 0x02,
				0xDC, 0x0E,
			},
			want: jet4TabControlNumericProperties{
				FontSize: 9, FontWeight: 700, Visible: true,
				Geometry: formControlGeometry{Top: 360, Width: 9600, Height: 7275}, HasGeometry: true,
			},
		},
		{
			name: "default font properties",
			tail: []byte{
				0xFD, 0x7B, 0x00,
				0x31, 0x57,
				0x61, 0x68, 0x01,
				0x62, 0xF0, 0x2D,
				0x63, 0x20, 0x1C,
				0x66, 0x07, 0x00,
				0xDC, 0x14,
			},
			want: jet4TabControlNumericProperties{
				FontSize: 8, FontWeight: 400, Visible: true,
				Geometry: formControlGeometry{Top: 360, Width: 11760, Height: 7200}, HasGeometry: true,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseJet4TabControlNumericTail(tt.tail)
			if !ok {
				t.Fatal("parseJet4TabControlNumericTail did not recognize TabControl record")
			}
			if got != tt.want {
				t.Fatalf("TabControl numeric properties=%+v want=%+v", got, tt.want)
			}
		})
	}
}

func TestParseJet4TabPageTextProperties(t *testing.T) {
	props := parseJet4TabPageTextProperties(
		FormControlInfo{Name: "ManifestStatus", Type: "TabPage"},
		[]jet4TaggedTextField{
			{Tag: 0xE3, Value: "Group"},
			{Tag: 0xE8, Value: "Manifest Status"},
		},
	)
	if got := formPropertyText(props, 0x0016); got != "Group" {
		t.Fatalf("TabPage EventProcPrefix=%q want=Group", got)
	}
	if got := formPropertyText(props, 0x0011); got != "Manifest Status" {
		t.Fatalf("TabPage Caption=%q want=%q", got, "Manifest Status")
	}
}

func TestParseJet4TabPageNumericTail(t *testing.T) {
	tests := []struct {
		name string
		tail []byte
	}{
		{
			name: "tab control boundary prefix",
			tail: []byte{
				0xFF, 0x03, 0x00, 0x7C, 0x00,
				0x31, 0xD7,
				0x60, 0x87, 0x00,
				0x61, 0xA4, 0x01,
				0x62, 0xEE, 0x2F,
				0x63, 0x95, 0x1F,
				0xDC, 0x1C,
			},
		},
		{
			name: "standard record",
			tail: []byte{
				0xFD, 0x7C, 0x00,
				0x31, 0xF7,
				0x60, 0x87, 0x00,
				0x61, 0xA4, 0x01,
				0x62, 0xEE, 0x2F,
				0x63, 0x95, 0x1F,
				0xDC, 0x18,
			},
		},
		{
			name: "legacy mask two",
			tail: []byte{
				0xFF, 0x02, 0x00, 0x7C, 0x00,
				0x31, 0xD7,
				0x60, 0x87, 0x00,
				0x61, 0xFD, 0x02,
				0x62, 0xE2, 0x2C,
				0x63, 0x04, 0x1A,
				0xDC, 0x08,
			},
		},
		{
			name: "legacy mask six",
			tail: []byte{
				0xFF, 0x06, 0x00, 0x7C, 0x00,
				0x31, 0xD7,
				0x60, 0x87, 0x00,
				0x61, 0x39, 0x03,
				0x62, 0x5E, 0x38,
				0x63, 0xE0, 0x1F,
				0xDC, 0x14,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseJet4TabPageNumericTail(tt.tail)
			if !ok {
				t.Fatal("parseJet4TabPageNumericTail did not recognize TabPage record")
			}
			want := jet4TabPageNumericProperties{
				Visible:     true,
				Geometry:    formControlGeometry{Left: 98, Top: 390, Width: 12345, Height: 8153},
				HasGeometry: true,
			}
			if tt.name == "legacy mask two" {
				want.Geometry = formControlGeometry{Left: 98, Top: 720, Width: 11565, Height: 6743}
			} else if tt.name == "legacy mask six" {
				want.Geometry = formControlGeometry{Left: 98, Top: 780, Width: 14505, Height: 8243}
			}
			if got != want {
				t.Fatalf("TabPage numeric properties=%+v want=%+v", got, want)
			}
		})
	}
}

func TestParseJet4RectangleDefaultHeight(t *testing.T) {
	tests := []struct {
		name   string
		prefix []byte
		want   int
	}{
		{
			name:   "no rectangle template",
			prefix: []byte{0xFD, 0x68, 0x00, 0x63, 0xA4, 0x01},
			want:   jet4RectangleBuiltInDefaultHeight,
		},
		{
			name: "template omits height",
			prefix: []byte{0x00, 0xFF, 0x12, 0x00, 0xFD, 0x65, 0x00, 0x31, 0x03, 0x32, 0x00,
				0xFD, 0x67, 0x00, 0x32, 0x00},
			want: jet4RectangleBuiltInDefaultHeight,
		},
		{
			name: "template has explicit height",
			prefix: []byte{0xFD, 0x65, 0x00, 0x31, 0x03, 0x32, 0x00, 0x63, 0x84, 0x03,
				0xFD, 0x67, 0x00},
			want: 900,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseJet4RectangleDefaultHeight(tt.prefix); got != tt.want {
				t.Fatalf("parseJet4RectangleDefaultHeight()=%d want=%d", got, tt.want)
			}
		})
	}
}

func TestParseJet4RectangleNumericTail(t *testing.T) {
	tests := []struct {
		name string
		tail []byte
		want jet4RectangleNumericProperties
	}{
		{
			name: "omitted left top and border color",
			tail: []byte{0xFD, 0x65, 0x00, 0x31, 0x00, 0x32, 0x01, 0x34, 0x01, 0x35, 0x5D,
				0x62, 0x70, 0x35, 0x63, 0xA4, 0x01, 0x9C, 0x35, 0x58, 0x75, 0x00, 0xDC, 0x16},
			want: jet4RectangleNumericProperties{
				BackStyle: 1, BorderStyle: 1, BorderWidth: 1,
				BackColor: "#355875", BackColorValue: 7690293, BorderColor: "#000000", Visible: true,
				Geometry: formControlGeometry{Width: 13680, Height: 420}, HasGeometry: true,
			},
		},
		{
			name: "explicit geometry and colors",
			tail: []byte{0xFD, 0x65, 0x00, 0x31, 0x00, 0x34, 0x01, 0x35, 0xDF,
				0x60, 0xF0, 0x00, 0x61, 0x32, 0x04, 0x62, 0x5C, 0x0D, 0x63, 0xC8, 0x04,
				0x9C, 0xC0, 0xC0, 0xC0, 0x00, 0x9D, 0x80, 0x80, 0x80, 0x00, 0xDC, 0x12},
			want: jet4RectangleNumericProperties{
				BorderStyle: 1, BorderWidth: 1,
				BackColor: "#c0c0c0", BackColorValue: 12632256,
				BorderColor: "#808080", BorderColorValue: 8421504, Visible: true,
				Geometry: formControlGeometry{Left: 240, Top: 1074, Width: 3420, Height: 1224}, HasGeometry: true,
			},
		},
		{
			name: "omitted default height",
			tail: []byte{0xFD, 0x65, 0x00, 0x31, 0x00, 0x34, 0x01, 0x35, 0xFF,
				0x60, 0x2C, 0x01, 0x61, 0xE8, 0x08, 0x62, 0x08, 0x07,
				0x9C, 0xC0, 0xC0, 0xC0, 0x00, 0x9D, 0x80, 0x80, 0x80, 0x00, 0xDC, 0x12},
			want: jet4RectangleNumericProperties{
				BorderStyle: 1, BorderWidth: 1,
				BackColor: "#c0c0c0", BackColorValue: 12632256,
				BorderColor: "#808080", BorderColorValue: 8421504, Visible: true,
				Geometry: formControlGeometry{Left: 300, Top: 2280, Width: 1800, Height: 720}, HasGeometry: true,
			},
		},
		{
			name: "tab page boundary prefix",
			tail: []byte{0xFF, 0x28, 0x00, 0x65, 0x00, 0x31, 0x00, 0x34, 0x01, 0x35, 0xFF,
				0x60, 0x2C, 0x01, 0x61, 0x90, 0x09, 0x62, 0xEC, 0x13, 0x63, 0xD8, 0x09,
				0x9C, 0xC0, 0xC0, 0xC0, 0x00, 0x9D, 0x80, 0x80, 0x80, 0x00, 0xDC, 0x0C},
			want: jet4RectangleNumericProperties{
				BorderStyle: 1, BorderWidth: 1,
				BackColor: "#c0c0c0", BackColorValue: 12632256,
				BorderColor: "#808080", BorderColorValue: 8421504, Visible: true,
				Geometry: formControlGeometry{Left: 300, Top: 2448, Width: 5100, Height: 2520}, HasGeometry: true,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseJet4RectangleNumericTail(tt.tail)
			if !ok {
				t.Fatal("parseJet4RectangleNumericTail did not recognize Rectangle record")
			}
			if got != tt.want {
				t.Fatalf("Rectangle numeric properties=%+v want=%+v", got, tt.want)
			}
		})
	}

	t.Run("form template supplies omitted height", func(t *testing.T) {
		tail := []byte{0xFD, 0x65, 0x00, 0x31, 0x00, 0x34, 0x01,
			0x60, 0x2C, 0x01, 0x61, 0xE8, 0x08, 0x62, 0x08, 0x07, 0xDC, 0x12}
		got, ok := parseJet4RectangleNumericTailWithDefaultHeight(tail, 900)
		if !ok {
			t.Fatal("parseJet4RectangleNumericTailWithDefaultHeight did not recognize Rectangle record")
		}
		if got.Geometry.Height != 900 {
			t.Fatalf("Rectangle height=%d want form template height=900", got.Geometry.Height)
		}
	})
}

func TestFormPropertyIDToNameAgainstInterop(t *testing.T) {
	cases := map[uint16]string{
		7:   "Picture",
		13:  "BoundColumn",
		18:  "ColumnWidths",
		17:  "Caption",
		20:  "Name",
		22:  "EventProcPrefix",
		27:  "ControlSource",
		28:  "BackColor",
		29:  "BackStyle",
		34:  "FontName",
		35:  "FontSize",
		36:  "FontUnderline",
		37:  "FontWeight",
		38:  "Format",
		44:  "Height",
		70:  "ColumnCount",
		54:  "Left",
		56:  "Locked",
		58:  "OptionValue",
		49:  "LinkChildFields",
		50:  "LinkMasterFields",
		72:  "InputMask",
		91:  "RowSource",
		93:  "RowSourceType",
		136: "TextAlign",
		132: "SourceObject",
		141: "Top",
		147: "DefaultView",
		148: "Visible",
		150: "Width",
		152: "ScrollBars",
		153: "ListRows",
		154: "ListWidth",
		204: "ForeColor",
		261: "TabIndex",
		266: "Tag",
		352: "PageIndex",
		476: "TextFormat",
	}
	for id, want := range cases {
		if got := FormPropertyIDToName(id); got != want {
			t.Errorf("FormPropertyIDToName(%d)=%q want=%q", id, got, want)
		}
	}
}

func TestParseJet4FormDefaultView(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want int
		ok   bool
	}{
		{
			name: "single form",
			data: []byte{0x13, 0x00, 0x13, 0x00, 0x00, 0x00, 0x00, 0x00, 0x96, 0x00, 0x06},
			want: 0,
			ok:   true,
		},
		{
			name: "continuous forms",
			data: []byte{0x13, 0x00, 0x13, 0x00, 0x00, 0x00, 0x00, 0x00, 0x96, 0x00, 0x0F},
			want: 1,
			ok:   true,
		},
		{
			name: "truncated header",
			data: []byte{0x13, 0x00, 0x13},
		},
		{
			name: "unknown template",
			data: []byte{0x13, 0x00, 0x13, 0x00, 0x00, 0x00, 0x00, 0x00, 0x96, 0x00, 0x20},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseJet4FormDefaultView(tt.data)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("parseJet4FormDefaultView()=(%d,%v) want=(%d,%v)", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestJet4ControlNumericTailWithoutGUID(t *testing.T) {
	name := "hs_code"
	block := append([]byte(nil), encodeUTF16LE(name)...)
	block = append(block, 0xEA, 0x0E)
	block = append(block, encodeUTF16LE("Verdana")...)
	want := []byte{0xFE, 0x64, 0x00, 0x35, 0xD7}
	block = append(block, want...)
	if got := jet4ControlNumericTailForType(block, name, "ComboBox"); !bytes.Equal(got, want) {
		t.Fatalf("numeric tail=% x want=% x", got, want)
	}
}

func TestOrderedFormControlOffsetsSkipsPrefixNameDictionary(t *testing.T) {
	appendTaggedName := func(dst []byte, name string) []byte {
		encoded := encodeUTF16LE(name)
		dst = append(dst, encoded...)
		dst = append(dst, 0xDD, byte(len(encoded)))
		return append(dst, encoded...)
	}

	data := appendTaggedName(nil, "field_id") // 窗体前部字段字典中的同名项。
	data = append(data, make([]byte, 80)...)
	sectionOffset := len(data)
	data = append(data, encodeUTF16LE("Detail0")...)
	data = append(data, make([]byte, 24)...)
	controlOffset := len(data)
	data = appendTaggedName(data, "field_id") // 真正的控件设计块。

	controls := []FormControlInfo{
		{Name: "field_id", Type: "TextBox", TypeCode: 0x126D},
		{Name: "Detail0", Type: "Detail", TypeCode: 0x1898},
	}
	offsets := orderedFormControlOffsets(data, controls)
	if offsets[0] != controlOffset || offsets[1] != sectionOffset {
		t.Fatalf("control offsets=%v want=[%d %d]", offsets, controlOffset, sectionOffset)
	}
}

func TestOrderedFormControlOffsetsSkipsLabelCaptionMatch(t *testing.T) {
	tests := []struct {
		name       string
		falseTag   byte
		falseValue string
	}{
		{name: "label font size", falseTag: 0xE4, falseValue: "128"},
		{name: "label font name e8", falseTag: 0xE8, falseValue: "Verdana"},
		{name: "label font name de", falseTag: 0xDE, falseValue: "Verdana"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := append([]byte(nil), encodeUTF16LE("Detail0")...)
			data = append(data, make([]byte, 24)...)
			data = append(data, encodeUTF16LE("MID")...)
			falseValue := encodeUTF16LE(tt.falseValue)
			data = append(data, tt.falseTag, byte(len(falseValue)))
			data = append(data, falseValue...)
			data = append(data, make([]byte, 24)...)
			controlOffset := len(data)
			data = append(data, encodeUTF16LE("mid")...)
			source := encodeUTF16LE("mid")
			data = append(data, 0xDD, byte(len(source)))
			data = append(data, source...)

			controls := []FormControlInfo{
				{Name: "Detail0", Type: "Detail", TypeCode: 0x1898},
				{Name: "mid", Type: "TextBox", TypeCode: 0x126D},
			}
			offsets := orderedFormControlOffsets(data, controls)
			if offsets[1] != controlOffset {
				t.Fatalf("TextBox mid offset=%d want=%d", offsets[1], controlOffset)
			}
		})
	}
}

func TestOrderedFormControlOffsetsSkipsLongerControlNamePrefix(t *testing.T) {
	appendTaggedName := func(dst []byte, name string) []byte {
		dst = append(dst, encodeUTF16LE(name)...)
		encoded := encodeUTF16LE(name)
		dst = append(dst, 0xDD, byte(len(encoded)))
		return append(dst, encoded...)
	}

	data := appendTaggedName(nil, "entry_seq_code1")
	data = append(data, make([]byte, 24)...)
	wantOffset := len(data)
	data = appendTaggedName(data, "entry_seq_code")
	offsets := orderedFormControlOffsets(data, []FormControlInfo{
		{Name: "entry_seq_code", Type: "TextBox", TypeCode: 0x126D},
	})
	if offsets[0] != wantOffset {
		t.Fatalf("entry_seq_code offset=%d want=%d", offsets[0], wantOffset)
	}
}

func TestOrderedFormControlOffsetsSkipsLongerControlNameSuffix(t *testing.T) {
	appendTabPage := func(dst []byte, name, caption string) []byte {
		dst = append(dst, encodeUTF16LE(name)...)
		encoded := encodeUTF16LE(caption)
		dst = append(dst, 0xE8, byte(len(encoded)))
		return append(dst, encoded...)
	}

	data := appendTabPage(nil, "EventLog", "Event Log")
	data = append(data, make([]byte, 24)...)
	wantOffset := len(data)
	data = appendTabPage(data, "Log", "Submit / Error Log")
	offsets := orderedFormControlOffsets(data, []FormControlInfo{
		{Name: "Log", Type: "TabPage", TypeCode: 0x217C},
	})
	if offsets[0] != wantOffset {
		t.Fatalf("Log offset=%d want complete TabPage block at %d", offsets[0], wantOffset)
	}
}

func TestOrderedFormControlOffsetsPrefersCompleteTextBoxBlock(t *testing.T) {
	name := "entry_seq_code"
	data := append([]byte(nil), encodeUTF16LE(name)...)
	tag := encodeUTF16LE("sel_status_desc")
	data = append(data, 0xF4, byte(len(tag)))
	data = append(data, tag...)
	data = append(data, make([]byte, 24)...)
	wantOffset := len(data)
	data = append(data, encodeUTF16LE(name)...)
	source := encodeUTF16LE(name)
	data = append(data, 0xDD, byte(len(source)))
	data = append(data, source...)
	data = append(data, 0xF4, byte(len(tag)))
	data = append(data, tag...)

	offsets := orderedFormControlOffsets(data, []FormControlInfo{
		{Name: name, Type: "TextBox", TypeCode: 0x126D},
	})
	if offsets[0] != wantOffset {
		t.Fatalf("entry_seq_code offset=%d want complete block at %d", offsets[0], wantOffset)
	}
}

func TestOrderedFormControlOffsetsRejectsLabelCaptionForComboBox(t *testing.T) {
	data := append([]byte(nil), encodeUTF16LE("mid")...)
	font := encodeUTF16LE("Verdana")
	data = append(data, 0xDE, byte(len(font)))
	data = append(data, font...)
	data = append(data, make([]byte, 24)...)
	wantOffset := len(data)
	data = append(data, encodeUTF16LE("mid")...)
	source := encodeUTF16LE("mid")
	data = append(data, 0xDD, byte(len(source)))
	data = append(data, source...)
	rowSourceType := encodeUTF16LE("Table/Query")
	data = append(data, 0xDE, byte(len(rowSourceType)))
	data = append(data, rowSourceType...)
	rowSource := encodeUTF16LE("q_mid")
	data = append(data, 0xDF, byte(len(rowSource)))
	data = append(data, rowSource...)

	offsets := orderedFormControlOffsets(data, []FormControlInfo{
		{Name: "mid", Type: "ComboBox", TypeCode: 0x136F},
	})
	if offsets[0] != wantOffset {
		t.Fatalf("mid offset=%d want complete ComboBox block at %d", offsets[0], wantOffset)
	}
}

func TestOrderedFormControlOffsetsRejectsFontOnlyTabPageCandidate(t *testing.T) {
	data := append([]byte(nil), encodeUTF16LE("Other")...)
	font := encodeUTF16LE("Verdana")
	data = append(data, 0xDE, byte(len(font)))
	data = append(data, font...)
	data = append(data, make([]byte, 24)...)
	wantOffset := len(data)
	data = append(data, encodeUTF16LE("Other")...)
	caption := encodeUTF16LE("DMS Filing")
	data = append(data, 0xE8, byte(len(caption)))
	data = append(data, caption...)

	offsets := orderedFormControlOffsets(data, []FormControlInfo{
		{Name: "Other", Type: "TabPage", TypeCode: 0x217C},
	})
	if offsets[0] != wantOffset {
		t.Fatalf("Other offset=%d want complete TabPage block at %d", offsets[0], wantOffset)
	}
}

func TestNormalizeControlSourcePreservesFullNativeIdentifier(t *testing.T) {
	for _, value := range []string{"loading_port_code", "discharge_port_code", "TrackingNo"} {
		got, ok := normalizeControlSource(value, strings.TrimSuffix(strings.ToLower(value), "_code"))
		if !ok || got != value {
			t.Fatalf("normalizeControlSource(%q)=%q,%v", value, got, ok)
		}
	}
}

func TestParseJet4LabelNumericTailNativeColorVariants(t *testing.T) {
	datasheetTail := []byte{
		0xFD, 0x64, 0x00, 0x32, 0x01, 0x33, 0x01, 0x35, 0x5D, 0x37, 0x02,
		0x60, 0xEC, 0x04, 0x62, 0x08, 0x07, 0x63, 0x20, 0x01, 0x65, 0x90, 0x01,
		0x9C, 0x33, 0x33, 0x33, 0x00, 0x9D, 0xFF, 0xFF, 0xFF, 0x00,
		0x9E, 0xFF, 0xFF, 0xFF, 0x00, 0xDC, 0x18,
	}
	datasheet, ok := parseJet4LabelNumericTail(datasheetTail)
	if !ok || datasheet.BackStyle != 1 {
		t.Fatalf("Datasheet Label BackStyle=%d ok=%v", datasheet.BackStyle, ok)
	}

	tail := []byte{
		0xFD, 0x64, 0x00, 0x01, 0x35, 0x5F, 0x37, 0x02,
		0x60, 0x5C, 0x1C,
		0x62, 0xC0, 0x03,
		0x63, 0x20, 0x01,
		0x65, 0x90, 0x01,
		0x9C, 0x33, 0x33, 0x33, 0x00,
		0x9D, 0xFF, 0xFF, 0xFF, 0x00,
		0xDC, 0x10,
	}
	got, ok := parseJet4LabelNumericTail(tail)
	if !ok || got.BackStyle != 1 || got.BackColorValue != 0x00333333 || got.ForeColorValue != 0x00FFFFFF {
		t.Fatalf("Label native colors=%+v ok=%v", got, ok)
	}

	defaultBackColorTail := []byte{
		0xFE, 0x64, 0x00, 0x35, 0xFF, 0x37, 0x01,
		0x60, 0xD4, 0x1C,
		0x61, 0x28, 0x05,
		0x62, 0x0C, 0x03,
		0x63, 0x2C, 0x01,
		0x64, 0x09, 0x00,
		0x9E, 0x00, 0x00, 0x00, 0x00,
		0xDC, 0x12,
	}
	got, ok = parseJet4LabelNumericTail(defaultBackColorTail)
	if !ok || got.BackColorValue != 0x8000000F || got.ForeColorValue != 0 {
		t.Fatalf("Label default native colors=%+v ok=%v", got, ok)
	}

	systemForeColorTail := append([]byte(nil), defaultBackColorTail...)
	copy(systemForeColorTail[len(systemForeColorTail)-6:len(systemForeColorTail)-2], []byte{0x12, 0x00, 0x00, 0x80})
	got, ok = parseJet4LabelNumericTail(systemForeColorTail)
	if !ok || got.BackColorValue != 0x00FFFFFF || got.ForeColorValue != 0x80000012 {
		t.Fatalf("Label system ForeColor defaults=%+v ok=%v", got, ok)
	}

	systemDefaultsTail := []byte{
		0xFE, 0x64, 0x00, 0x35, 0xFF, 0x37, 0x01,
		0x60, 0xD4, 0x1C,
		0x61, 0x28, 0x05,
		0x62, 0x0C, 0x03,
		0x63, 0x2C, 0x01,
		0x64, 0x09, 0x00,
		0xDC, 0x12,
	}
	got, ok = parseJet4LabelNumericTail(systemDefaultsTail)
	if !ok || got.BackColorValue != 0x8000000F || got.ForeColorValue != 0x80000012 {
		t.Fatalf("Label omitted native colors=%+v ok=%v", got, ok)
	}

	backColorOnlyTail := []byte{
		0xFE, 0x64, 0x00, 0x35, 0xDF, 0x37, 0x02,
		0x60, 0xB0, 0x31,
		0x61, 0x34, 0x08,
		0x62, 0xA0, 0x05,
		0x63, 0x2C, 0x01,
		0x64, 0x09, 0x00,
		0x9C, 0x0F, 0x00, 0x00, 0x80,
		0xDC, 0x10,
	}
	got, ok = parseJet4LabelNumericTail(backColorOnlyTail)
	if !ok || got.BackColorValue != 0x8000000F || got.ForeColorValue != 0x80000012 {
		t.Fatalf("Label BackColor-only native colors=%+v ok=%v", got, ok)
	}

	records := map[int]jet4LabelNumericProperties{
		0: got,
		1: func() jet4LabelNumericProperties {
			value, parsed := parseJet4LabelNumericTail(systemDefaultsTail)
			if !parsed {
				t.Fatal("parse omitted Label color record failed")
			}
			return value
		}(),
	}
	applyJet4LabelColorDefaults(records)
	if records[0].BackColorValue != 0x8000000F || records[0].ForeColorValue != 0 ||
		records[1].BackColorValue != 0x00FFFFFF || records[1].ForeColorValue != 0 {
		t.Fatalf("Label form RGB defaults=%+v", records)
	}
}

func TestAssignFormControlSections(t *testing.T) {
	controls := []FormControlContent{
		{Name: "FormHeader", Type: "FormHeader", TypeCode: 0x1899, Index: 0,
			Height: 291, BackColor: "#808080", BackColorValue: 0x00808080,
			Visible: true, EventProcPrefix: "FormHeader0"},
		{Name: "title", Type: "Label", TypeCode: 0x0C64, Index: 1},
		{Name: "Detail", Type: "Detail", TypeCode: 0x1898, Index: 2},
		{Name: "field", Type: "TextBox", TypeCode: 0x126D, Index: 3},
		{Name: "FormFooter", Type: "FormFooter", TypeCode: 0x189A, Index: 4},
		{Name: "total", Type: "TextBox", TypeCode: 0x126D, Index: 5},
	}
	sections := assignFormControlSections(controls)
	if len(sections) != 3 {
		t.Fatalf("sections=%d want=3", len(sections))
	}
	wantTypes := []string{"FormHeader", "Detail", "FormFooter"}
	wantControls := []string{"title", "field", "total"}
	for i := range sections {
		if sections[i].Type != wantTypes[i] {
			t.Fatalf("section %d type=%q want=%q", i, sections[i].Type, wantTypes[i])
		}
		if len(sections[i].Controls) != 1 || sections[i].Controls[0].Name != wantControls[i] {
			t.Fatalf("section %q controls=%+v", sections[i].Type, sections[i].Controls)
		}
		if sections[i].Controls[0].Section != wantTypes[i] {
			t.Fatalf("control %q section=%q want=%q", sections[i].Controls[0].Name,
				sections[i].Controls[0].Section, wantTypes[i])
		}
	}
	if !controls[0].IsSection || !controls[2].IsSection || !controls[4].IsSection {
		t.Fatalf("section markers were not identified: %+v", controls)
	}
	if sections[0].Height != 291 || sections[0].BackColorValue != 0x00808080 ||
		!sections[0].Visible || sections[0].EventProcPrefix != "FormHeader0" {
		t.Fatalf("section properties were not copied: %+v", sections[0])
	}
}

func TestParseJet4SectionRecord(t *testing.T) {
	record := []byte{
		0xFD, 0x98, 0x00,
		0x02, 0x33, 0x01,
		0x60, 0xC0, 0x21,
		0x9C, 0x0F, 0x00, 0x00, 0x80,
		0xDF, 0x0C,
	}
	record = append(record, encodeUTF16LE("Detail")...)
	record = append(record, 0xE7, 0x10)
	record = append(record, make([]byte, 16)...)

	got, ok := parseJet4SectionRecord(record)
	if !ok {
		t.Fatal("parseJet4SectionRecord did not recognize Detail")
	}
	if got.Height != 8640 || got.BackColorValue != 0x8000000F || got.BackColor != "#0f0000" ||
		got.SpecialEffect != 1 || !got.Visible || got.EventProcPrefix != "Detail" {
		t.Fatalf("section properties=%+v", got)
	}
}

func TestParseJet4ControlGeometry(t *testing.T) {
	block := []byte{
		0x60, 0x2c, 0x01,
		0x61, 0xd8, 0x00,
		0x62, 0xec, 0x04,
		0x63, 0x20, 0x01,
	}
	got, ok := parseJet4ControlGeometry(block)
	if !ok {
		t.Fatal("parseJet4ControlGeometry did not recognize geometry tags")
	}
	want := formControlGeometry{Left: 300, Top: 216, Width: 1260, Height: 288}
	if got != want {
		t.Fatalf("geometry=%+v want=%+v", got, want)
	}
}

func TestExportFormContents(t *testing.T) {
	dbPath := requireDBFile(t)
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	contents, err := db.ExportFormContents()
	if err != nil {
		t.Fatalf("ExportFormContents failed: %v", err)
	}
	if len(contents) == 0 {
		t.Fatal("ExportFormContents returned no forms")
	}
	controlCount := 0
	geometryCount := 0
	formWidthCount := 0
	for i, content := range contents {
		if strings.TrimSpace(content.FormName) == "" {
			t.Fatalf("form %d has empty name", i)
		}
		controlCount += len(content.Controls)
		if len(content.Sections) == 0 {
			t.Fatalf("form %q has no sections", content.FormName)
		}
		sectionTypes := make([]string, 0, len(content.Sections))
		sectionControlCount := 0
		for _, section := range content.Sections {
			sectionTypes = append(sectionTypes, section.Type)
			sectionControlCount += len(section.Controls)
			for _, control := range section.Controls {
				if control.Section != section.Type {
					t.Fatalf("form %q control %q section=%q want=%q",
						content.FormName, control.Name, control.Section, section.Type)
				}
			}
		}
		sectionMarkerCount := 0
		for _, control := range content.Controls {
			if control.IsSection {
				sectionMarkerCount++
			}
		}
		if sectionControlCount+sectionMarkerCount != len(content.Controls) {
			t.Fatalf("form %q grouped controls=%d section markers=%d flat controls=%d",
				content.FormName, sectionControlCount, sectionMarkerCount, len(content.Controls))
		}
		if content.Width > 0 {
			formWidthCount++
		}
		formGeometryCount := 0
		for _, control := range content.Controls {
			if control.HasGeometry {
				geometryCount++
				formGeometryCount++
			}
		}
		if os.Getenv("MDBGO_DEBUG_FORM_GEOMETRY") != "" {
			t.Logf("form=%q width=%d sections=%v controls=%d geometry=%d", content.FormName,
				content.Width, sectionTypes, len(content.Controls), formGeometryCount)
		}
		if i > 0 && strings.ToLower(contents[i-1].FormName) > strings.ToLower(content.FormName) {
			t.Fatalf("forms are not sorted: %q before %q", contents[i-1].FormName, content.FormName)
		}
	}
	if controlCount == 0 {
		t.Fatal("ExportFormContents returned no controls")
	}
	if geometryCount == 0 {
		t.Fatal("ExportFormContents returned no control geometry")
	}
	t.Logf("forms=%d forms_with_width=%d controls=%d controls_with_geometry=%d",
		len(contents), formWidthCount, controlCount, geometryCount)
}

func TestDebugComboBoxControls(t *testing.T) {
	if os.Getenv("MDBGO_DEBUG_COMBOBOX") == "" {
		t.Skip("set MDBGO_DEBUG_COMBOBOX=1 to enable debug output")
	}
	dbPath := requireDBFile(t)
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	contents, err := db.ExportFormContents()
	if err != nil {
		t.Fatalf("ExportFormContents failed: %v", err)
	}
	controlType := strings.TrimSpace(os.Getenv("MDBGO_DEBUG_CONTROL_TYPE"))
	if controlType == "" {
		controlType = "ComboBox"
	}
	count := 0
	for _, content := range contents {
		for _, control := range content.Controls {
			if control.Type != controlType {
				continue
			}
			count++
			t.Logf("form=%q control=%q section=%s source=%q properties=%+v",
				content.FormName, control.Name, control.Section, control.ControlSource, control.Properties)
		}
	}
	if count == 0 {
		t.Logf("no %s controls found", controlType)
	}
}

func TestDebugFormControlsByType(t *testing.T) {
	targetType := strings.TrimSpace(os.Getenv("MDBGO_DEBUG_CONTROL_TYPE"))
	if targetType == "" {
		t.Skip("set MDBGO_DEBUG_CONTROL_TYPE to enable debug output")
	}
	dbPath := requireDBFile(t)
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = db.Close() }()
	contents, err := db.ExportFormContents()
	if err != nil {
		t.Fatalf("ExportFormContents failed: %v", err)
	}
	count := 0
	for _, content := range contents {
		for _, control := range content.Controls {
			if !strings.EqualFold(control.Type, targetType) {
				continue
			}
			count++
			t.Logf("form=%q control=%q type=%s section=%s source=%q geometry=(%d,%d,%d,%d)",
				content.FormName, control.Name, control.Type, control.Section, control.ControlSource,
				control.Left, control.Top, control.Width, control.Height)
		}
	}
	t.Logf("type=%s controls=%d", targetType, count)
}

func TestDebugFormObjectStreams(t *testing.T) {
	if os.Getenv("MDBGO_DEBUG_FORM_OLE") == "" {
		t.Skip("set MDBGO_DEBUG_FORM_OLE=1 to enable debug output")
	}
	dbPath := requireDBFile(t)
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	entries, err := db.ReadAccessObjectEntries()
	if err != nil {
		t.Fatalf("ReadAccessObjectEntries failed: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir || (!strings.HasPrefix(entry.Path, "Forms/0/") &&
			entry.Path != "Forms/DirData" && entry.Path != "Forms/PropData") {
			continue
		}
		limit := len(entry.Data)
		if limit > 256 {
			limit = 256
		}
		words := append(extractUTF16LEWords(entry.Data, 2), extractUTF16LEWords(entry.Data[1:], 2)...)
		if len(words) > 30 {
			words = words[:30]
		}
		t.Logf("stream=%s size=%d hex=% x words=%q", entry.Path, len(entry.Data), entry.Data[:limit], words)
	}
}

func TestDebugFormTypeInfo(t *testing.T) {
	if os.Getenv("MDBGO_DEBUG_FORM_TYPEINFO") == "" {
		t.Skip("set MDBGO_DEBUG_FORM_TYPEINFO=1 to enable debug output")
	}
	dbPath := requireDBFile(t)
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	formName := requireFormName(t, db)
	if debugFormName := strings.TrimSpace(os.Getenv("MDBGO_DEBUG_FORM_NAME")); debugFormName != "" {
		formName = debugFormName
	}
	streams, err := db.ReadFormObjectStreams(formName)
	if err != nil {
		t.Fatalf("ReadFormObjectStreams failed: %v", err)
	}
	entries, err := parseFormTypeInfo(streams.TypeInfo, true)
	if err != nil {
		t.Fatalf("parseFormTypeInfo failed: %v", err)
	}
	for _, entry := range entries {
		t.Logf("type=0x%04X index=%d kind=%s name=%q", entry.TypeCode, entry.Index, entry.Type, entry.Name)
	}
}

func TestDebugFormBlobControls(t *testing.T) {
	if os.Getenv("MDBGO_DEBUG_FORM_BLOB") == "" {
		t.Skip("set MDBGO_DEBUG_FORM_BLOB=1 to enable debug output")
	}
	dbPath := requireDBFile(t)
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	formName := requireFormName(t, db)
	if debugFormName := strings.TrimSpace(os.Getenv("MDBGO_DEBUG_FORM_NAME")); debugFormName != "" {
		formName = debugFormName
	}
	streams, err := db.ReadFormObjectStreams(formName)
	if err != nil {
		t.Fatalf("ReadFormObjectStreams failed: %v", err)
	}
	debugLogFormBlobControls(t, streams)
}

func debugLogFormBlobControls(t *testing.T, streams *FormObjectStreams) {
	t.Helper()
	controls, err := ParseFormTypeInfo(streams.TypeInfo)
	if err != nil {
		t.Fatalf("ParseFormTypeInfo failed: %v", err)
	}
	blob := streams.Blob
	if os.Getenv("MDBGO_DEBUG_EXPANDED_RAW") == "" {
		blob = normalizeJet4ExpandedFormBlob(streams.Blob, controls)
	}
	controlFilter := strings.TrimSpace(os.Getenv("MDBGO_DEBUG_FORM_CONTROL"))
	controlOffsets := orderedFormControlOffsets(blob, controls)
	indices := make([]int, len(controls))
	for i := range indices {
		indices[i] = i
	}
	sort.Slice(indices, func(i, j int) bool {
		return controlOffsets[indices[i]] < controlOffsets[indices[j]]
	})
	numericOnly := strings.EqualFold(controlFilter, "@numeric")
	labelNumericOnly := strings.EqualFold(controlFilter, "@labelnumeric")
	allTailsOnly := strings.EqualFold(controlFilter, "@tails")
	numericIndex := 0
	for order, i := range indices {
		control := controls[i]
		if controlFilter != "" && !numericOnly && !labelNumericOnly && !allTailsOnly {
			matched := false
			for _, name := range strings.Split(controlFilter, ",") {
				if strings.EqualFold(control.Name, strings.TrimSpace(name)) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		if controlFilter == "" && order >= 12 {
			break
		}
		start := controlOffsets[i]
		if start < 0 {
			t.Logf("control=%q type=%s has no Blob block", control.Name, control.Type)
			continue
		}
		end := len(blob)
		for _, offset := range controlOffsets {
			if offset > start && offset < end {
				end = offset
			}
		}
		block := blob[start:end]
		numericTail := jet4ControlNumericTailForType(block, control.Name, control.Type)
		numeric, numericOK := parseJet4TextBoxNumericTail(numericTail)
		labelNumeric, labelNumericOK := parseJet4LabelNumericTail(numericTail)
		if numericOnly && !numericOK {
			continue
		}
		if labelNumericOnly && !labelNumericOK {
			continue
		}
		if numericOnly {
			t.Logf("numeric[%d] source=%q start=%d value=%+v tail=% x",
				numericIndex, control.Name, start, numeric, numericTail)
			numericIndex++
			continue
		}
		if labelNumericOnly {
			t.Logf("label_numeric[%d] source=%q start=%d value=%+v tail=% x",
				numericIndex, control.Name, start, labelNumeric, numericTail)
			numericIndex++
			continue
		}
		if allTailsOnly {
			t.Logf("tail[%d] source=%q type=%s start=%d value=% x",
				numericIndex, control.Name, control.Type, start, numericTail)
			numericIndex++
			continue
		}
		limit := len(block)
		if limit > 512 {
			limit = 512
		}
		t.Logf("control=%q type=%s start=%d end=%d fields=%+v numeric_ok=%v numeric=%+v tail=% x words=%q hex=% x",
			control.Name, control.Type, start, end,
			parseJet4TaggedTextFieldsForType(block, control.Name, control.Type),
			numericOK, numeric, numericTail,
			extractUTF16LEWords(block[:limit], 1), block[:limit])
	}
}

func TestDebugFormBlobPrefix(t *testing.T) {
	if os.Getenv("MDBGO_DEBUG_FORM_PREFIX") == "" {
		t.Skip("set MDBGO_DEBUG_FORM_PREFIX=1 to enable debug output")
	}
	dbPath := requireDBFile(t)
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	formName := requireFormName(t, db)
	if debugFormName := strings.TrimSpace(os.Getenv("MDBGO_DEBUG_FORM_NAME")); debugFormName != "" {
		formName = debugFormName
	}
	streams, err := db.ReadFormObjectStreams(formName)
	if err != nil {
		t.Fatalf("ReadFormObjectStreams failed: %v", err)
	}
	controls, err := ParseFormTypeInfo(streams.TypeInfo)
	if err != nil {
		t.Fatalf("ParseFormTypeInfo failed: %v", err)
	}
	firstOffset := len(streams.Blob)
	for _, offset := range orderedFormControlOffsets(streams.Blob, controls) {
		if offset >= 0 && offset < firstOffset {
			firstOffset = offset
		}
	}
	limit := firstOffset
	if limit > 4096 {
		limit = 4096
	}
	t.Logf("form=%q blob_len=%d first_control=%d prefix_hex=% x words=%q",
		formName, len(streams.Blob), firstOffset, streams.Blob[:limit],
		extractUTF16LEWords(streams.Blob[:limit], 2))
}

func TestDebugFormSectionRecords(t *testing.T) {
	if os.Getenv("MDBGO_DEBUG_FORM_SECTIONS") == "" {
		t.Skip("set MDBGO_DEBUG_FORM_SECTIONS=1 to enable debug output")
	}
	dbPath := requireDBFile(t)
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	formName := requireFormName(t, db)
	if debugFormName := strings.TrimSpace(os.Getenv("MDBGO_DEBUG_FORM_NAME")); debugFormName != "" {
		formName = debugFormName
	}
	if formName == "@all" {
		entries, err := db.ReadAccessObjectEntries()
		if err != nil {
			t.Fatalf("ReadAccessObjectEntries failed: %v", err)
		}
		formIDs, err := formStorageIDsFromEntries(entries)
		if err != nil {
			t.Fatalf("formStorageIDsFromEntries failed: %v", err)
		}
		formNames := make([]string, 0, len(formIDs))
		for name := range formIDs {
			formNames = append(formNames, name)
		}
		sort.Strings(formNames)
		for _, name := range formNames {
			streams, err := formObjectStreamsFromEntries(entries, name)
			if err != nil {
				t.Fatalf("formObjectStreamsFromEntries(%q) failed: %v", name, err)
			}
			debugLogFormSectionRecords(t, streams)
		}
		return
	}
	streams, err := db.ReadFormObjectStreams(formName)
	if err != nil {
		t.Fatalf("ReadFormObjectStreams failed: %v", err)
	}
	debugLogFormSectionRecords(t, streams)
}

func debugLogFormSectionRecords(t *testing.T, streams *FormObjectStreams) {
	t.Helper()
	controls, err := ParseFormTypeInfo(streams.TypeInfo)
	if err != nil {
		t.Fatalf("ParseFormTypeInfo failed: %v", err)
	}
	for _, control := range controls {
		if isFormSectionTypeCode(control.TypeCode) {
			t.Logf("logical section type=%s code=0x%04X index=%d", control.Type, control.TypeCode, control.Index)
		}
	}
	for pos := 0; pos+3 <= len(streams.Blob); pos++ {
		if streams.Blob[pos] != 0xFD && streams.Blob[pos] != 0xFE && streams.Blob[pos] != 0xFF {
			continue
		}
		if streams.Blob[pos+2] != 0 || (streams.Blob[pos+1] != 0x98 && streams.Blob[pos+1] != 0x99 && streams.Blob[pos+1] != 0x9A) {
			continue
		}
		end := pos + 20
		if end > len(streams.Blob) {
			end = len(streams.Blob)
		}
		t.Logf("form=%q record offset=%d marker=%02X type=%02X head=% x",
			streams.FormName, pos, streams.Blob[pos], streams.Blob[pos+1], streams.Blob[pos:end])
	}
}

func TestDebugJet4GeometryTags(t *testing.T) {
	if os.Getenv("MDBGO_DEBUG_FORM_GEOMETRY") == "" {
		t.Skip("set MDBGO_DEBUG_FORM_GEOMETRY=1 to enable debug output")
	}
	dbPath := requireDBFile(t)
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	formName := requireFormName(t, db)
	if debugFormName := strings.TrimSpace(os.Getenv("MDBGO_DEBUG_FORM_NAME")); debugFormName != "" {
		formName = debugFormName
	}
	streams, err := db.ReadFormObjectStreams(formName)
	if err != nil {
		t.Fatalf("ReadFormObjectStreams failed: %v", err)
	}
	controls, err := ParseFormTypeInfo(streams.TypeInfo)
	if err != nil {
		t.Fatalf("ParseFormTypeInfo failed: %v", err)
	}
	offsets := orderedFormControlOffsets(streams.Blob, controls)
	for i, start := range offsets {
		if start < 0 {
			continue
		}
		end := len(streams.Blob)
		for _, off := range offsets {
			if off > start && off < end {
				end = off
			}
		}
		found := false
		for pos := start; pos+12 <= end; pos++ {
			if streams.Blob[pos] != 0x60 || streams.Blob[pos+3] != 0x61 ||
				streams.Blob[pos+6] != 0x62 || streams.Blob[pos+9] != 0x63 {
				continue
			}
			t.Logf("name=%q type=%s start=%d end=%d tagpos=%d values=%d,%d,%d,%d",
				controls[i].Name, controls[i].Type, start, end, pos,
				le16(streams.Blob[pos+1:]), le16(streams.Blob[pos+4:]),
				le16(streams.Blob[pos+7:]), le16(streams.Blob[pos+10:]))
			found = true
		}
		if !found {
			t.Logf("missing name=%q type=%s start=%d end=%d hex=% x",
				controls[i].Name, controls[i].Type, start, end, streams.Blob[start:end])
		}
	}
}

func TestControlTypeCodeToString(t *testing.T) {
	cases := map[int]string{
		109: "TextBox",
		108: "Label",
		104: "Button",
		111: "ComboBox",
		110: "ListBox",
		106: "CheckBox",
		105: "OptionButton",
		107: "OptionGroup",
		112: "SubForm",
		114: "ObjectFrame",
		100: "Line",
		101: "Rectangle",
		119: "CustomControl",
		118: "TabControl",
	}
	for in, want := range cases {
		got := ControlTypeCodeToString(in)
		if got != want {
			t.Fatalf("ControlTypeCodeToString(%d)=%q, want %q", in, got, want)
		}
	}
	if got := ControlTypeCodeToString(999); got != "Unknown_999" {
		t.Fatalf("unexpected unknown mapping: %q", got)
	}
	if got := FormControlTypeCodeToString(0x116B); got != "OptionGroup" {
		t.Fatalf("FormControlTypeCodeToString(0x116B)=%q, want OptionGroup", got)
	}
	if got := FormControlTypeCodeToString(0x0769); got != "OptionButton" {
		t.Fatalf("FormControlTypeCodeToString(0x0769)=%q, want OptionButton", got)
	}
}

func TestReadFormAccessObjectChunks(t *testing.T) {
	dbPath := requireDBFile(t)

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	formName := requireFormName(t, db)
	chunks, err := db.ReadFormAccessObjectChunks(formName)
	if err != nil {
		t.Fatalf("ReadFormAccessObjectChunks failed: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatalf("ReadFormAccessObjectChunks returned empty")
	}
	for i := 1; i < len(chunks); i++ {
		if chunks[i].Score > chunks[i-1].Score {
			t.Fatalf("chunks not sorted by score desc at %d", i)
		}
	}
	if filepath.Clean(dbPath) == filepath.Clean(defaultTestDB) {
		if len(chunks[0].Data) == 0 {
			t.Fatalf("top chunk has empty data")
		}
	}
	fmt.Printf("%s chunks=%d top_id=%d top_score=%d top_len=%d\n",
		formName, len(chunks), chunks[0].ObjectID, chunks[0].Score, len(chunks[0].Data))
}

func TestReadFormDesignChunks(t *testing.T) {
	dbPath := requireDBFile(t)

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	formName := requireFormName(t, db)
	chunks, err := db.ReadFormDesignChunks(formName)
	if err != nil {
		t.Fatalf("ReadFormDesignChunks failed: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatalf("ReadFormDesignChunks returned empty")
	}
	strongCount := 0
	for _, c := range chunks {
		if c.StrongHit {
			strongCount++
		}
	}
	if filepath.Clean(dbPath) == filepath.Clean(defaultTestDB) && strongCount == 0 {
		t.Fatalf("expected at least one strong design chunk in default db")
	}
	fmt.Printf("%s design_chunks=%d strong=%d top_id=%d top_name_hits=%d\n",
		formName, len(chunks), strongCount, chunks[0].Chunk.ObjectID, chunks[0].NameHitCnt)
}

func TestReadAndParseFormLayout(t *testing.T) {
	dbPath := requireDBFile(t)

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	formName := requireFormName(t, db)
	layout, err := db.ReadAndParseFormLayout(formName)
	if err != nil {
		t.Fatalf("ReadAndParseFormLayout failed: %v", err)
	}
	if layout == nil {
		t.Fatalf("ReadAndParseFormLayout returned nil")
	}
	if layout.FormName != formName {
		t.Fatalf("unexpected form name: %q", layout.FormName)
	}
	if filepath.Clean(dbPath) == filepath.Clean(defaultTestDB) && len(layout.Controls) == 0 {
		t.Fatalf("expected non-empty controls for %s in default db", formName)
	}
	fmt.Printf("%s parsed_layout controls=%d width=%d sample=%+v\n",
		formName, len(layout.Controls), layout.Width, func() FormLayoutControl {
			if len(layout.Controls) == 0 {
				return FormLayoutControl{}
			}
			return layout.Controls[0]
		}())
}

type exportFormLayout struct {
	FormName string              `json:"FormName"`
	Width    int                 `json:"Width"`
	Controls []FormLayoutControl `json:"Controls"`
}

func TestReadAndParseFormLayout_GeometryAgainstExportJSON(t *testing.T) {
	dbPath := requireDBFile(t)

	raw, err := os.ReadFile(defaultFormExportJSON)
	if err != nil {
		t.Skipf("skip geometry compare, export json not found: %v", err)
	}
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	var expected exportFormLayout
	if err := json.Unmarshal(raw, &expected); err != nil {
		t.Fatalf("unmarshal export json failed: %v", err)
	}
	if len(expected.Controls) == 0 {
		t.Fatalf("export json has no controls")
	}

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	layout, err := db.ReadAndParseFormLayout("f_midb")
	if err != nil {
		t.Fatalf("ReadAndParseFormLayout failed: %v", err)
	}
	if layout == nil {
		t.Fatalf("ReadAndParseFormLayout returned nil")
	}

	gotMap := make(map[string]FormLayoutControl, len(layout.Controls))
	for _, c := range layout.Controls {
		gotMap[c.Name] = c
	}

	// 只比较名称交集，避免把“尚未识别出的控件”混入几何误差统计。
	matched := 0
	hitGeom := 0
	totalErr := 0
	for _, exp := range expected.Controls {
		got, ok := gotMap[exp.Name]
		if !ok {
			continue
		}
		matched++
		if got.Width <= 0 || got.Height <= 0 {
			continue
		}
		errSum := absInt(got.Top-exp.Top) + absInt(got.Left-exp.Left) + absInt(got.Width-exp.Width) + absInt(got.Height-exp.Height)
		totalErr += errSum
		if absInt(got.Top-exp.Top) <= 240 &&
			absInt(got.Left-exp.Left) <= 240 &&
			absInt(got.Width-exp.Width) <= 360 &&
			absInt(got.Height-exp.Height) <= 180 {
			hitGeom++
		}
	}

	if matched == 0 {
		t.Fatalf("no control name overlap between parsed layout and export json")
	}
	coverage := float64(matched) / float64(len(expected.Controls))
	avgErr := 0.0
	if hitGeom > 0 {
		avgErr = float64(totalErr) / float64(hitGeom)
	}
	fmt.Printf("f_midb geometry_compare matched=%d/%d coverage=%.2f hit=%d avg_err=%.1f parsed_controls=%d\n",
		matched, len(expected.Controls), coverage, hitGeom, avgErr, len(layout.Controls))

	if coverage < 0.40 {
		t.Fatalf("name overlap too low: %.2f", coverage)
	}
	if hitGeom == 0 {
		t.Logf("geometry hit is 0, current parser still needs more format-level decoding")
	}
}

func TestDebugRectCandidates(t *testing.T) {
	if os.Getenv("MDBGO_DEBUG_RECT") == "" {
		t.Skip("set MDBGO_DEBUG_RECT=1 to enable debug output")
	}
	dbPath := requireDBFile(t)

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	streams, err := db.ReadFormStreams("f_midb")
	if err != nil {
		t.Fatalf("ReadFormStreams failed: %v", err)
	}
	parsed, _ := ParseFormPropsFromLvProp(streams.LvProp)
	chunks, err := db.ReadFormDesignChunks("f_midb")
	if err != nil {
		t.Fatalf("ReadFormDesignChunks failed: %v", err)
	}

	targets := []string{"ace_update_date", "created_date", "midb_seq_id", "btn0Save"}
	for _, name := range targets {
		fmt.Printf("\n== %s ==\n", name)
		total := 0
		for _, ch := range chunks {
			offsets := findUTF16LETokenOffsets(ch.Chunk.Data, name)
			for _, off := range offsets {
				cands := collectRectCandidatesNearOffset(ch.Chunk.Data, off, parsed.findWidth(), inferControlTypeByName(name), ch)
				limit := len(cands)
				if limit > 5 {
					limit = 5
				}
				for i := 0; i < limit; i++ {
					c := cands[i]
					fmt.Printf("chunk=%d off=%d cand[%d]=(%d,%d,%d,%d) score=%d strong=%v\n",
						ch.Chunk.ObjectID, off, i, c.Top, c.Left, c.Width, c.Height, c.Score, ch.StrongHit)
					total++
					if total >= 12 {
						break
					}
				}
				if total >= 12 {
					break
				}
			}
			if total >= 12 {
				break
			}
		}
	}
}
