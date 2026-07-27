package purego

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// MDB 数据库结构
type MDB struct {
	handle *MdbHandle
}

// Open 打开 MDB 文件
func OpenMDB(filename string) (*MDB, error) {
	handle, err := Open(filename, MDBNoFlags)
	if err != nil {
		return nil, err
	}
	return &MDB{handle: handle}, nil
}

// OpenFromBuffer 从缓冲区打开 MDB 文件
func OpenFromBuffer(buffer []byte) (*MDB, error) {
	handle, err := OpenBuffer(buffer, MDBNoFlags)
	if err != nil {
		return nil, err
	}
	return &MDB{handle: handle}, nil
}

// Close 关闭数据库
func (mdb *MDB) Close() {
	if mdb.handle != nil {
		mdb.handle.Close()
		mdb.handle = nil
	}
}

// GetHandle 获取底层句柄
func (mdb *MDB) GetHandle() *MdbHandle {
	return mdb.handle
}

// Tables 返回所有用户表名
func (mdb *MDB) Tables() ([]string, error) {
	if mdb.handle == nil {
		return nil, fmt.Errorf("数据库未打开")
	}

	entries := mdb.handle.ReadCatalog(MDBTable)
	var tables []string
	for _, entry := range entries {
		if IsUserTableEntry(entry) {
			tables = append(tables, entry.ObjectName)
		}
	}
	return tables, nil
}

// Views 返回所有视图（保存的查询）名
func (mdb *MDB) Views() ([]string, error) {
	if mdb.handle == nil {
		return nil, fmt.Errorf("数据库未打开")
	}

	entries := mdb.handle.ReadCatalog(MDBQuery)
	var views []string
	for _, entry := range entries {
		views = append(views, entry.ObjectName)
	}
	return views, nil
}

// TableNames 返回所有对象名（包括表、查询等）
func (mdb *MDB) TableNames() ([]string, error) {
	if mdb.handle == nil {
		return nil, fmt.Errorf("数据库未打开")
	}

	entries := mdb.handle.ReadCatalog(MDBAny)
	var names []string
	for _, entry := range entries {
		names = append(names, entry.ObjectName)
	}
	return names, nil
}

// TableSchema 获取表结构
type ColumnSchema struct {
	Name     string
	Type     int
	TypeName string
	Size     int
	Prec     int
	Scale    int
	IsFixed  bool
}

type TableSchema struct {
	Name     string
	Columns  []ColumnSchema
	RowCount uint
}

// GetTableSchema 获取指定表的结构
func (mdb *MDB) GetTableSchema(tableName string) (*TableSchema, error) {
	if mdb.handle == nil {
		return nil, fmt.Errorf("数据库未打开")
	}

	entry := mdb.handle.GetCatalogEntryByName(tableName)
	if entry == nil || entry.ObjectType != MDBTable {
		// 当前目录可能刚被 Views 刷新为查询条目；按表目录重新定位同名物理表。
		mdb.handle.ReadCatalog(MDBTable)
		entry = mdb.handle.GetCatalogEntryByName(tableName)
	}
	if entry == nil || entry.ObjectType != MDBTable {
		return nil, fmt.Errorf("表 %s 不存在", tableName)
	}

	table := mdb.handle.ReadTable(entry)
	if table == nil {
		return nil, fmt.Errorf("无法读取表 %s", tableName)
	}
	defer mdb.handle.FreeTableDef(table)

	mdb.handle.ReadColumns(table)

	schema := &TableSchema{
		Name:     tableName,
		Columns:  make([]ColumnSchema, 0),
		RowCount: table.NumRows,
	}

	for _, col := range table.Columns {
		colSchema := ColumnSchema{
			Name:     col.Name,
			Type:     col.ColType,
			TypeName: ColumnTypeName(col.ColType),
			Size:     col.ColSize,
			Prec:     col.ColPrec,
			Scale:    col.ColScale,
			IsFixed:  col.IsFixed,
		}
		schema.Columns = append(schema.Columns, colSchema)
	}

	return schema, nil
}

// ReadTableData 读取表数据，返回行数据和 null 标记（与行/列形状相同）。
func (mdb *MDB) ReadTableData(tableName string) ([][]string, [][]bool, error) {
	return mdb.ReadTableDataColumns(tableName, nil, 0)
}

// ReadTableDataColumns 读取指定列的数据。columnNames 为空时读取全部列，
// maxRows <= 0 时读取全部行。
func (mdb *MDB) ReadTableDataColumns(tableName string, columnNames []string, maxRows int) ([][]string, [][]bool, error) {
	return mdb.ReadTableDataColumnsContext(context.Background(), tableName, columnNames, maxRows)
}

// ReadTableDataColumnsContext 与 ReadTableDataColumns 相同，但会在扫描期间响应取消。
func (mdb *MDB) ReadTableDataColumnsContext(ctx context.Context, tableName string, columnNames []string, maxRows int) ([][]string, [][]bool, error) {
	if mdb.handle == nil {
		return nil, nil, fmt.Errorf("数据库未打开")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	entry := mdb.handle.GetCatalogEntryByName(tableName)
	if entry == nil || entry.ObjectType != MDBTable {
		// 同名保存查询会覆盖当前目录查找结果，重新加载物理表目录后再读取数据。
		mdb.handle.ReadCatalog(MDBTable)
		entry = mdb.handle.GetCatalogEntryByName(tableName)
	}
	if entry == nil || entry.ObjectType != MDBTable {
		return nil, nil, fmt.Errorf("表 %s 不存在", tableName)
	}

	table := mdb.handle.ReadTable(entry)
	if table == nil {
		return nil, nil, fmt.Errorf("无法读取表 %s", tableName)
	}
	defer mdb.handle.FreeTableDef(table)

	if mdb.handle.ReadColumns(table) == nil {
		return nil, nil, fmt.Errorf("无法读取表 %s 的列", tableName)
	}

	columnIndexes := make([]int, 0, table.NumCols)
	if len(columnNames) == 0 {
		for i := 0; i < int(table.NumCols); i++ {
			columnIndexes = append(columnIndexes, i)
		}
	} else {
		for _, name := range columnNames {
			index := -1
			for i, col := range table.Columns {
				if strings.EqualFold(col.Name, name) {
					index = i
					break
				}
			}
			if index < 0 {
				return nil, nil, fmt.Errorf("表 %s 中不存在列 %s", tableName, name)
			}
			columnIndexes = append(columnIndexes, index)
		}
	}

	// 只绑定调用方需要的列，并记录每行转换后的实际字节长度。
	boundValues := make([][]byte, len(columnIndexes))
	boundLengths := make([]int, len(columnIndexes))
	for i, columnIndex := range columnIndexes {
		boundValues[i] = make([]byte, MDBBindSize)
		ret := table.BindColumn(columnIndex+1, boundValues[i], &boundLengths[i])
		if ret == -1 {
			boundValues[i] = nil
		}
	}

	nCols := len(columnIndexes)
	capacity := int(table.NumRows)
	if maxRows > 0 && (capacity == 0 || maxRows < capacity) {
		capacity = maxRows
	}
	result := make([][]string, 0, capacity)
	nulls := make([][]bool, 0, capacity)
	table.RewindTable()
	for table.FetchRow() {
		if len(result)&1023 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
		}
		row := make([]string, nCols)
		nullRow := make([]bool, nCols)
		for i, columnIndex := range columnIndexes {
			col := table.Columns[columnIndex]
			if col.IsNull {
				nullRow[i] = true
			} else if boundValues[i] != nil {
				length := boundLengths[i]
				if length < 0 {
					length = 0
				} else if length > len(boundValues[i]) {
					length = len(boundValues[i])
				}
				value := boundValues[i][:length]
				if col.ColType == MDBText || col.ColType == MDBMemo {
					row[i] = UTF16LEToString(value)
				} else {
					row[i] = string(value)
				}
			}
		}
		result = append(result, row)
		nulls = append(nulls, nullRow)
		if maxRows > 0 && len(result) >= maxRows {
			break
		}
	}

	return result, nulls, nil
}

// RowCount 获取表的行数
func (mdb *MDB) RowCount(tableName string) (int, error) {
	if mdb.handle == nil {
		return 0, fmt.Errorf("数据库未打开")
	}

	entry := mdb.handle.GetCatalogEntryByName(tableName)
	if entry == nil || entry.ObjectType != MDBTable {
		// 当前目录可能由查询 API 填充，行数读取必须使用物理表对应的目录条目。
		mdb.handle.ReadCatalog(MDBTable)
		entry = mdb.handle.GetCatalogEntryByName(tableName)
	}
	if entry == nil || entry.ObjectType != MDBTable {
		return 0, fmt.Errorf("表 %s 不存在", tableName)
	}

	table := mdb.handle.ReadTable(entry)
	if table == nil {
		return 0, fmt.Errorf("无法读取表 %s", tableName)
	}
	defer mdb.handle.FreeTableDef(table)

	return int(table.NumRows), nil
}

// FileFormat 获取文件格式信息
func (mdb *MDB) FileFormat() (string, int) {
	if mdb.handle == nil {
		return "", 0
	}

	jetVersion, pageSize := mdb.handle.GetFileFormat()
	var version string
	switch jetVersion {
	case MDBVerJet3:
		version = "Jet3"
	case MDBVerJet4:
		version = "Jet4"
	case MDBVerAccdb2007:
		version = "ACCDB 2007"
	case MDBVerAccdb2010:
		version = "ACCDB 2010"
	case MDBVerAccdb2013:
		version = "ACCDB 2013"
	case MDBVerAccdb2016:
		version = "ACCDB 2016"
	case MDBVerAccdb2019:
		version = "ACCDB 2019"
	default:
		version = fmt.Sprintf("Unknown (%x)", jetVersion)
	}
	return version, pageSize
}

// ExportToFile 导出表数据到文件
func (mdb *MDB) ExportToFile(tableName, filename string) error {
	data, _, err := mdb.ReadTableData(tableName)
	if err != nil {
		return err
	}

	schema, err := mdb.GetTableSchema(tableName)
	if err != nil {
		return err
	}

	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	// 写入表头
	headers := make([]string, len(schema.Columns))
	for i, col := range schema.Columns {
		headers[i] = col.Name
	}
	fmt.Fprintln(file, strings.Join(headers, "\t"))

	// 写入数据
	for _, row := range data {
		fmt.Fprintln(file, strings.Join(row, "\t"))
	}

	return nil
}
