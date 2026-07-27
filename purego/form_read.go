package purego

import (
	"fmt"
	"strconv"
	"strings"
)

// AccessObjectData 是 MSysAccessObjects.Data 的原始内容。
type AccessObjectData struct {
	ObjectID int
	Data     []byte
}

// AccessStorageRow 是 MSysAccessStorage 中的一条记录。
type AccessStorageRow struct {
	ID       int
	ParentID int
	Type     int
	Name     string
	Data     []byte
}

// AccessStorageKind 表示 Access 应用对象的存储方式。
const (
	AccessStorageNone    = 0
	AccessStorageObjects = 1 // MSysAccessObjects（Access 2000）
	AccessStorageTree    = 2 // MSysAccessStorage（Access 2003+）
)

// HasAccessObjectStorage 检测数据库使用哪种 Access 对象存储表。
func (mdb *MDB) HasAccessObjectStorage() int {
	if mdb.handle == nil {
		return AccessStorageNone
	}
	return mdb.handle.detectAccessStorageKind()
}

func (mdb *MdbHandle) detectAccessStorageKind() int {
	catalog := mdb.ReadCatalog(MDBAny)
	if catalog == nil {
		return AccessStorageNone
	}
	for _, entry := range catalog {
		if entry == nil {
			continue
		}
		if strings.EqualFold(entry.ObjectName, "MSysAccessObjects") {
			return AccessStorageObjects
		}
		if strings.EqualFold(entry.ObjectName, "MSysAccessStorage") {
			return AccessStorageTree
		}
	}
	return AccessStorageNone
}

// ReadMSysAccessObjectsAll 读取 MSysAccessObjects 表中的所有条目。
// Data 列是 OLE 长字段，会读取完整数据。
func (mdb *MDB) ReadMSysAccessObjectsAll() ([]*AccessObjectData, error) {
	if mdb.handle == nil {
		return nil, fmt.Errorf("数据库未打开")
	}
	return mdb.handle.readMSysAccessObjectsAll()
}

func (mdb *MdbHandle) readMSysAccessObjectsAll() ([]*AccessObjectData, error) {
	table := mdb.ReadTableByName("MSysAccessObjects", MDBAny)
	if table == nil {
		return nil, fmt.Errorf("无法读取 MSysAccessObjects 表")
	}
	defer mdb.FreeTableDef(table)

	if mdb.ReadColumns(table) == nil {
		return nil, fmt.Errorf("无法读取 MSysAccessObjects 的列")
	}

	// 绑定列：列 2 = ID (LongInt)，列 1 = Data
	idBuf := make([]byte, MDBBindSize)
	if mdb.BindColumnByName(table, "Id", idBuf, nil) == -1 {
		// 尝试按列号绑定
		if table.BindColumn(2, idBuf, nil) == -1 {
			return nil, fmt.Errorf("无法绑定 MSysAccessObjects.Id 列")
		}
	}

	// Data 列绑定但不使用 bindPtr（列类型未知，AttemptBind 会写入字符串表示）
	var dataLenDummy int
	dataIdx := mdb.BindColumnByName(table, "Data", nil, &dataLenDummy)
	if dataIdx == -1 {
		if table.BindColumn(1, nil, &dataLenDummy) == -1 {
			return nil, fmt.Errorf("无法绑定 MSysAccessObjects.Data 列")
		}
		dataIdx = 0 // fallback：用列号
	}

	var dataCol *MdbColumn
	if dataIdx > 0 && dataIdx-1 < len(table.Columns) {
		dataCol = table.Columns[dataIdx-1]
	} else {
		// 找列名为 Data 的列
		for _, col := range table.Columns {
			if strings.EqualFold(col.Name, "Data") {
				dataCol = col
				break
			}
		}
	}
	if dataCol == nil {
		return nil, fmt.Errorf("未找到 MSysAccessObjects.Data 列")
	}

	// 为 Data 列绑定缓冲区
	dataBuf := make([]byte, MDBMemoOverhead)
	dataCol.BindPtr = dataBuf

	table.RewindTable()

	var results []*AccessObjectData
	for table.FetchRow() {
		idStr := strings.TrimRight(string(idBuf), "\x00")
		objID, _ := strconv.Atoi(idStr)

		item := &AccessObjectData{
			ObjectID: objID,
		}

		// 读取 Data 列
		data, err := mdb.readOleColumnData(dataCol)
		if err != nil {
			return nil, fmt.Errorf("读取 MSysAccessObjects.Data id=%d: %w", objID, err)
		}
		item.Data = data

		if len(item.Data) == 0 {
			continue
		}

		results = append(results, item)
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("MSysAccessObjects 为空")
	}

	return results, nil
}

// ReadMSysAccessStorageAll 读取 MSysAccessStorage 表中的所有条目。
func (mdb *MDB) ReadMSysAccessStorageAll() ([]*AccessStorageRow, error) {
	if mdb.handle == nil {
		return nil, fmt.Errorf("数据库未打开")
	}
	return mdb.handle.readMSysAccessStorageAll()
}

func (mdb *MdbHandle) readMSysAccessStorageAll() ([]*AccessStorageRow, error) {
	table := mdb.ReadTableByName("MSysAccessStorage", MDBAny)
	if table == nil {
		return nil, fmt.Errorf("无法读取 MSysAccessStorage 表")
	}
	defer mdb.FreeTableDef(table)

	if mdb.ReadColumns(table) == nil {
		return nil, fmt.Errorf("无法读取 MSysAccessStorage 的列")
	}

	idBuf := make([]byte, MDBBindSize)
	parentIDBuf := make([]byte, MDBBindSize)
	typeBuf := make([]byte, MDBBindSize)
	nameBuf := make([]byte, MDBBindSize)
	lvBuf := make([]byte, MDBMemoOverhead)

	if mdb.BindColumnByName(table, "Id", idBuf, nil) == -1 {
		return nil, fmt.Errorf("无法绑定 MSysAccessStorage.Id 列")
	}
	if mdb.BindColumnByName(table, "ParentId", parentIDBuf, nil) == -1 {
		return nil, fmt.Errorf("无法绑定 MSysAccessStorage.ParentId 列")
	}
	if mdb.BindColumnByName(table, "Type", typeBuf, nil) == -1 {
		return nil, fmt.Errorf("无法绑定 MSysAccessStorage.Type 列")
	}
	if mdb.BindColumnByName(table, "Name", nameBuf, nil) == -1 {
		return nil, fmt.Errorf("无法绑定 MSysAccessStorage.Name 列")
	}

	lvIdx := mdb.BindColumnByName(table, "Lv", lvBuf, nil)
	if lvIdx == -1 {
		return nil, fmt.Errorf("无法绑定 MSysAccessStorage.Lv 列")
	}

	var lvCol *MdbColumn
	if lvIdx > 0 && lvIdx-1 < len(table.Columns) {
		lvCol = table.Columns[lvIdx-1]
	} else {
		for _, col := range table.Columns {
			if strings.EqualFold(col.Name, "Lv") {
				lvCol = col
				break
			}
		}
	}
	if lvCol == nil {
		return nil, fmt.Errorf("未找到 MSysAccessStorage.Lv 列")
	}
	lvCol.BindPtr = lvBuf

	table.RewindTable()

	var results []*AccessStorageRow
	for table.FetchRow() {
		idStr := strings.TrimRight(string(idBuf), "\x00")
		parentIDStr := strings.TrimRight(string(parentIDBuf), "\x00")
		typeStr := strings.TrimRight(string(typeBuf), "\x00")
		nameStr := UTF16LEToString(nameBuf)
		if nameStr == "" {
			nameStr = strings.TrimRight(string(nameBuf), "\x00")
		}

		item := &AccessStorageRow{
			ID:       parseInt(idStr),
			ParentID: parseInt(parentIDStr),
			Type:     parseInt(typeStr),
			Name:     nameStr,
		}

		if lvCol.ColType == MDBOle || lvCol.CurValueLen > 0 {
			data, err := mdb.readOleColumnData(lvCol)
			if err == nil && len(data) > 0 {
				item.Data = data
			}
		}

		results = append(results, item)
	}

	return results, nil
}

// readOleColumnData 从列中读取完整的 OLE 数据。
// 兼容 MDBOle 类型和未知类型（如类型 17）的列。
func (mdb *MdbHandle) readOleColumnData(col *MdbColumn) ([]byte, error) {
	if col == nil {
		return nil, fmt.Errorf("column is nil")
	}

	if col.ColType == MDBOle {
		// OLE 类型：bindPtr 包含 OLE 头，直接使用 ReadOleFullData
		if col.BindPtr != nil {
			// 确保 bindPtr 的前 12 字节是 OLE 头
			if buf, ok := col.BindPtr.([]byte); ok && len(buf) >= MDBMemoOverhead {
				// 可能已经被 AttemptBind 的 default 路径修改，从 pg_buf 重新复制
				copy(buf, mdb.PgBuf[col.CurValueStart:col.CurValueStart+MDBMemoOverhead])
			}
		}
		return mdb.ReadOleFullData(col)
	}

	// 未知类型：bindPtr 可能不是 OLE 头，直接从 pg_buf 读取
	if col.CurValueLen > MDBMemoOverhead &&
		col.CurValueStart >= 0 &&
		col.CurValueStart+col.CurValueLen <= mdb.Fmt.PgSize {
		// 数据在当前页面上内联可用
		data := make([]byte, col.CurValueLen)
		copy(data, mdb.PgBuf[col.CurValueStart:col.CurValueStart+col.CurValueLen])
		return data, nil
	}

	// 链式存储：构造 OLE 头并调用 ReadOleFullData
	if col.CurValueLen >= MDBMemoOverhead &&
		col.CurValueStart >= 0 &&
		col.CurValueStart+MDBMemoOverhead <= mdb.Fmt.PgSize {
		// 修复 bindPtr 为正确的 OLE 头
		bindBuf, ok := col.BindPtr.([]byte)
		if !ok || len(bindBuf) < MDBMemoOverhead {
			bindBuf = make([]byte, MDBMemoOverhead)
			col.BindPtr = bindBuf
		}
		copy(bindBuf, mdb.PgBuf[col.CurValueStart:col.CurValueStart+MDBMemoOverhead])
		return mdb.ReadOleFullData(col)
	}

	return nil, fmt.Errorf("无可用数据: cur_start=%d cur_len=%d col_type=%d",
		col.CurValueStart, col.CurValueLen, col.ColType)
}

func parseInt(s string) int {
	v, _ := strconv.Atoi(s)
	return v
}
