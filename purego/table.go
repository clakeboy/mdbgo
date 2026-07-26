package purego

import (
	"encoding/binary"
	"fmt"
	"sort"
	"strings"
	"unicode/utf16"
)

// decompressUnicode 解压 Access 压缩 Unicode 格式。
// 规则：
//   - 跳过前导标记（BOM FF FE、压缩标记 00 FE、空对 00 00）
//   - 每个非零字节 = 一个 UTF-16LE 对（byte + 0x00）
//   - 零字节直接忽略
func decompressUnicode(src []byte) []uint16 {
	pos := 0
	if len(src) >= 2 {
		if (src[0] == 0xFF && src[1] == 0xFE) ||
			(src[0] == 0x00 && src[1] == 0xFE) ||
			(src[0] == 0x00 && src[1] == 0x00) {
			pos = 2
		}
	}
	var result []uint16
	for i := pos; i < len(src); i++ {
		if src[i] != 0 {
			result = append(result, uint16(src[i]))
		}
	}
	return result
}

// UTF16LEToString converts a UTF-16LE byte slice to a Go string.
// Handles BOM prefix (FF FE) and Access compressed Unicode format.
func UTF16LEToString(buf []byte) string {
	if len(buf) == 0 {
		return ""
	}
	// Check for Access compressed Unicode format
	if len(buf) >= 2 {
		if (buf[0] == 0xFF && buf[1] == 0xFE) ||
			(buf[0] == 0x00 && buf[1] == 0xFE) ||
			(buf[0] == 0x00 && buf[1] == 0x00) {
			u16s := decompressUnicode(buf)
			return string(utf16.Decode(u16s))
		}
	}
	// Standard UTF-16LE decoding
	end := len(buf)
	for i := 0; i+1 < len(buf); i += 2 {
		if buf[i] == 0 && buf[i+1] == 0 {
			end = i
			break
		}
	}
	if end == 0 {
		return ""
	}
	u16s := make([]uint16, end/2)
	for i := range u16s {
		u16s[i] = binary.LittleEndian.Uint16(buf[i*2:])
	}
	return string(utf16.Decode(u16s))
}

// AllocTableDef 分配表定义
func AllocTableDef(entry *MdbCatalogEntry) *MdbTableDef {
	table := &MdbTableDef{
		Entry:         entry,
		Name:          entry.ObjectName,
		Columns:       make([]*MdbColumn, 0),
		Indices:       make([]*MdbIndex, 0),
		TempTablePages: make([]interface{}, 0),
	}
	return table
}

// FreeTableDef 释放表定义
func (mdb *MdbHandle) FreeTableDef(table *MdbTableDef) {
	if table == nil {
		return
	}

	if table.IsTempTable {
		// 临时表的页面存储在内存中
		for _, page := range table.TempTablePages {
			_ = page
		}
		table.TempTablePages = nil
		// 临时表使用虚拟条目
		table.Entry = nil
	}

	mdb.FreeColumns(table.Columns)
	mdb.FreeIndices(table.Indices)
	table.UsageMap = nil
	table.FreeUsageMap = nil
}

// ReadTable 读取表定义
func (mdb *MdbHandle) ReadTable(entry *MdbCatalogEntry) *MdbTableDef {
	if mdb == nil || entry == nil {
		return nil
	}

	pgBuf := mdb.PgBuf[:]

	// 读取表定义页
	if mdb.ReadPg(entry.TablePg) == 0 {
		fmt.Printf("mdb_read_table: 无法读取页 %d\n", entry.TablePg)
		return nil
	}

	// 检查页面类型
	if GetByte(pgBuf, 0) != 0x02 {
		fmt.Printf("mdb_read_table: 页 %d 不是有效的表定义页 (首字节 = 0x%02X, 期望 0x02)\n",
			entry.TablePg, GetByte(pgBuf, 0))
		return nil
	}

	table := AllocTableDef(entry)

	// 读取表元数据
	GetInt16(pgBuf, 8) // len

	// 注意：如果数据库未正确关闭，num_rows 可能为零
	table.NumRows = uint(GetInt32(pgBuf, int(mdb.Fmt.TabNumRowsOffset)))
	table.NumVarCols = uint(GetInt16(pgBuf, int(mdb.Fmt.TabNumColsOffset)-2))
	table.NumCols = uint(GetInt16(pgBuf, int(mdb.Fmt.TabNumColsOffset)))
	table.NumIdxs = uint(GetInt32(pgBuf, int(mdb.Fmt.TabNumIdxsOffset)))
	table.NumRealIdxs = uint(GetInt32(pgBuf, int(mdb.Fmt.TabNumRidxsOffset)))

	// 读取使用映射
	pgRow := GetInt32(pgBuf, int(mdb.Fmt.TabUsageMapOffset))
	usageMap, usageMapSize, err := mdb.FindPgRow(pgRow)
	if err != nil {
		fmt.Printf("mdb_read_table: 无法找到页行 %d\n", pgRow)
		mdb.FreeTableDef(table)
		return nil
	}

	if usageMapSize < 1 {
		fmt.Printf("mdb_read_table: 无效的映射大小: %d\n", usageMapSize)
		mdb.FreeTableDef(table)
		return nil
	}

	table.UsageMap = make([]byte, usageMapSize)
	copy(table.UsageMap, usageMap)
	table.MapSize = usageMapSize

	// 读取空闲空间页映射
	pgRow = GetInt32(pgBuf, int(mdb.Fmt.TabFreeMapOffset))
	freeMap, freeMapSize, err := mdb.FindPgRow(pgRow)
	if err != nil {
		fmt.Printf("mdb_read_table: 无法找到页行 %d\n", pgRow)
		mdb.FreeTableDef(table)
		return nil
	}

	table.FreeUsageMap = make([]byte, freeMapSize)
	copy(table.FreeUsageMap, freeMap)
	table.FreemapSize = freeMapSize

	// 读取第一个数据页
	table.FirstDataPg = uint32(GetInt16(pgBuf, int(mdb.Fmt.TabFirstDpgOffset)))

	// 复制表属性
	if entry.Props != nil {
		for _, props := range entry.Props {
			if props != nil && props.Name == "" {
				table.Props = props
			}
		}
	}

	return table
}

// ReadTableByName 根据名称读取表
func (mdb *MdbHandle) ReadTableByName(tableName string, objType int) *MdbTableDef {
	if mdb == nil {
		return nil
	}

	mdb.ReadCatalog(objType)

	for i := 0; i < int(mdb.NumCatalog); i++ {
		entry := mdb.Catalog[i]
		if entry != nil && strings.EqualFold(entry.ObjectName, tableName) {
			return mdb.ReadTable(entry)
		}
	}

	return nil
}

// ReadPgIf32 从页面读取 32 位整数
func (mdb *MdbHandle) ReadPgIf32(curPos *int) uint32 {
	buf := make([]byte, 4)
	mdb.ReadPgIfN(buf, curPos, 4)
	return uint32(GetInt32(buf, 0))
}

// ReadPgIf16 从页面读取 16 位整数
func (mdb *MdbHandle) ReadPgIf16(curPos *int) uint16 {
	buf := make([]byte, 2)
	mdb.ReadPgIfN(buf, curPos, 2)
	return uint16(GetInt16(buf, 0))
}

// ReadPgIf8 从页面读取 8 位整数
func (mdb *MdbHandle) ReadPgIf8(curPos *int) byte {
	var buf [1]byte
	mdb.ReadPgIfN(buf[:], curPos, 1)
	return buf[0]
}

// ReadPgIfN 从页面读取指定长度的数据
func (mdb *MdbHandle) ReadPgIfN(buf []byte, curPos *int, length int) bool {
	if *curPos < 0 {
		return false
	}

	// 推进到包含第一个字节的页面
	for *curPos >= mdb.Fmt.PgSize {
		nextPg := GetInt32(mdb.PgBuf[:], 4)
		if mdb.ReadPg(uint32(nextPg)) == 0 {
			return false
		}
		*curPos -= (mdb.Fmt.PgSize - 8)
	}

	// 将页面数据复制到缓冲区
	remaining := length
	bufOffset := 0

	for *curPos+remaining >= mdb.Fmt.PgSize {
		pieceLen := mdb.Fmt.PgSize - *curPos
		if buf != nil {
			if bufOffset+pieceLen > len(buf) {
				return false
			}
			copy(buf[bufOffset:], mdb.PgBuf[*curPos:])
			bufOffset += pieceLen
		}
		remaining -= pieceLen

		nextPg := GetInt32(mdb.PgBuf[:], 4)
		if mdb.ReadPg(uint32(nextPg)) == 0 {
			return false
		}
		*curPos = 8
	}

	// 从最后一页复制数据
	if remaining > 0 && buf != nil {
		if bufOffset+remaining > len(buf) {
			return false
		}
		copy(buf[bufOffset:], mdb.PgBuf[*curPos:*curPos+remaining])
	}

	*curPos += remaining
	return true
}

// AppendColumn 追加列
func AppendColumn(columns []*MdbColumn, inCol *MdbColumn) []*MdbColumn {
	// 复制列信息
	newCol := &MdbColumn{
		Table:          inCol.Table,
		Name:           inCol.Name,
		ColType:        inCol.ColType,
		ColSize:        inCol.ColSize,
		IsFixed:        inCol.IsFixed,
		ColNum:         inCol.ColNum,
		FixedOffset:    inCol.FixedOffset,
		VarColNum:      inCol.VarColNum,
		RowColNum:      inCol.RowColNum,
		ColScale:       inCol.ColScale,
		ColPrec:        inCol.ColPrec,
		IsLongAuto:     inCol.IsLongAuto,
		IsUuidAuto:     inCol.IsUuidAuto,
	}
	return append(columns, newCol)
}

// FreeColumns 释放列
func (mdb *MdbHandle) FreeColumns(columns []*MdbColumn) {
	if columns == nil {
		return
	}
	// 在 Go 中，垃圾回收器会自动处理
}

// ReadColumns 读取列定义
func (mdb *MdbHandle) ReadColumns(table *MdbTableDef) []*MdbColumn {
	if mdb == nil || table == nil {
		return nil
	}

	table.Columns = make([]*MdbColumn, 0)
	curPos := int(mdb.Fmt.TabColsStartOffset) + int(table.NumRealIdxs)*int(mdb.Fmt.TabRidxEntrySize)

	// 读取列属性
	colBuf := make([]byte, mdb.Fmt.TabColEntrySize)

	for i := 0; i < int(table.NumCols); i++ {
		if !mdb.ReadPgIfN(colBuf, &curPos, int(mdb.Fmt.TabColEntrySize)) {
			mdb.FreeColumns(table.Columns)
			return nil
		}

		pcol := &MdbColumn{
			Table: table,
		}

		pcol.ColType = int(colBuf[0])
		pcol.ColNum = int(colBuf[mdb.Fmt.ColNumOffset])
		pcol.VarColNum = uint(GetInt16(colBuf, int(mdb.Fmt.TabColOffsetVar)))
		pcol.RowColNum = GetInt16(colBuf, int(mdb.Fmt.TabRowColNumOffset))

		if pcol.ColType == MDBNumeric || pcol.ColType == MDBMoney ||
			pcol.ColType == MDBFloat || pcol.ColType == MDBDouble {
			pcol.ColScale = int(colBuf[mdb.Fmt.ColScaleOffset])
			pcol.ColPrec = int(colBuf[mdb.Fmt.ColPrecOffset])
		}

		pcol.IsFixed = colBuf[mdb.Fmt.ColFlagsOffset]&0x01 != 0
		pcol.IsLongAuto = colBuf[mdb.Fmt.ColFlagsOffset]&0x04 != 0
		pcol.IsUuidAuto = colBuf[mdb.Fmt.ColFlagsOffset]&0x40 != 0

		pcol.FixedOffset = GetInt16(colBuf, int(mdb.Fmt.TabColOffsetFixed))

		if pcol.ColType != MDBBool {
			pcol.ColSize = GetInt16(colBuf, int(mdb.Fmt.ColSizeOffset))
		} else {
			pcol.ColSize = 0
		}

		table.Columns = append(table.Columns, pcol)
	}

	// 读取列名称
	for i := 0; i < int(table.NumCols); i++ {
		pcol := table.Columns[i]

		var nameSize int
		if mdb.F.JetVersion == MDBVerJet3 {
			nameSize = int(mdb.ReadPgIf8(&curPos))
		} else {
			nameSize = int(mdb.ReadPgIf16(&curPos))
		}

		if nameSize > 0 {
			tmpBuf := make([]byte, nameSize)
			if mdb.ReadPgIfN(tmpBuf, &curPos, nameSize) {
				if mdb.F.JetVersion == MDBVerJet3 {
					pcol.Name = string(tmpBuf)
				} else {
					pcol.Name = UTF16LEToString(tmpBuf)
				}
			}
		}
	}

	// 按列号排序
	sort.Slice(table.Columns, func(i, j int) bool {
		return table.Columns[i].ColNum < table.Columns[j].ColNum
	})

	// 分配列属性
	allProps := table.Entry.Props
	if allProps != nil {
		for i := 0; i < int(table.NumCols); i++ {
			pcol := table.Columns[i]
			for _, props := range allProps {
				if props != nil && props.Name == pcol.Name {
					pcol.Props = props
					break
				}
			}
		}
	}

	table.IndexStart = curPos
	return table.Columns
}

// TableDump 输出表信息
func (mdb *MdbHandle) TableDump(entry *MdbCatalogEntry) {
	table := mdb.ReadTable(entry)
	if table == nil {
		return
	}
	defer mdb.FreeTableDef(table)

	fmt.Printf("definition page     = %d\n", entry.TablePg)
	fmt.Printf("number of datarows  = %d\n", table.NumRows)
	fmt.Printf("number of columns   = %d\n", table.NumCols)
	fmt.Printf("number of indices   = %d\n", table.NumRealIdxs)

	if table.Props != nil {
		mdb.DumpProps(table.Props, 0)
	}

	mdb.ReadColumns(table)
	mdb.ReadIndices(table)

	for i := 0; i < int(table.NumCols); i++ {
		col := table.Columns[i]
		fmt.Printf("column %d Name: %-20s Type: %s(%d)\n",
			i, col.Name,
			mdb.GetColBackendTypeString(col),
			col.ColSize)
		if col.Props != nil {
			mdb.DumpProps(col.Props, 0)
		}
	}

	for i := 0; i < int(table.NumIdxs); i++ {
		idx := table.Indices[i]
		if idx != nil {
			mdb.IndexDump(table, idx)
		}
	}

	if table.UsageMap != nil {
		fmt.Println("pages reserved by this object")
		fmt.Printf("usage map pg %d\n", table.MapBasePg)
		fmt.Printf("free map pg %d\n", table.FreemapBasePg)
	}
}

// IsUserTable 判断是否为用户表
func IsUserTableEntry(entry *MdbCatalogEntry) bool {
	return entry.ObjectType == MDBTable && (entry.Flags&0x80000002) == 0
}

// IsSystemTableEntry 判断是否为系统表
func IsSystemTableEntry(entry *MdbCatalogEntry) bool {
	return entry.ObjectType == MDBTable && (entry.Flags&0x80000002) != 0
}

// TableGetProp 获取表属性
func (mdb *MdbHandle) TableGetProp(table *MdbTableDef, key string) string {
	if table.Props == nil || table.Props.Hash == nil {
		return ""
	}
	val, ok := table.Props.Hash[key]
	if !ok {
		return ""
	}
	if str, ok := val.(string); ok {
		return str
	}
	return ""
}

// ColGetProp 获取列属性
func (mdb *MdbHandle) ColGetProp(col *MdbColumn, key string) string {
	if col.Props == nil || col.Props.Hash == nil {
		return ""
	}
	val, ok := col.Props.Hash[key]
	if !ok {
		return ""
	}
	if str, ok := val.(string); ok {
		return str
	}
	return ""
}

// ColIsShortdate 判断列是否为短日期格式
func (mdb *MdbHandle) ColIsShortdate(col *MdbColumn) bool {
	format := mdb.ColGetProp(col, "Format")
	return format == "Short Date"
}
