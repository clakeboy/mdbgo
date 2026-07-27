package purego

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"
)

// ReadIndices 读取索引定义到 MdbTableDef
func (mdb *MdbHandle) ReadIndices(table *MdbTableDef) []*MdbIndex {
	if mdb == nil || table == nil {
		return nil
	}

	table.Indices = make([]*MdbIndex, 0)
	curPos := table.IndexStart

	for i := 0; i < int(table.NumRealIdxs); i++ {
		if curPos+12 > len(mdb.PgBuf) {
			break
		}

		idx := &MdbIndex{
			Table: table,
		}

		idx.IndexNum = int(mdb.PgBuf[curPos])
		idx.IndexType = mdb.PgBuf[curPos+1]
		idx.FirstPg = uint32(GetInt32(mdb.PgBuf[:], curPos+4))
		if nk := uint(GetInt16(mdb.PgBuf[:], curPos+8)); nk > MDBMaxIdxCols {
			idx.NumKeys = MDBMaxIdxCols
		} else {
			idx.NumKeys = nk
		}
		idx.Flags = mdb.PgBuf[curPos+10]

		nameStart := curPos + 12
		if nameStart < len(mdb.PgBuf) {
			nameLen := int(mdb.PgBuf[nameStart])
			if nameStart+1+nameLen <= len(mdb.PgBuf) {
				idx.Name = string(mdb.PgBuf[nameStart+1 : nameStart+1+nameLen])
			}
		}

		for k := 0; k < int(idx.NumKeys) && k < MDBMaxIdxCols; k++ {
			keyPos := nameStart + 1 + len(idx.Name) + k*2
			if keyPos+2 <= len(mdb.PgBuf) {
				idx.KeyColNum[k] = int16(GetInt16(mdb.PgBuf[:], keyPos))
			}
		}

		table.Indices = append(table.Indices, idx)
		curPos += 12 + 1 + len(idx.Name) + int(idx.NumKeys)*2
	}

	return table.Indices
}

// FreeIndices 释放索引
func (mdb *MdbHandle) FreeIndices(indices []*MdbIndex) {
}

// IndexDump 输出索引信息
func (mdb *MdbHandle) IndexDump(table *MdbTableDef, idx *MdbIndex) {
	if idx == nil {
		return
	}

	fmt.Printf("Index %d: %s\n", idx.IndexNum, idx.Name)
	fmt.Printf("  Type: %d, First Page: %d\n", idx.IndexType, idx.FirstPg)
	fmt.Printf("  Keys: %d, Flags: %d\n", idx.NumKeys, idx.Flags)

	for k := 0; k < int(idx.NumKeys); k++ {
		colNum := idx.KeyColNum[k]
		order := idx.KeyColOrder[k]
		orderStr := "ASC"
		if order == MDBDesc {
			orderStr = "DESC"
		}
		fmt.Printf("  Key %d: Column %d %s\n", k, colNum, orderStr)
	}
}

// IndexScanInit 初始化索引扫描
func (mdb *MdbHandle) IndexScanInit(table *MdbTableDef) {
	if table == nil {
		return
	}
	table.Chain = &MdbIndexChain{}
}

// IndexScanFree 释放索引扫描
func (mdb *MdbHandle) IndexScanFree(table *MdbTableDef) {
	if table == nil {
		return
	}
	table.Chain = nil
}

// IndexFindNext 查找索引中的下一个条目
func (mdb *MdbHandle) IndexFindNext(idx *MdbIndex, chain *MdbIndexChain, pg *uint32, row *uint16) bool {
	if idx == nil || chain == nil {
		return false
	}
	return false
}

// IndexFindRow 查找索引中的指定行
func (mdb *MdbHandle) IndexFindRow(idx *MdbIndex, chain *MdbIndexChain, pg uint32, row uint16) bool {
	return false
}

// ===== 索引 B-tree 遍历实现 =====

type indexPageHeader struct {
	numEntries int
	nextPg     uint32
	prevPg     uint32
}

func (mdb *MdbHandle) readIndexPageHeader() indexPageHeader {
	pgBuf := mdb.PgBuf[:]
	return indexPageHeader{
		numEntries: GetInt16(pgBuf, 2),
		nextPg:     uint32(GetInt32(pgBuf, 4)),
		prevPg:     uint32(GetInt32(pgBuf, 8)),
	}
}

func (mdb *MdbHandle) indexEntryOffsets() ([]int, error) {
	hdr := mdb.readIndexPageHeader()
	if hdr.numEntries == 0 {
		return nil, nil
	}
	pgBuf := mdb.PgBuf[:]

	entries := make([]int, 0, hdr.numEntries)
	pos := 12
	for i := 0; i < hdr.numEntries && pos < mdb.Fmt.PgSize; i++ {
		if pos >= len(pgBuf) {
			break
		}
		flag := pgBuf[pos]
		if flag == 0 {
			break
		}
		entries = append(entries, pos)
		entryLen, err := mdb.indexEntryLength(pos)
		if err != nil {
			return nil, err
		}
		if entryLen <= 0 {
			break
		}
		pos += entryLen
	}
	return entries, nil
}

func keyColumnFixedSize(colType int) int {
	switch colType {
	case MDBBool:
		return 1
	case MDBByte:
		return 1
	case MDBInt:
		return 2
	case MDBLongInt, MDBComplex:
		return 4
	case MDBMoney:
		return 8
	case MDBFloat:
		return 4
	case MDBDouble, MDBDateTime:
		return 8
	case MDBText:
		return -1
	case MDBRepId:
		return 16
	default:
		return 4
	}
}

func (mdb *MdbHandle) indexEntryLength(pos int) (int, error) {
	pgBuf := mdb.PgBuf[:]
	if pos >= mdb.Fmt.PgSize || pos >= len(pgBuf) {
		return 0, fmt.Errorf("entry position out of range: %d", pos)
	}
	flag := pgBuf[pos]
	if flag == 0 {
		return 0, nil
	}

	// 叶条目通常标志为 0x04/0x14/0x05/0x15
	// 内部条目标志为 0x01/0x00/0x10
	isLeaf := (flag & 0x04) != 0

	// 对于变长文本，尝试读取长度前缀
	var keyLen int
	if pos+1 < len(pgBuf) {
		possibleLen := int(pgBuf[pos+1])
		// 文本条目第一个字节之后的第一个字节可能是长度
		// 尝试定长解析：先假设 4 字节 LongInt 最常见
		if isLeaf && possibleLen > 0 && possibleLen <= 128 && pos+2+possibleLen+4 <= mdb.Fmt.PgSize {
			keyLen = 1 + possibleLen
		} else if !isLeaf && possibleLen > 0 && possibleLen < 128 && pos+2+possibleLen+4 <= mdb.Fmt.PgSize {
			keyLen = 1 + possibleLen
		} else {
			keyLen = 4
		}
	} else {
		keyLen = 4
	}

	return 1 + keyLen + 4, nil
}

func (mdb *MdbHandle) entryKeyBytes(entryOff int) ([]byte, error) {
	pgBuf := mdb.PgBuf[:]
	if entryOff+1 >= len(pgBuf) {
		return nil, fmt.Errorf("entry offset out of range: %d", entryOff)
	}

	if entryOff+2 < len(pgBuf) {
		possibleLen := int(pgBuf[entryOff+1])
		if possibleLen > 0 && possibleLen <= 128 && entryOff+2+possibleLen <= len(pgBuf) {
			buf := make([]byte, possibleLen)
			copy(buf, pgBuf[entryOff+2:entryOff+2+possibleLen])
			return buf, nil
		}
	}

	keyLen := 4
	buf := make([]byte, keyLen)
	if entryOff+1+keyLen <= len(pgBuf) {
		copy(buf, pgBuf[entryOff+1:entryOff+1+keyLen])
	}
	return buf, nil
}

func (mdb *MdbHandle) entryDataRef(entryOff int) (uint32, error) {
	pgBuf := mdb.PgBuf[:]
	entryLen, err := mdb.indexEntryLength(entryOff)
	if err != nil || entryLen <= 0 {
		return 0, fmt.Errorf("cannot determine entry length at %d", entryOff)
	}
	refOff := entryOff + entryLen - 4
	if refOff < 0 || refOff+4 > len(pgBuf) {
		return 0, fmt.Errorf("data reference out of range at %d", refOff)
	}
	return binary.LittleEndian.Uint32(pgBuf[refOff : refOff+4]), nil
}

func (mdb *MdbHandle) entryIsLeaf(entryOff int) bool {
	pgBuf := mdb.PgBuf[:]
	if entryOff >= len(pgBuf) {
		return false
	}
	return (pgBuf[entryOff] & 0x04) != 0
}

// IndexEntry 表示索引中的一个叶条目
type IndexEntry struct {
	Key string
	Ref uint32
	PG  uint32
	Row uint16
}

func (e *IndexEntry) PageRow() (uint32, uint16) {
	return e.Ref >> 8, uint16(e.Ref & 0xFF)
}

func (mdb *MdbHandle) collectIndexEntries(idx *MdbIndex) ([]IndexEntry, error) {
	return mdb.collectLeafEntries(idx.FirstPg)
}

func (mdb *MdbHandle) collectLeafEntries(pg uint32) ([]IndexEntry, error) {
	if pg == 0 {
		return nil, nil
	}

	if mdb.ReadPg(pg) == 0 {
		return nil, fmt.Errorf("cannot read index page %d", pg)
	}

	hdr := mdb.readIndexPageHeader()
	entries, err := mdb.indexEntryOffsets()
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, nil
	}

	isLeaf := mdb.entryIsLeaf(entries[0])

	if !isLeaf {
		var result []IndexEntry
		for _, off := range entries {
			childPg, err := mdb.entryDataRef(off)
			if err != nil {
				continue
			}
			childEntries, err := mdb.collectLeafEntries(childPg)
			if err != nil {
				return nil, err
			}
			result = append(result, childEntries...)
		}
		return result, nil
	}

	var result []IndexEntry
	for _, off := range entries {
		keyBytes, err := mdb.entryKeyBytes(off)
		if err != nil {
			continue
		}
		ref, err := mdb.entryDataRef(off)
		if err != nil {
			continue
		}
		result = append(result, IndexEntry{
			Key: string(keyBytes),
			Ref: ref,
			PG:  ref >> 8,
			Row: uint16(ref & 0xFF),
		})
	}

	if hdr.nextPg != 0 {
		nextEntries, err := mdb.collectLeafEntries(hdr.nextPg)
		if err != nil {
			return nil, err
		}
		result = append(result, nextEntries...)
	}

	return result, nil
}

// TableIndices 获取表的索引列表
func (mdb *MDB) TableIndices(tableName string) ([]*MdbIndex, error) {
	if mdb.handle == nil {
		return nil, fmt.Errorf("database is not open")
	}

	entry := mdb.handle.GetCatalogEntryByName(tableName)
	if entry == nil || entry.ObjectType != MDBTable {
		mdb.handle.ReadCatalog(MDBTable)
		entry = mdb.handle.GetCatalogEntryByName(tableName)
	}
	if entry == nil || entry.ObjectType != MDBTable {
		return nil, fmt.Errorf("table %s does not exist", tableName)
	}

	table := mdb.handle.ReadTable(entry)
	if table == nil {
		return nil, fmt.Errorf("cannot read table %s", tableName)
	}
	defer mdb.handle.FreeTableDef(table)

	mdb.handle.ReadColumns(table)
	mdb.handle.ReadIndices(table)

	return table.Indices, nil
}

// FindIndexByColumn 查找表中包含指定列的索引
func (mdb *MDB) FindIndexByColumn(tableName string, columnName string) (*MdbIndex, error) {
	indices, err := mdb.TableIndices(tableName)
	if err != nil {
		return nil, err
	}

	entry := mdb.handle.GetCatalogEntryByName(tableName)
	if entry == nil || entry.ObjectType != MDBTable {
		mdb.handle.ReadCatalog(MDBTable)
		entry = mdb.handle.GetCatalogEntryByName(tableName)
	}
	if entry == nil {
		return nil, fmt.Errorf("table %s not found", tableName)
	}

	table := mdb.handle.ReadTable(entry)
	if table == nil {
		return nil, fmt.Errorf("cannot read table %s", tableName)
	}
	defer mdb.handle.FreeTableDef(table)

	mdb.handle.ReadColumns(table)

	var targetCol *MdbColumn
	for _, col := range table.Columns {
		if strings.EqualFold(col.Name, columnName) {
			targetCol = col
			break
		}
	}
	if targetCol == nil {
		return nil, nil
	}

	for _, idx := range indices {
		maxKeys := int(idx.NumKeys)
		if maxKeys > len(idx.KeyColNum) {
			maxKeys = len(idx.KeyColNum)
		}
		for k := 0; k < maxKeys; k++ {
			if int(idx.KeyColNum[k]) == targetCol.ColNum {
				if mdb.isValidIndex(idx) {
					return idx, nil
				}
				return nil, nil
			}
		}
	}
	return nil, nil
}

// isValidIndex 检查索引定义是否有效：FirstPg 必须指向一个索引页 (MDBPageIndex/MDBPageLeaf)
func (mdb *MDB) isValidIndex(idx *MdbIndex) bool {
	if idx == nil || idx.FirstPg == 0 {
		return false
	}

	oldPg := mdb.handle.CurPg
	n := mdb.handle.ReadPg(idx.FirstPg)
	if n == 0 {
		return false
	}

	pageType := mdb.handle.PgBuf[0]
	// 恢复原始页面
	if oldPg != 0 && oldPg != idx.FirstPg {
		mdb.handle.ReadPg(oldPg)
	}

	return pageType == MDBPageIndex || pageType == MDBPageLeaf
}

// LookupIndexByString 在索引中查找指定字符串值，返回匹配的 IndexEntry 列表
func (mdb *MDB) LookupIndexByString(tableName string, columnName string, lookupValue string) ([]IndexEntry, error) {
	idx, err := mdb.FindIndexByColumn(tableName, columnName)
	if err != nil {
		return nil, err
	}
	if idx == nil {
		return nil, nil
	}

	entries, err := mdb.handle.collectIndexEntries(idx)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, nil
	}

	var result []IndexEntry
	for _, e := range entries {
		if e.Key == lookupValue {
			result = append(result, e)
		}
	}
	return result, nil
}

// ReadRowsByIndexEntries 根据索引条目列表读取具体的行数据
func (mdb *MDB) ReadRowsByIndexEntries(tableName string, entries []IndexEntry, requestedColumns []string) ([][]string, [][]bool, error) {
	if mdb.handle == nil {
		return nil, nil, fmt.Errorf("database is not open")
	}

	entry := mdb.handle.GetCatalogEntryByName(tableName)
	if entry == nil || entry.ObjectType != MDBTable {
		mdb.handle.ReadCatalog(MDBTable)
		entry = mdb.handle.GetCatalogEntryByName(tableName)
	}
	if entry == nil {
		return nil, nil, fmt.Errorf("table %s not found", tableName)
	}

	table := mdb.handle.ReadTable(entry)
	if table == nil {
		return nil, nil, fmt.Errorf("cannot read table %s", tableName)
	}
	defer mdb.handle.FreeTableDef(table)

	if mdb.handle.ReadColumns(table) == nil {
		return nil, nil, fmt.Errorf("cannot read columns for table %s", tableName)
	}

	nCols := int(table.NumCols)

	boundValues := make([][]byte, nCols)
	for i := 0; i < nCols; i++ {
		boundValues[i] = make([]byte, MDBBindSize)
		ret := table.BindColumn(i+1, boundValues[i], nil)
		if ret == -1 {
			boundValues[i] = nil
		}
	}

	colIndex := make([]int, 0)
	if len(requestedColumns) > 0 {
		req := make(map[string]bool)
		for _, rc := range requestedColumns {
			req[rc] = true
		}
		for i, col := range table.Columns {
			if req[col.Name] {
				colIndex = append(colIndex, i)
			}
		}
	} else {
		for i := 0; i < nCols; i++ {
			colIndex = append(colIndex, i)
		}
	}

	var result [][]string
	var nulls [][]bool

	pageGroups := make(map[uint32][]uint16)
	for _, e := range entries {
		pg, row := e.PageRow()
		pageGroups[pg] = append(pageGroups[pg], row)
	}

	for pg, rows := range pageGroups {
		if mdb.handle.ReadPg(pg) == 0 {
			continue
		}
		for _, row := range rows {
			for i := 0; i < nCols; i++ {
				if boundValues[i] != nil {
					clear(boundValues[i])
				}
				table.Columns[i].IsNull = false
			}

			if mdb.handle.ReadRow(table, uint(row)) == 0 {
				continue
			}

			dataRow := make([]string, len(colIndex))
			nullRow := make([]bool, len(colIndex))
			for j, ci := range colIndex {
				col := table.Columns[ci]
				if col.IsNull {
					nullRow[j] = true
				} else if boundValues[ci] != nil {
					if col.ColType == MDBText || col.ColType == MDBMemo {
						dataRow[j] = UTF16LEToString(boundValues[ci])
					} else {
						dataRow[j] = strings.TrimRight(string(boundValues[ci]), "\x00")
					}
				}
			}
			result = append(result, dataRow)
			nulls = append(nulls, nullRow)
		}
	}

	return result, nulls, nil
}

// ReadTableDataByIndex 使用索引过滤读取表数据
func (mdb *MDB) ReadTableDataByIndex(tableName string, columnName string, lookupValue string, requestedColumns []string) ([][]string, [][]bool, error) {
	entries, err := mdb.LookupIndexByString(tableName, columnName, lookupValue)
	if err != nil {
		return nil, nil, err
	}
	if len(entries) == 0 {
		return nil, nil, nil
	}
	return mdb.ReadRowsByIndexEntries(tableName, entries, requestedColumns)
}

// ValueToIndexKey 将值的字符串表示转换为索引键可比较的字符串
// 用于 mdbgo.Value 字符串在 SQL 引擎中的索引查找
func ValueToIndexKey(colType int, raw string) string {
	switch colType {
	case MDBInt:
		if val, ok := tryParseInt(raw); ok {
			return int16ToIndexKey(int16(val))
		}
	case MDBLongInt, MDBComplex:
		if val, ok := tryParseInt(raw); ok {
			return int32ToIndexKey(int32(val))
		}
	case MDBDouble, MDBDateTime:
		if val, ok := tryParseFloat(raw); ok {
			return float64ToIndexKey(val)
		}
	case MDBFloat:
		if val, ok := tryParseFloat(raw); ok {
			return float32ToIndexKey(float32(val))
		}
	case MDBMoney:
		if val, ok := tryParseInt(raw); ok {
			return int64ToIndexKey(val)
		}
	case MDBBool:
		if raw == "1" || raw == "true" || raw == "yes" {
			return "\x01"
		}
		return "\x00"
	case MDBText:
		return raw
	case MDBByte:
		if val, ok := tryParseInt(raw); ok {
			return string(rune(byte(val)))
		}
	}
	return raw
}

func tryParseInt(s string) (int64, bool) {
	var v int64
	if _, err := fmt.Sscanf(s, "%d", &v); err == nil {
		return v, true
	}
	return 0, false
}

func tryParseFloat(s string) (float64, bool) {
	var v float64
	if _, err := fmt.Sscanf(s, "%f", &v); err == nil {
		return v, true
	}
	return 0, false
}

func int16ToIndexKey(v int16) string {
	val := uint16(v) ^ 0x8000
	buf := make([]byte, 2)
	binary.BigEndian.PutUint16(buf, val)
	return string(buf)
}

func int32ToIndexKey(v int32) string {
	val := uint32(v) ^ 0x80000000
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, val)
	return string(buf)
}

func int64ToIndexKey(v int64) string {
	val := uint64(v) ^ 0x8000000000000000
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, val)
	return string(buf)
}

func float32ToIndexKey(v float32) string {
	bits := math.Float32bits(v)
	if v < 0 {
		bits ^= 0xFFFFFFFF
	} else {
		bits ^= 0x80000000
	}
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, bits)
	return string(buf)
}

func float64ToIndexKey(v float64) string {
	bits := math.Float64bits(v)
	if v < 0 {
		bits ^= 0xFFFFFFFFFFFFFFFF
	} else {
		bits ^= 0x8000000000000000
	}
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, bits)
	return string(buf)
}
