package purego

import (
	"fmt"
)

// ReadOleFullData 读取 OLE 长字段的完整数据。
//
// 从绑定列的 CurValueStart/CurValueLen 和 bindPtr（前 MDBMemoOverhead 字节为 OLE 头）
// 获取长值的完整内容。支持三种存储方式：
//   - 内联（inline）：数据直接嵌在行记录的 OLE 头之后
//   - 单页链（single-page）：数据存储在另一个页面的单行中
//   - 多页链（multi-page）：数据分布在多个长值页中，通过链表连接
//
// 调用方必须确保 col 已通过 BindColumn 绑定，且当前行已通过 FetchRow/ReadRow 加载。
func (mdb *MdbHandle) ReadOleFullData(col *MdbColumn) ([]byte, error) {
	if col == nil {
		return nil, fmt.Errorf("ReadOleFullData: column is nil")
	}

	bindPtr, ok := col.BindPtr.([]byte)
	if !ok || bindPtr == nil || len(bindPtr) < MDBMemoOverhead {
		return nil, fmt.Errorf("ReadOleFullData: column not bound or buffer too short")
	}

	oleLen := GetInt32(bindPtr, 0)

	if oleLen&0x80000000 != 0 {
		return mdb.readOleInline(col, bindPtr)
	}
	if oleLen&0x40000000 != 0 {
		return mdb.readOleSinglePage(col, bindPtr)
	}
	return mdb.readOleMultiPage(col, bindPtr)
}

func (mdb *MdbHandle) readOleInline(col *MdbColumn, bindPtr []byte) ([]byte, error) {
	if col.CurValueLen <= MDBMemoOverhead {
		return nil, nil
	}
	dataLen := col.CurValueLen - MDBMemoOverhead
	data := make([]byte, dataLen)
	copy(data, mdb.PgBuf[col.CurValueStart+MDBMemoOverhead:col.CurValueStart+col.CurValueLen])
	return data, nil
}

func (mdb *MdbHandle) readOleSinglePage(col *MdbColumn, bindPtr []byte) ([]byte, error) {
	pgRow := GetInt32(bindPtr, 4)
	if pgRow == 0 {
		return nil, nil
	}

	buf, length, err := mdb.findPgRowCopy(pgRow)
	if err != nil {
		return nil, err
	}
	if length == 0 {
		return nil, nil
	}

	data := make([]byte, length)
	copy(data, buf[:length])
	return data, nil
}

func (mdb *MdbHandle) readOleMultiPage(col *MdbColumn, bindPtr []byte) ([]byte, error) {
	pgRow := GetInt32(bindPtr, 4)
	if pgRow == 0 {
		return nil, nil
	}

	colSize := OleBufferSize
	if colSize < 4096 {
		colSize = 4096
	}
	result := make([]byte, 0, colSize)
	nextPgRow := uint32(pgRow)

	for nextPgRow != 0 {
		buf, length, err := mdb.findPgRowCopy(int(nextPgRow))
		if err != nil {
			return nil, fmt.Errorf("ReadOleFullData: read page at pg_row %d: %w", nextPgRow, err)
		}
		if length < 4 {
			break
		}

		nextPgRow = uint32(GetInt32(buf, 0))
		chunkLen := length - 4
		if chunkLen > 0 {
			result = append(result, buf[4:length]...)
		}
	}

	return result, nil
}

// findPgRowCopy 调用 FindPgRow 并将行数据复制到独立缓冲区。
// FindPgRow 返回的切片引用 mdb.AltPgBuf，后续页面读取会覆盖它。
func (mdb *MdbHandle) findPgRowCopy(pgRow int) (data []byte, length int, err error) {
	src, length, err := mdb.FindPgRow(pgRow)
	if err != nil {
		return nil, 0, err
	}
	if length == 0 || len(src) == 0 {
		return nil, 0, nil
	}
	cp := make([]byte, length)
	copy(cp, src[:length])
	return cp, length, nil
}
