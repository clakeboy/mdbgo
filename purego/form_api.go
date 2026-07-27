package purego

import (
	"fmt"
	"strings"
)

// FormStreams 是窗体在 MDB 中的原始设计流数据（来自 MSysObjects 的 Lv/LvProp/LvExtra）。
type FormStreams struct {
	FormName string
	Lv       []byte
	LvProp   []byte
	LvExtra  []byte
}

// FormNames 返回数据库中所有窗体名称。
func (mdb *MDB) FormNames() ([]string, error) {
	if mdb.handle == nil {
		return nil, fmt.Errorf("数据库未打开")
	}

	entries := mdb.handle.ReadCatalog(MDBForm)
	if len(entries) == 0 {
		entries = mdb.handle.ReadCatalog(MDBAny)
	}

	var names []string
	for _, entry := range entries {
		if entry != nil && entry.ObjectType == MDBForm {
			names = append(names, entry.ObjectName)
		}
	}
	return names, nil
}

// ReadFormStreams 从 MSysObjects 表读取指定窗体的 Lv/LvProp/LvExtra 原始流。
func (mdb *MDB) ReadFormStreams(formName string) (*FormStreams, error) {
	if mdb.handle == nil {
		return nil, fmt.Errorf("数据库未打开")
	}
	if strings.TrimSpace(formName) == "" {
		return nil, fmt.Errorf("窗体名为空")
	}
	return mdb.handle.readFormMSysObjectsStreams(formName)
}

func (mdb *MdbHandle) readFormMSysObjectsStreams(formName string) (*FormStreams, error) {
	msysobj := &MdbCatalogEntry{
		Mdb:        mdb,
		ObjectType: MDBTable,
		TablePg:    2,
		ObjectName: "MSysObjects",
	}

	table := mdb.ReadTable(msysobj)
	if table == nil {
		return nil, fmt.Errorf("无法读取 MSysObjects 表")
	}
	defer mdb.FreeTableDef(table)

	if mdb.ReadColumns(table) == nil {
		return nil, fmt.Errorf("无法读取 MSysObjects 的列")
	}

	nameBuf := make([]byte, MDBBindSize)
	lvBuf := make([]byte, MDBMemoOverhead)
	lvPropBuf := make([]byte, MDBMemoOverhead)
	lvExtraBuf := make([]byte, MDBMemoOverhead)

	if mdb.BindColumnByName(table, "Name", nameBuf, nil) == -1 {
		return nil, fmt.Errorf("无法绑定 MSysObjects.Name 列")
	}

	lvIdx := mdb.BindColumnByName(table, "Lv", lvBuf, nil)
	lvPropIdx := mdb.BindColumnByName(table, "LvProp", lvPropBuf, nil)
	lvExtraIdx := mdb.BindColumnByName(table, "LvExtra", lvExtraBuf, nil)

	var lvCol, lvPropCol, lvExtraCol *MdbColumn

	if lvIdx > 0 && lvIdx-1 < len(table.Columns) {
		lvCol = table.Columns[lvIdx-1]
		lvCol.BindPtr = lvBuf
	}
	if lvPropIdx > 0 && lvPropIdx-1 < len(table.Columns) {
		lvPropCol = table.Columns[lvPropIdx-1]
		lvPropCol.BindPtr = lvPropBuf
	}
	if lvExtraIdx > 0 && lvExtraIdx-1 < len(table.Columns) {
		lvExtraCol = table.Columns[lvExtraIdx-1]
		lvExtraCol.BindPtr = lvExtraBuf
	}

	table.RewindTable()
	for table.FetchRow() {
		rowName := UTF16LEToString(nameBuf)
		if !strings.EqualFold(rowName, formName) {
			continue
		}

		result := &FormStreams{FormName: rowName}

		if lvCol != nil && lvCol.CurValueLen > 0 {
			data, err := mdb.readOleColumnData(lvCol)
			if err == nil {
				result.Lv = data
			}
		}
		if lvPropCol != nil && lvPropCol.CurValueLen > 0 {
			data, err := mdb.readOleColumnData(lvPropCol)
			if err == nil {
				result.LvProp = data
			}
		}
		if lvExtraCol != nil && lvExtraCol.CurValueLen > 0 {
			data, err := mdb.readOleColumnData(lvExtraCol)
			if err == nil {
				result.LvExtra = data
			}
		}

		return result, nil
	}

	return nil, fmt.Errorf("在 MSysObjects 中未找到窗体: %s", formName)
}

// ListAccessObjectIDs 列出 MSysAccessObjects 表中所有 ObjectID。
func (mdb *MDB) ListAccessObjectIDs() ([]int, error) {
	if mdb.handle == nil {
		return nil, fmt.Errorf("数据库未打开")
	}

	objects, err := mdb.handle.readMSysAccessObjectsAll()
	if err != nil {
		return nil, err
	}

	ids := make([]int, len(objects))
	for i, obj := range objects {
		ids[i] = obj.ObjectID
	}
	return ids, nil
}
