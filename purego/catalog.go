package purego

import (
	"fmt"
	"strconv"
	"strings"
)

// FreeCatalog 释放目录
func (mdb *MdbHandle) FreeCatalog() {
	if mdb == nil || mdb.Catalog == nil {
		return
	}

	for _, entry := range mdb.Catalog {
		if entry != nil {
			if entry.Props != nil {
				for _, props := range entry.Props {
					if props != nil {
						// 释放属性
					}
				}
				entry.Props = nil
			}
		}
	}
	mdb.Catalog = nil
	mdb.NumCatalog = 0
}

// ReadCatalog 读取目录
func (mdb *MdbHandle) ReadCatalog(objtype int) []*MdbCatalogEntry {
	if mdb == nil {
		return nil
	}

	// 释放旧目录
	if mdb.Catalog != nil {
		mdb.FreeCatalog()
	}
	mdb.Catalog = make([]*MdbCatalogEntry, 0)
	mdb.NumCatalog = 0

	// 创建 MSysObjects 表的虚拟条目
	msysobj := &MdbCatalogEntry{
		Mdb:        mdb,
		ObjectType: MDBTable,
		TablePg:    2,
		ObjectName: "MSysObjects",
	}

	// 读取 MSysObjects 表
	table := mdb.ReadTable(msysobj)
	if table == nil {
		fmt.Printf("无法读取表 %s\n", msysobj.ObjectName)
		return nil
	}
	defer mdb.FreeTableDef(table)

	// 读取列
	if mdb.ReadColumns(table) == nil {
		fmt.Printf("无法读取表 %s 的列\n", msysobj.ObjectName)
		return nil
	}

	// 绑定列
	objIdBuf := make([]byte, mdb.BindSize)
	objNameBuf := make([]byte, mdb.BindSize)
	objTypeBuf := make([]byte, mdb.BindSize)
	objFlagsBuf := make([]byte, mdb.BindSize)
	objPropsBuf := make([]byte, mdb.BindSize)

	if mdb.BindColumnByName(table, "Id", objIdBuf, nil) == -1 ||
		mdb.BindColumnByName(table, "Name", objNameBuf, nil) == -1 ||
		mdb.BindColumnByName(table, "Type", objTypeBuf, nil) == -1 ||
		mdb.BindColumnByName(table, "Flags", objFlagsBuf, nil) == -1 {
		fmt.Printf("无法绑定表 %s 的列\n", msysobj.ObjectName)
		return nil
	}

	// LvProp 是 OLE 长字段：这里只会绑定到 12 字节 OLE 头，而下面的旧解析器
	// 仍要求完整 KKD 数据，因此暂不启用长度指针，避免把头部误当成属性内容。
	kkdSizeOle := 0
	propsIdx := mdb.BindColumnByName(table, "LvProp", objPropsBuf, nil)
	if propsIdx == -1 {
		fmt.Printf("无法绑定列 %s 从表 %s\n", "LvProp", msysobj.ObjectName)
		return nil
	}
	_ = propsIdx

	// 重置表
	table.RewindTable()

	// 读取所有行
	for table.FetchRow() {
		typeStr := strings.TrimRight(string(objTypeBuf), "\x00")
		typeVal, _ := strconv.Atoi(typeStr)

		if objtype == MDBAny || typeVal == objtype {
			idStr := strings.TrimRight(string(objIdBuf), "\x00")
			idVal, _ := strconv.Atoi(idStr)

			flagsStr := strings.TrimRight(string(objFlagsBuf), "\x00")
			flagsVal, _ := strconv.Atoi(flagsStr)

			entry := &MdbCatalogEntry{
				Mdb:        mdb,
				ObjectName: UTF16LEToString(objNameBuf),
				ObjectType: typeVal & 0x7F,
				TablePg:    uint32(idVal & 0x00FFFFFF),
				Flags:      flagsVal,
				Props:      make([]*MdbProperties, 0),
			}
			mdb.NumCatalog++
			mdb.Catalog = append(mdb.Catalog, entry)

			// 读取属性（如果存在）
			if kkdSizeOle > 0 {
				kkd := objPropsBuf[:kkdSizeOle]
				if kkd != nil {
					props := mdb.KKDToProps(kkd, len(kkd))
					if props != nil {
						entry.Props = append(entry.Props, props)
					}
				}
			}
		}
	}

	return mdb.Catalog
}

// GetCatalogEntryByName 根据名称获取目录条目
func (mdb *MdbHandle) GetCatalogEntryByName(name string) *MdbCatalogEntry {
	if mdb == nil {
		return nil
	}

	if mdb.NumCatalog == 0 {
		mdb.ReadCatalog(MDBAny)
	}

	for i := 0; i < int(mdb.NumCatalog); i++ {
		entry := mdb.Catalog[i]
		if entry != nil && strings.EqualFold(entry.ObjectName, name) {
			return entry
		}
	}

	return nil
}

// DumpCatalog 输出目录信息
func (mdb *MdbHandle) DumpCatalog(objType int) {
	if mdb == nil {
		return
	}

	mdb.ReadCatalog(objType)
	for i := 0; i < int(mdb.NumCatalog); i++ {
		entry := mdb.Catalog[i]
		if entry != nil && (objType == MDBAny || entry.ObjectType == objType) {
			fmt.Printf("Type: %-12s Name: %-48s Page: %06d\n",
				ObjectTypeString(entry.ObjectType),
				entry.ObjectName,
				entry.TablePg)
		}
	}
}
