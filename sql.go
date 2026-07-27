package mdbgo

import (
	"context"
	"errors"
	"strings"
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
	if db == nil || db.handle == nil {
		return nil, errors.New("db is closed")
	}
	db.metaMu.Lock()
	if db.tableCache != nil {
		result := append([]string(nil), db.tableCache...)
		db.metaMu.Unlock()
		return result, nil
	}
	db.metaMu.Unlock()

	result, err := db.puregoDB.Tables()
	if err != nil {
		return nil, err
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
	if db == nil || db.handle == nil {
		return nil, errors.New("db is closed")
	}
	db.metaMu.Lock()
	if db.viewCache != nil {
		result := append([]string(nil), db.viewCache...)
		db.metaMu.Unlock()
		return result, nil
	}
	db.metaMu.Unlock()

	result, err := db.puregoDB.Views()
	if err != nil {
		return nil, err
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
	if db == nil || db.handle == nil {
		return "", errors.New("db is closed")
	}
	if strings.TrimSpace(viewName) == "" {
		return "", errors.New("view name is empty")
	}
	return db.puregoDB.ViewSQL(viewName)
}

// TableRowCount 从 Access 表定义页读取记录数，不扫描或加载数据行。
//
// 该值通常能快速反映当前记录数；但 MDB 文件未正常关闭时，表定义页中的
// 记录数可能为 0，此时如需精确结果应通过查询或 ReadTable 扫描数据行。
func (db *DB) TableRowCount(tableName string) (uint64, error) {
	if db == nil || db.handle == nil {
		return 0, errors.New("db is closed")
	}
	if tableName == "" {
		return 0, errors.New("table name is empty")
	}
	count, err := db.puregoDB.RowCount(tableName)
	if err != nil {
		return 0, err
	}
	return uint64(count), nil
}

// ReadTable 读取整张表到内存并返回。
func (db *DB) ReadTable(tableName string) (*TableData, error) {
	if db == nil || db.handle == nil {
		return nil, errors.New("db is closed")
	}
	if tableName == "" {
		return nil, errors.New("table name is empty")
	}

	schema, err := db.puregoDB.GetTableSchema(tableName)
	if err != nil {
		return nil, err
	}

	rows, err := db.puregoDB.ReadTableData(tableName)
	if err != nil {
		return nil, err
	}

	columns := make([]string, len(schema.Columns))
	for i, col := range schema.Columns {
		columns[i] = col.Name
	}

	return &TableData{Columns: columns, Rows: rows}, nil
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

// Schemas 一次读取并返回所有用户表的 schema 信息。
func (db *DB) Schemas() ([]*TableSchema, error) {
	if db == nil || db.handle == nil {
		return nil, errors.New("db is closed")
	}

	tables, err := db.puregoDB.Tables()
	if err != nil {
		return nil, err
	}

	result := make([]*TableSchema, 0, len(tables))
	for _, tableName := range tables {
		schema, err := db.puregoDB.GetTableSchema(tableName)
		if err != nil {
			continue
		}
		ts := &TableSchema{
			TableName: tableName,
			RowCount:  uint64(schema.RowCount),
			Columns:   make([]ColumnSchema, len(schema.Columns)),
		}
		for i, col := range schema.Columns {
			ts.Columns[i] = ColumnSchema{
				Name:     col.Name,
				ColType:  col.Type,
				TypeName: col.TypeName,
				Size:     col.Size,
				Prec:     col.Prec,
				Scale:    col.Scale,
				IsFixed:  col.IsFixed,
			}
		}
		result = append(result, ts)
	}

	db.metaMu.Lock()
	db.tableCache = make([]string, 0, len(result))
	db.tableLookup = make(map[string]string, len(result))
	db.schemaCache = make(map[string]*TableSchema, len(result))
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
	if db == nil || db.handle == nil {
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

	pureSchema, err := db.puregoDB.GetTableSchema(tableName)
	if err != nil {
		return nil, err
	}

	result := &TableSchema{
		TableName: tableName,
		RowCount:  uint64(pureSchema.RowCount),
		Columns:   make([]ColumnSchema, len(pureSchema.Columns)),
	}
	for i, col := range pureSchema.Columns {
		result.Columns[i] = ColumnSchema{
			Name:     col.Name,
			ColType:  col.Type,
			TypeName: col.TypeName,
			Size:     col.Size,
			Prec:     col.Prec,
			Scale:    col.Scale,
			IsFixed:  col.IsFixed,
		}
	}

	db.metaMu.Lock()
	if db.schemaCache == nil {
		db.schemaCache = make(map[string]*TableSchema)
	}
	db.schemaCache[cacheKey] = cloneTableSchema(result)
	db.metaMu.Unlock()
	return result, nil
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

// escapeSQLString 转义 SQL 字符串字面量中的单引号。
func escapeSQLString(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

// queryLegacy 保留兼容别名，为新引擎提供引用。
func (db *DB) queryLegacy(sql string) (*TableData, error) {
	if strings.TrimSpace(sql) == "" {
		return nil, errors.New("sql is empty")
	}
	return db.Query(sql)
}

// readTableBatched 读取表数据，支持过滤列和限制行数。
func (db *DB) readTableBatched(ctx context.Context, tableName string, requestedColumns []string, maxRows int) (*TableData, error) {
	schema, err := db.puregoDB.GetTableSchema(tableName)
	if err != nil {
		return nil, err
	}

	rows, err := db.puregoDB.ReadTableData(tableName)
	if err != nil {
		return nil, err
	}

	columns := make([]string, 0, len(schema.Columns))
	colIndex := make([]int, 0, len(schema.Columns))
	if len(requestedColumns) > 0 {
		requestedLower := make(map[string]bool, len(requestedColumns))
		for _, rc := range requestedColumns {
			requestedLower[strings.ToLower(rc)] = true
		}
		for i, col := range schema.Columns {
			if requestedLower[strings.ToLower(col.Name)] {
				columns = append(columns, col.Name)
				colIndex = append(colIndex, i)
			}
		}
	} else {
		for i, col := range schema.Columns {
			columns = append(columns, col.Name)
			colIndex = append(colIndex, i)
		}
	}

	result := &TableData{Columns: columns}
	limit := len(rows)
	if maxRows > 0 && maxRows < limit {
		limit = maxRows
	}
	for r := 0; r < limit; r++ {
		if r&1023 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		row := make([]string, len(colIndex))
		for j, ci := range colIndex {
			if ci < len(rows[r]) {
				row[j] = rows[r][ci]
			}
		}
		result.Rows = append(result.Rows, row)
	}

	return result, nil
}
