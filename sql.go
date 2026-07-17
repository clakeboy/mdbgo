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
#cgo CFLAGS: -DHAVE_REALLOCF=1
#include <stdlib.h>
#include "bridge.h"
*/
import "C"

import (
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
	Columns   []ColumnSchema
}

// Tables 返回当前数据库中的用户表名列表。
func (db *DB) Tables() ([]string, error) {
	if db == nil || db.ptr == nil {
		return nil, errors.New("db is closed")
	}

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
	return result, nil
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

// Query 执行 SQL 并返回结果集。
// 当前不允许 CONNECT/DISCONNECT 语句，以避免影响共享 DB 句柄生命周期。
func (db *DB) Query(sql string) (*TableData, error) {
	if db == nil || db.ptr == nil {
		return nil, errors.New("db is closed")
	}
	if sql == "" {
		return nil, errors.New("sql is empty")
	}

	cSQL := C.CString(sql)
	defer C.free(unsafe.Pointer(cSQL))

	errBuf := make([]byte, cErrBufSize)
	var raw C.mdbgo_table_data_t

	rc := C.mdbgo_query(db.ptr, cSQL, &raw, (*C.char)(unsafe.Pointer(&errBuf[0])), C.size_t(len(errBuf)))
	if rc != 0 {
		return nil, errors.New(cStringFromBuf(errBuf))
	}
	defer C.mdbgo_free_table_data(&raw)

	return rawTableDataToGo(&raw)
}

// Schema 返回指定表的 schema 信息。
func (db *DB) Schema(tableName string) (*TableSchema, error) {
	if db == nil || db.ptr == nil {
		return nil, errors.New("db is closed")
	}
	if tableName == "" {
		return nil, errors.New("table name is empty")
	}

	cName := C.CString(tableName)
	defer C.free(unsafe.Pointer(cName))

	errBuf := make([]byte, cErrBufSize)
	var raw C.mdbgo_table_schema_t

	rc := C.mdbgo_get_table_schema(db.ptr, cName, &raw, (*C.char)(unsafe.Pointer(&errBuf[0])), C.size_t(len(errBuf)))
	if rc != 0 {
		return nil, errors.New(cStringFromBuf(errBuf))
	}
	defer C.mdbgo_free_table_schema(&raw)

	colCount := int(raw.col_count)
	cols := make([]ColumnSchema, colCount)
	if colCount > 0 {
		cCols := unsafe.Slice((*C.mdbgo_column_schema_t)(unsafe.Pointer(raw.columns)), colCount)
		for i := 0; i < colCount; i++ {
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
		Columns:   cols,
	}, nil
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
	if colCount > 0 && rowCount > 0 {
		total := rowCount * colCount
		cCells := unsafe.Slice((**C.char)(unsafe.Pointer(raw.cells)), total)

		for r := 0; r < rowCount; r++ {
			row := make([]string, colCount)
			base := r * colCount
			for c := 0; c < colCount; c++ {
				row[c] = C.GoString(cCells[base+c])
			}
			rows[r] = row
		}
	}

	return &TableData{Columns: columns, Rows: rows}, nil
}

// escapeSQLString 转义 SQL 字符串字面量中的单引号。
func escapeSQLString(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}
