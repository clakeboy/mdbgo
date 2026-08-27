package purego

import (
	"fmt"
	"strings"
	"time"
)

const (
	OffsetMask    = 0x1fff
	OleBufferSize = MDBBindSize * 64
)

func (table *MdbTableDef) BindColumn(colNum int, bindPtr []byte, lenPtr *int) int {
	if table.Columns == nil {
		return -1
	}
	colNum--
	if colNum < 0 || colNum >= int(table.NumCols) {
		return -1
	}
	col := table.Columns[colNum]
	if col == nil {
		return -1
	}
	if bindPtr != nil {
		col.BindPtr = bindPtr
	}
	if lenPtr != nil {
		col.LenPtr = lenPtr
	}
	return colNum + 1
}

func (mdb *MdbHandle) BindColumnByName(table *MdbTableDef, colName string, bindPtr []byte, lenPtr *int) int {
	if table.Columns == nil {
		return -1
	}
	for i := 0; i < int(table.NumCols); i++ {
		col := table.Columns[i]
		if strings.EqualFold(col.Name, colName) {
			if bindPtr != nil {
				col.BindPtr = bindPtr
			}
			if lenPtr != nil {
				col.LenPtr = lenPtr
			}
			return i + 1
		}
	}
	return -1
}

func (mdb *MdbHandle) FindPgRow(pgRow int) ([]byte, int, error) {
	pg := uint32(pgRow >> 8)
	row := pgRow & 0xff
	if mdb.ReadAltPg(pg) != mdb.Fmt.PgSize {
		return nil, 0, fmt.Errorf("cannot read page %d", pg)
	}
	mdb.SwapPgBuf()
	start, length, err := mdb.FindRow(row)
	if err != nil {
		mdb.SwapPgBuf()
		return nil, 0, err
	}
	mdb.SwapPgBuf()
	start &= OffsetMask
	return mdb.AltPgBuf[start : start+length], length, nil
}

func (mdb *MdbHandle) FindRow(row int) (int, int, error) {
	rco := int(mdb.Fmt.RowCountOffset)
	if row > 1000 {
		return 0, 0, fmt.Errorf("row > 1000")
	}
	start := GetInt16(mdb.PgBuf[:], rco+2+row*2)
	var nextStart int
	if row == 0 {
		nextStart = mdb.Fmt.PgSize
	} else {
		nextStart = GetInt16(mdb.PgBuf[:], rco+row*2) & OffsetMask
	}
	maskedStart := start & OffsetMask
	length := nextStart - maskedStart
	if maskedStart >= mdb.Fmt.PgSize || maskedStart > nextStart || nextStart > mdb.Fmt.PgSize {
		return 0, 0, fmt.Errorf("invalid row position")
	}
	return start, length, nil
}

func IsNullBit(nullMask []byte, colNum int) bool {
	if colNum < 1 {
		return true
	}
	byteNum := (colNum - 1) / 8
	bitNum := (colNum - 1) % 8
	if byteNum >= len(nullMask) {
		return true
	}
	return (1<<bitNum)&nullMask[byteNum] != 0
}

func (mdb *MdbHandle) ReadRow(table *MdbTableDef, row uint) int {
	if table.NumCols == 0 || table.Columns == nil {
		return 0
	}
	rowStart, rowSize, err := mdb.FindRow(int(row))
	if err != nil || rowSize == 0 {
		return 0
	}
	delflag := rowStart&0x4000 != 0
	rowStart &= OffsetMask
	if !table.NoskipDel && delflag {
		return 0
	}
	numCols := int(table.NumCols)
	if cap(table.rowFields) < numCols {
		table.rowFields = make([]MdbField, numCols)
	}
	fields := table.rowFields[:numCols]
	numFields := CrackRow(mdb, table, rowStart, rowSize, fields)
	if numFields < 0 {
		return 0
	}
	if !TestSargs(mdb, table, fields, numFields) {
		return 0
	}
	for i := 0; i < int(table.NumCols); i++ {
		col := table.Columns[fields[i].ColNum]
		AttemptBind(mdb, table, col, fields[i].IsNull, fields[i].Start, fields[i].Size)
	}
	return 1
}

func CrackRow(mdb *MdbHandle, table *MdbTableDef, rowStart int, rowSize int, fields []MdbField) int {
	pgBuf := mdb.PgBuf[:]
	rowEnd := rowStart + rowSize - 1

	var rowCols int
	var colCountSize int
	if mdb.F.JetVersion == MDBVerJet3 {
		rowCols = int(GetByte(pgBuf, rowStart))
		colCountSize = 1
	} else {
		rowCols = GetInt16(pgBuf, rowStart)
		colCountSize = 2
	}

	bitmaskSz := (rowCols + 7) / 8
	if bitmaskSz+colCountSize >= rowSize {
		return 0
	}

	nullMask := pgBuf[rowEnd-bitmaskSz+1 : rowEnd+1]

	// Read variable column offsets
	var varColOffsets []int
	rowVarCols := 0
	if table.NumVarCols > 0 {
		if mdb.F.JetVersion == MDBVerJet3 {
			rowVarCols = int(GetByte(pgBuf, rowEnd-bitmaskSz))
		} else {
			rowVarCols = GetInt16(pgBuf, rowEnd-bitmaskSz-1)
		}

		offsetCount := rowVarCols + 1
		if cap(table.varColOffsets) < offsetCount {
			table.varColOffsets = make([]int, offsetCount)
		}
		varColOffsets = table.varColOffsets[:offsetCount]
		var ok bool
		if IS_JET3(mdb) {
			ok = mdb.crackRow3(rowStart, rowEnd, bitmaskSz, rowVarCols, varColOffsets)
		} else {
			ok = mdb.crackRow4(rowStart, rowEnd, bitmaskSz, rowVarCols, varColOffsets)
		}
		if !ok {
			return 0
		}
	}

	fixedColsFound := 0
	rowFixedCols := rowCols - rowVarCols

	for i := 0; i < int(table.NumCols); i++ {
		col := table.Columns[i]
		fields[i].ColNum = i
		fields[i].IsFixed = col.IsFixed

		byteNum := col.ColNum / 8
		bitNum := col.ColNum % 8
		if byteNum < len(nullMask) {
			fields[i].IsNull = (1<<bitNum)&nullMask[byteNum] == 0
		} else {
			fields[i].IsNull = true
		}

		if fields[i].IsFixed && fixedColsFound < rowFixedCols {
			colStart := col.FixedOffset + colCountSize
			fields[i].Start = rowStart + colStart
			fields[i].Size = col.ColSize
			fixedColsFound++
		} else if !fields[i].IsFixed && col.VarColNum < uint(rowVarCols) {
			colStart := varColOffsets[col.VarColNum]
			fields[i].Start = rowStart + colStart
			fields[i].Size = varColOffsets[col.VarColNum+1] - colStart
		} else {
			fields[i].Start = 0
			fields[i].Size = 0
			fields[i].IsNull = true
		}
	}
	return rowCols
}

func (mdb *MdbHandle) crackRow4(rowStart int, rowEnd int, bitmaskSz int, rowVarCols int, offsets []int) bool {
	if bitmaskSz+3+rowVarCols*2+2 > rowEnd {
		return false
	}
	if len(offsets) < rowVarCols+1 {
		return false
	}
	for i := 0; i < rowVarCols+1; i++ {
		offsets[i] = GetInt16(mdb.PgBuf[:], rowEnd-bitmaskSz-3-i*2)
	}
	return true
}

func (mdb *MdbHandle) crackRow3(rowStart int, rowEnd int, bitmaskSz int, rowVarCols int, offsets []int) bool {
	pgBuf := mdb.PgBuf[:]
	rowLen := rowEnd - rowStart + 1
	numJumps := (rowLen - 1) / 256
	colPtr := rowEnd - bitmaskSz - numJumps - 1

	if (colPtr-rowStart-rowVarCols)/256 < numJumps {
		numJumps--
	}
	if bitmaskSz+numJumps+1 > rowEnd {
		return false
	}
	if colPtr >= mdb.Fmt.PgSize || colPtr < rowVarCols {
		return false
	}
	if len(offsets) < rowVarCols+1 {
		return false
	}

	jumpsUsed := 0
	for i := 0; i < rowVarCols+1; i++ {
		for jumpsUsed < numJumps && i == int(pgBuf[rowEnd-bitmaskSz-jumpsUsed-1]) {
			jumpsUsed++
		}
		offsets[i] = int(pgBuf[colPtr-i]) + jumpsUsed*256
	}
	return true
}

func IS_JET3(mdb *MdbHandle) bool {
	return mdb.F.JetVersion == MDBVerJet3
}

func TestSargs(mdb *MdbHandle, table *MdbTableDef, fields []MdbField, numFields int) bool {
	if table.SargTree == nil {
		return true
	}
	return true
}

func AttemptBind(mdb *MdbHandle, table *MdbTableDef, col *MdbColumn, isNull bool, offset int, length int) {
	col.IsNull = isNull
	if col.LenPtr != nil {
		*col.LenPtr = 0
	}
	// 没有长度指针的旧调用方仍依赖零结尾缓冲区。提供长度指针的扫描路径
	// 只读取实际长度，无需为每个字段反复清空整块 MDBBindSize 缓冲区。
	if bindPtr, ok := col.BindPtr.([]byte); ok && bindPtr != nil && col.LenPtr == nil {
		clear(bindPtr)
	}
	if col.ColType == MDBBool {
		// Access 的 Boolean 没有独立数据字节，而是复用行空值位图：位为 1 表示 True，
		// 位为 0 表示 False。该字段本身并不是 NULL，避免上层扫描把 False 当成空值。
		col.IsNull = false
		if bindPtr, ok := col.BindPtr.([]byte); ok && bindPtr != nil {
			var value string
			if isNull {
				value = mdb.BooleanFalse
			} else {
				value = mdb.BooleanTrue
			}
			n := copy(bindPtr, value)
			terminateBoundValue(bindPtr, n)
			if col.LenPtr != nil {
				*col.LenPtr = n
			}
		}
	} else if isNull {
		col.CurValueStart = 0
		col.CurValueLen = 0
		if bindPtr, ok := col.BindPtr.([]byte); ok && bindPtr != nil {
			terminateBoundValue(bindPtr, 0)
		}
	} else if col.ColType == MDBOle {
		col.CurValueStart = offset
		col.CurValueLen = length
		if bindPtr, ok := col.BindPtr.([]byte); ok && bindPtr != nil {
			n := copy(bindPtr, mdb.PgBuf[offset:offset+MDBMemoOverhead])
			if col.LenPtr != nil {
				*col.LenPtr = n
			}
		}
	} else {
		col.CurValueStart = offset
		col.CurValueLen = length
		if bindPtr, ok := col.BindPtr.([]byte); ok && bindPtr != nil && length > 0 {
			str := ColToString(mdb, mdb.PgBuf[:], offset, col.ColType, length)
			n := copy(bindPtr, str)
			terminateBoundValue(bindPtr, n)
			if col.LenPtr != nil {
				*col.LenPtr = n
			}
		}
	}
}

func terminateBoundValue(buf []byte, length int) {
	if length < len(buf) {
		buf[length] = 0
	}
	if length+1 < len(buf) {
		buf[length+1] = 0
	}
}

func (mdb *MdbHandle) ReadNextDpg(table *MdbTableDef) int {
	maxPg := uint32(mdb.F.Reader.Size / int64(mdb.Fmt.PgSize))
	for {
		nextPg := mdb.MapFindNext(table.UsageMap, table.MapSize, int(table.CurPhysPg))
		if nextPg == 0 {
			break
		}
		if mdb.ReadPg(uint32(nextPg)) == 0 {
			return 0
		}
		table.CurPhysPg = uint32(nextPg)
		if mdb.PgBuf[0] == MDBPageData && GetInt32(mdb.PgBuf[:], 4) == int(table.Entry.TablePg) {
			return int(table.CurPhysPg)
		}
	}
	table.CurPhysPg++
	for table.CurPhysPg < maxPg {
		if mdb.ReadPg(table.CurPhysPg) == 0 {
			return 0
		}
		if mdb.PgBuf[0] == MDBPageData && GetInt32(mdb.PgBuf[:], 4) == int(table.Entry.TablePg) {
			return int(table.CurPhysPg)
		}
		table.CurPhysPg++
	}
	return 0
}

func (table *MdbTableDef) RewindTable() {
	table.CurPgNum = 0
	table.CurPhysPg = 0
	table.CurRow = 0
}

func (table *MdbTableDef) FetchRow() bool {
	mdb := table.Entry.Mdb
	if table.CurPgNum == 0 {
		table.CurPgNum = 1
		table.CurRow = 0
		if !table.IsTempTable && table.Strategy != MDBIndexScan {
			if mdb.ReadNextDpg(table) == 0 {
				return false
			}
		}
	}
	for {
		if table.IsTempTable {
			if len(table.TempTablePages) == 0 {
				return false
			}
			pgBuf := table.TempTablePages[table.CurPgNum-1].([]byte)
			rows := GetInt16(pgBuf, int(mdb.Fmt.RowCountOffset))
			if int(table.CurRow) >= rows {
				table.CurRow = 0
				if int(table.CurPgNum) >= len(table.TempTablePages) {
					return false
				}
				table.CurPgNum++
			}
			copy(mdb.PgBuf[:], pgBuf)
		} else if table.Strategy == MDBIndexScan {
			return false
		} else {
			rows := GetInt16(mdb.PgBuf[:], int(mdb.Fmt.RowCountOffset))
			if int(table.CurRow) >= rows {
				table.CurRow = 0
				if mdb.ReadNextDpg(table) == 0 {
					return false
				}
			}
		}
		rc := mdb.ReadRow(table, table.CurRow)
		table.CurRow++
		if rc != 0 {
			return true
		}
	}
}

func DateToTm(td float64) time.Time {
	if td < 0.0 || td > 1e6 {
		return time.Time{}
	}
	days := int(td)
	frac := td - float64(days)
	base := time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)
	date := base.AddDate(0, 0, days)
	hours := int(frac * 24)
	minutes := int((frac*24 - float64(hours)) * 60)
	seconds := int(((frac*24-float64(hours))*60 - float64(minutes)) * 60)
	return time.Date(date.Year(), date.Month(), date.Day(), hours, minutes, seconds, 0, time.UTC)
}

func ColToString(mdb *MdbHandle, buf []byte, start int, dataType int, size int) string {
	if start < 0 || start+size > len(buf) {
		return ""
	}
	switch dataType {
	case MDBBool:
		if buf[start] != 0 {
			return "yes"
		}
		return "no"
	case MDBByte:
		return fmt.Sprintf("%d", buf[start])
	case MDBInt:
		return fmt.Sprintf("%d", int16(GetInt16(buf, start)))
	case MDBLongInt, MDBComplex:
		return fmt.Sprintf("%d", int32(GetInt32(buf, start)))
	case MDBFloat:
		return fmt.Sprintf("%.8g", GetSingle(buf, start))
	case MDBDouble:
		return fmt.Sprintf("%.16g", GetDouble(buf, start))
	case MDBMoney:
		return mdb.MoneyToString(buf, start)
	case MDBDateTime:
		return mdb.DateToString(buf, start)
	case MDBText:
		return string(buf[start : start+size])
	case MDBMemo:
		return mdb.MemoToString(start, size)
	case MDBRepId:
		return UUIDToStringFmt(buf, start, mdb.RepidFmt)
	default:
		return string(buf[start : start+size])
	}
}

func (mdb *MdbHandle) DateToString(buf []byte, start int) string {
	td := GetDouble(buf, start)
	t := DateToTm(td)
	return fmt.Sprintf("%04d-%02d-%02d %02d:%02d:%02d",
		t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second())
}

func UUIDToStringFmt(buf []byte, pos int, format MdbUuidFormat) string {
	if pos < 0 || pos+16 > len(buf) {
		return ""
	}
	if format == MDBBraces4228 {
		return fmt.Sprintf("{%02X%02X%02X%02X-%02X%02X-%02X%02X-%02X%02X-%02X%02X%02X%02X%02X%02X}",
			buf[pos+3], buf[pos+2], buf[pos+1], buf[pos],
			buf[pos+5], buf[pos+4],
			buf[pos+7], buf[pos+6],
			buf[pos+8], buf[pos+9],
			buf[pos+10], buf[pos+11],
			buf[pos+12], buf[pos+13],
			buf[pos+14], buf[pos+15])
	}
	return fmt.Sprintf("%02X%02X%02X%02X-%02X%02X-%02X%02X-%02X%02X-%02X%02X%02X%02X%02X%02X",
		buf[pos+3], buf[pos+2], buf[pos+1], buf[pos],
		buf[pos+5], buf[pos+4],
		buf[pos+7], buf[pos+6],
		buf[pos+8], buf[pos+9],
		buf[pos+10], buf[pos+11],
		buf[pos+12], buf[pos+13],
		buf[pos+14], buf[pos+15])
}

// MemoToString 从当前记录的 Memo 头读取完整长值，并保持原始字节格式供上层统一解码。
// Access Memo 可能内联在当前记录中，也可能存放在单页或多页长值链中；三种形式统一
// 交给 ReadOleFullData 处理，避免普通表扫描只能读取短 Memo、长 Memo 变成空字符串。
func (mdb *MdbHandle) MemoToString(start int, size int) string {
	if size < MDBMemoOverhead || start < 0 || start+MDBMemoOverhead > len(mdb.PgBuf) {
		return ""
	}

	// ReadOleFullData 必须读取未经格式化的 12 字节 Memo 头，不能复用最终字符串缓冲区。
	header := make([]byte, MDBMemoOverhead)
	copy(header, mdb.PgBuf[start:start+MDBMemoOverhead])
	column := &MdbColumn{
		CurValueStart: start,
		CurValueLen:   size,
		BindPtr:       header,
	}
	data, err := mdb.ReadOleFullData(column)
	if err != nil {
		return ""
	}
	return string(data)
}

func ColDispSize(col *MdbColumn) int {
	switch col.ColType {
	case MDBBool:
		return 1
	case MDBByte:
		return 4
	case MDBInt:
		return 6
	case MDBLongInt, MDBComplex:
		return 11
	case MDBFloat:
		return 10
	case MDBDouble:
		return 10
	case MDBText:
		return col.ColSize
	case MDBDateTime:
		return 20
	case MDBMemo:
		return 64000
	case MDBMoney:
		return 21
	}
	return 0
}

func ColFixedSize(col *MdbColumn) int {
	switch col.ColType {
	case MDBBool:
		return 1
	case MDBByte:
		return -1
	case MDBInt:
		return 2
	case MDBLongInt, MDBComplex:
		return 4
	case MDBFloat:
		return 4
	case MDBDouble:
		return 8
	case MDBText:
		return -1
	case MDBDateTime:
		return 4
	case MDBBinary:
		return -1
	case MDBMemo:
		return -1
	case MDBMoney:
		return 8
	}
	return 0
}
