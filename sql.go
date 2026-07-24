package mdbgo

/*
#cgo CFLAGS: -I${SRCDIR}/internal/bundled
#cgo CFLAGS: -I${SRCDIR}/internal/bundled/include
#cgo CFLAGS: -DTLS=__thread
#cgo CFLAGS: -DICONV_CONST=const
#cgo CFLAGS: -DHAVE_STRTOK_R=1
#cgo CFLAGS: -DHAVE_SETLOCALE=1
#cgo CFLAGS: -DHAVE_SYS_STAT_H=1
#cgo CFLAGS: -DHAVE_SYS_TYPES_H=1
#cgo darwin CFLAGS: -DHAVE_REALLOCF=1
#include <stdlib.h>
#include "bridge.h"
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unsafe"
)

// TableData 是读取整张表后的内存对象。
// Rows 的每一项对应一行，列顺序与 Columns 一致。
type TableData struct {
	Columns []string
	Rows    [][]string
	// Nulls mirrors Rows and distinguishes database NULL from an empty value.
	// Legacy producers may leave it nil.
	Nulls [][]bool `json:"Nulls,omitempty"`
}

// ColumnSchema 描述单列的元信息。
type ColumnSchema struct {
	Name     string
	ColType  int
	TypeName string
	Size     int
	Prec     int
	Scale    int
	IsFixed  bool
}

// TableSchema 描述一张表的元信息。
type TableSchema struct {
	TableName string
	RowCount  uint64
	Columns   []ColumnSchema
}

// Tables 返回当前数据库中的用户表名列表。
func (db *DB) Tables() ([]string, error) {
	if db == nil || db.ptr == nil {
		return nil, errors.New("db is closed")
	}
	db.metaMu.Lock()
	if db.tableCache != nil {
		result := append([]string(nil), db.tableCache...)
		db.metaMu.Unlock()
		return result, nil
	}
	db.metaMu.Unlock()

	errBuf := make([]byte, cErrBufSize)
	var cNames **C.char
	var cCount C.size_t

	rc := C.mdbgo_list_tables(db.ptr, &cNames, &cCount, (*C.char)(unsafe.Pointer(&errBuf[0])), C.size_t(len(errBuf)))
	if rc != 0 {
		return nil, errors.New(cStringFromBuf(errBuf))
	}
	defer C.mdbgo_free_string_array(cNames, cCount)

	count := int(cCount)
	result := make([]string, 0, count)
	if count == 0 {
		return result, nil
	}

	// 把 C 的 char** 转成 Go 切片视图，再逐项拷贝成 Go string。
	names := unsafe.Slice((**C.char)(unsafe.Pointer(cNames)), count)
	for i := 0; i < count; i++ {
		result = append(result, C.GoString(names[i]))
	}
	db.metaMu.Lock()
	db.tableCache = append([]string(nil), result...)
	db.tableLookup = make(map[string]string, len(result))
	for _, name := range result {
		db.tableLookup[strings.ToLower(name)] = name
	}
	db.metaMu.Unlock()
	return result, nil
}

// Views 返回当前数据库中的 Access 保存查询名称列表。
// 与原版 mdb-queries 一致，列表中也可能包含 Access 自动生成的 ~sq_ 查询。
func (db *DB) Views() ([]string, error) {
	if db == nil || db.ptr == nil {
		return nil, errors.New("db is closed")
	}
	db.metaMu.Lock()
	if db.viewCache != nil {
		result := append([]string(nil), db.viewCache...)
		db.metaMu.Unlock()
		return result, nil
	}
	db.metaMu.Unlock()

	errBuf := make([]byte, cErrBufSize)
	var cNames **C.char
	var cCount C.size_t

	rc := C.mdbgo_list_views(db.ptr, &cNames, &cCount, (*C.char)(unsafe.Pointer(&errBuf[0])), C.size_t(len(errBuf)))
	if rc != 0 {
		return nil, errors.New(cStringFromBuf(errBuf))
	}
	defer C.mdbgo_free_string_array(cNames, cCount)

	count := int(cCount)
	result := make([]string, 0, count)
	if count == 0 {
		return result, nil
	}
	names := unsafe.Slice((**C.char)(unsafe.Pointer(cNames)), count)
	for i := 0; i < count; i++ {
		result = append(result, C.GoString(names[i]))
	}
	db.metaMu.Lock()
	db.viewCache = append([]string(nil), result...)
	db.viewLookup = make(map[string]string, len(result))
	for _, name := range result {
		db.viewLookup[strings.ToLower(name)] = name
	}
	db.metaMu.Unlock()
	return result, nil
}

// ViewSQL 从 MSysQueries 还原指定 Access 保存查询的 SQL。
// 当前完整支持普通 SELECT View，包括 JOIN、参数、分组、HAVING 和排序。
func (db *DB) ViewSQL(viewName string) (string, error) {
	if db == nil || db.ptr == nil {
		return "", errors.New("db is closed")
	}
	if strings.TrimSpace(viewName) == "" {
		return "", errors.New("view name is empty")
	}

	cName := C.CString(viewName)
	defer C.free(unsafe.Pointer(cName))
	errBuf := make([]byte, cErrBufSize)
	var cSQL *C.char
	rc := C.mdbgo_get_view_sql(db.ptr, cName, &cSQL, (*C.char)(unsafe.Pointer(&errBuf[0])), C.size_t(len(errBuf)))
	if rc != 0 {
		return "", errors.New(cStringFromBuf(errBuf))
	}
	defer C.mdbgo_free_string(cSQL)
	return C.GoString(cSQL), nil
}

// TableRowCount 从 Access 表定义页读取记录数，不扫描或加载数据行。
//
// 该值通常能快速反映当前记录数；但 MDB 文件未正常关闭时，表定义页中的
// 记录数可能为 0，此时如需精确结果应通过查询或 ReadTable 扫描数据行。
func (db *DB) TableRowCount(tableName string) (uint64, error) {
	if db == nil || db.ptr == nil {
		return 0, errors.New("db is closed")
	}
	if tableName == "" {
		return 0, errors.New("table name is empty")
	}

	cName := C.CString(tableName)
	defer C.free(unsafe.Pointer(cName))

	errBuf := make([]byte, cErrBufSize)
	var count C.size_t
	rc := C.mdbgo_table_row_count(
		db.ptr,
		cName,
		&count,
		(*C.char)(unsafe.Pointer(&errBuf[0])),
		C.size_t(len(errBuf)),
	)
	if rc != 0 {
		return 0, errors.New(cStringFromBuf(errBuf))
	}
	return uint64(count), nil
}

// ReadTable 读取整张表到内存并返回。
func (db *DB) ReadTable(tableName string) (*TableData, error) {
	if db == nil || db.ptr == nil {
		return nil, errors.New("db is closed")
	}
	if tableName == "" {
		return nil, errors.New("table name is empty")
	}

	cName := C.CString(tableName)
	defer C.free(unsafe.Pointer(cName))

	errBuf := make([]byte, cErrBufSize)
	var raw C.mdbgo_table_data_t

	rc := C.mdbgo_read_table(db.ptr, cName, &raw, (*C.char)(unsafe.Pointer(&errBuf[0])), C.size_t(len(errBuf)))
	if rc != 0 {
		return nil, errors.New(cStringFromBuf(errBuf))
	}
	defer C.mdbgo_free_table_data(&raw)

	return rawTableDataToGo(&raw)
}

// Query 执行只读 Access SQL 并返回兼容的字符串结果集。
// 新代码优先使用 QueryContext，以获得类型化结果、参数和取消支持。
func (db *DB) Query(sql string) (*TableData, error) {
	rows, err := db.QueryContext(context.Background(), sql, nil)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols := rows.Columns()
	result := &TableData{Columns: make([]string, len(cols))}
	for i := range cols {
		result.Columns[i] = cols[i].Name
	}
	for rows.Next() {
		values := rows.Values()
		row := make([]string, len(values))
		nullRow := make([]bool, len(values))
		for i := range values {
			row[i] = values[i].String()
			nullRow[i] = values[i].IsNull()
		}
		result.Rows = append(result.Rows, row)
		result.Nulls = append(result.Nulls, nullRow)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// queryLegacy retains the bundled mdb-sql path for compatibility benchmarks
// while the Go engine is being validated. It is intentionally not exported.
func (db *DB) queryLegacy(sql string) (*TableData, error) {
	if strings.TrimSpace(sql) == "" {
		return nil, errors.New("sql is empty")
	}
	session, release, err := db.acquireQuerySession(context.Background())
	if err != nil {
		return nil, err
	}
	defer release()
	cSQL := C.CString(sql)
	defer C.free(unsafe.Pointer(cSQL))
	errBuf := make([]byte, cErrBufSize)
	var raw C.mdbgo_table_data_t
	rc := C.mdbgo_query(session.ptr, cSQL, &raw, (*C.char)(unsafe.Pointer(&errBuf[0])), C.size_t(len(errBuf)))
	if rc != 0 {
		return nil, errors.New(cStringFromBuf(errBuf))
	}
	defer C.mdbgo_free_table_data(&raw)
	return rawTableDataToGo(&raw)
}

// Schemas 一次读取并返回所有用户表的 schema 信息。
func (db *DB) Schemas() ([]*TableSchema, error) {
	if db == nil || db.ptr == nil {
		return nil, errors.New("db is closed")
	}

	errBuf := make([]byte, cErrBufSize)
	var raw C.mdbgo_table_schemas_t
	rc := C.mdbgo_get_table_schemas(db.ptr, &raw, (*C.char)(unsafe.Pointer(&errBuf[0])), C.size_t(len(errBuf)))
	if rc != 0 {
		return nil, errors.New(cStringFromBuf(errBuf))
	}
	defer C.mdbgo_free_table_schemas(&raw)

	count := int(raw.count)
	result := make([]*TableSchema, count)
	if count > 0 {
		rawSchemas := unsafe.Slice((*C.mdbgo_table_schema_t)(unsafe.Pointer(raw.schemas)), count)
		for i := range rawSchemas {
			result[i] = rawTableSchemaToGo(&rawSchemas[i])
		}
	}

	db.metaMu.Lock()
	db.tableCache = make([]string, 0, count)
	db.tableLookup = make(map[string]string, count)
	db.schemaCache = make(map[string]*TableSchema, count)
	for _, schema := range result {
		db.tableCache = append(db.tableCache, schema.TableName)
		cacheKey := strings.ToLower(schema.TableName)
		db.tableLookup[cacheKey] = schema.TableName
		db.schemaCache[cacheKey] = cloneTableSchema(schema)
	}
	db.metaMu.Unlock()
	return result, nil
}

// Schema 返回指定表的 schema 信息，包括表定义页中的记录数。
func (db *DB) Schema(tableName string) (*TableSchema, error) {
	if db == nil || db.ptr == nil {
		return nil, errors.New("db is closed")
	}
	if tableName == "" {
		return nil, errors.New("table name is empty")
	}
	cacheKey := strings.ToLower(tableName)
	db.metaMu.Lock()
	if cached := db.schemaCache[cacheKey]; cached != nil {
		result := cloneTableSchema(cached)
		db.metaMu.Unlock()
		return result, nil
	}
	db.metaMu.Unlock()

	cName := C.CString(tableName)
	defer C.free(unsafe.Pointer(cName))

	errBuf := make([]byte, cErrBufSize)
	var raw C.mdbgo_table_schema_t

	rc := C.mdbgo_get_table_schema(db.ptr, cName, &raw, (*C.char)(unsafe.Pointer(&errBuf[0])), C.size_t(len(errBuf)))
	if rc != 0 {
		return nil, errors.New(cStringFromBuf(errBuf))
	}
	defer C.mdbgo_free_table_schema(&raw)

	result := rawTableSchemaToGo(&raw)
	db.metaMu.Lock()
	if db.schemaCache == nil {
		db.schemaCache = make(map[string]*TableSchema)
	}
	db.schemaCache[cacheKey] = cloneTableSchema(result)
	db.metaMu.Unlock()
	return result, nil
}

func rawTableSchemaToGo(raw *C.mdbgo_table_schema_t) *TableSchema {
	colCount := int(raw.col_count)
	cols := make([]ColumnSchema, colCount)
	if colCount > 0 {
		cCols := unsafe.Slice((*C.mdbgo_column_schema_t)(unsafe.Pointer(raw.columns)), colCount)
		for i := range cCols {
			cols[i] = ColumnSchema{
				Name:     C.GoString(cCols[i].name),
				ColType:  int(cCols[i].col_type),
				TypeName: C.GoString(cCols[i].col_type_name),
				Size:     int(cCols[i].col_size),
				Prec:     int(cCols[i].col_prec),
				Scale:    int(cCols[i].col_scale),
				IsFixed:  cCols[i].is_fixed != 0,
			}
		}
	}
	return &TableSchema{
		TableName: C.GoString(raw.table_name),
		RowCount:  uint64(raw.row_count),
		Columns:   cols,
	}
}

func cloneTableSchema(schema *TableSchema) *TableSchema {
	if schema == nil {
		return nil
	}
	return &TableSchema{
		TableName: schema.TableName,
		RowCount:  schema.RowCount,
		Columns:   append([]ColumnSchema(nil), schema.Columns...),
	}
}

// rawTableDataToGo 把 C 侧结果结构深拷贝为 Go 切片，随后可安全释放 C 内存。
func rawTableDataToGo(raw *C.mdbgo_table_data_t) (*TableData, error) {
	if raw == nil {
		return nil, errors.New("nil table data")
	}

	colCount := int(raw.col_count)
	rowCount := int(raw.row_count)
	if colCount < 0 || rowCount < 0 {
		return nil, fmt.Errorf("invalid table shape col=%d row=%d", colCount, rowCount)
	}

	columns := make([]string, colCount)
	if colCount > 0 {
		cCols := unsafe.Slice((**C.char)(unsafe.Pointer(raw.columns)), colCount)
		for i := 0; i < colCount; i++ {
			columns[i] = C.GoString(cCols[i])
		}
	}

	rows := make([][]string, rowCount)
	var nulls [][]bool
	var cNulls []C.uchar
	if raw.nulls != nil && colCount > 0 && rowCount > 0 {
		nulls = make([][]bool, rowCount)
		cNulls = unsafe.Slice((*C.uchar)(unsafe.Pointer(raw.nulls)), rowCount*colCount)
	}
	if colCount > 0 && rowCount > 0 {
		total := rowCount * colCount
		cCells := unsafe.Slice((**C.char)(unsafe.Pointer(raw.cells)), total)

		for r := 0; r < rowCount; r++ {
			row := make([]string, colCount)
			var nullRow []bool
			if nulls != nil {
				nullRow = make([]bool, colCount)
			}
			base := r * colCount
			for c := 0; c < colCount; c++ {
				row[c] = C.GoString(cCells[base+c])
				if nullRow != nil {
					nullRow[c] = cNulls[base+c] != 0
				}
			}
			rows[r] = row
			if nullRow != nil {
				nulls[r] = nullRow
			}
		}
	}

	return &TableData{Columns: columns, Rows: rows, Nulls: nulls}, nil
}

// escapeSQLString 转义 SQL 字符串字面量中的单引号。
func escapeSQLString(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}
