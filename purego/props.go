package purego

import (
	"fmt"
	"strings"
)

// FreeProps 释放属性
func FreeProps(props *MdbProperties) {
	if props == nil {
		return
	}
	props.Name = ""
	props.Hash = nil
}

// AllocProps 分配属性
func AllocProps() *MdbProperties {
	return &MdbProperties{
		Name: "",
		Hash: make(map[string]interface{}),
	}
}

// ReadPropsList 读取属性名称列表
func (mdb *MdbHandle) ReadPropsList(kkd []byte, length int) []string {
	pos := 0
	names := make([]string, 0)

	for pos < length {
		if pos+2 > length {
			break
		}
		recordLen := GetInt16(kkd, pos)
		pos += 2

		if pos+recordLen > length {
			break
		}

		// 简单的 UTF-8 转换
		name := string(kkd[pos : pos+recordLen])
		pos += recordLen
		names = append(names, name)
	}

	return names
}

// ReadProps 读取属性
func (mdb *MdbHandle) ReadProps(names []string, kkd []byte, length int) *MdbProperties {
	pos := 0
	props := AllocProps()

	// 读取记录长度
	if pos+4 > length {
		return props
	}
	_ = GetInt16(kkd, pos) // record_len
	pos += 4

	// 读取名称长度
	if pos+2 > length {
		return props
	}
	nameLen := GetInt16(kkd, pos)
	pos += 2

	if nameLen > 0 && pos+nameLen <= length {
		props.Name = string(kkd[pos : pos+nameLen])
	}
	pos += nameLen

	// 读取属性值
	for pos < length {
		if pos+8 > length {
			break
		}

		recordLen := GetInt16(kkd, pos)
		_ = kkd[pos+3] // dtype
		elem := GetInt16(kkd, pos+4)
		_ = GetInt16(kkd, pos+6) // dsize

		if elem < 0 || elem >= len(names) {
			break
		}

		dsize := GetInt16(kkd, pos+6)
		if dsize < 0 || pos+8+dsize > length {
			break
		}

		value := string(kkd[pos+8 : pos+8+dsize])
		name := names[elem]

		// 根据类型处理值
		props.Hash[name] = value

		pos += recordLen
	}

	return props
}

// DumpProps 输出属性信息
func (mdb *MdbHandle) DumpProps(props *MdbProperties, showName int) {
	if props == nil {
		return
	}

	if showName != 0 {
		name := props.Name
		if name == "" {
			name = "(none)"
		}
		fmt.Printf("name: %s\n", name)
	}

	for k, v := range props.Hash {
		fmt.Printf("\t%s: %s\n", k, v)
	}

	if showName != 0 {
		fmt.Println()
	}
}

// KKDToProps 将 KKD 数据转换为属性
func (mdb *MdbHandle) KKDToProps(buffer []byte, length int) *MdbProperties {
	if length < 4 {
		return nil
	}

	// 检查格式
	format := string(buffer[:4])
	if format != "KKD" && format != "MR2" {
		fmt.Printf("无法识别的格式: %s\n", format)
		return nil
	}

	var names []string
	props := AllocProps()

	pos := 4
	for pos < length {
		if pos+6 > length {
			break
		}

		recordLen := GetInt32(buffer, pos)
		recordType := GetInt16(buffer, pos+4)

		if recordLen < 6 || pos+int(recordLen) > length {
			break
		}

		switch recordType {
		case 0x80:
			// 属性名称列表
			names = mdb.ReadPropsList(buffer[pos+6:pos+int(recordLen)], int(recordLen)-6)
		case 0x00, 0x01, 0x02:
			// 属性值
			if names == nil {
				fmt.Printf("序列错误!\n")
				break
			}
			propBlock := mdb.ReadProps(names, buffer[pos+6:pos+int(recordLen)], int(recordLen)-6)
			if propBlock != nil {
				// 合并属性
				for k, v := range propBlock.Hash {
					props.Hash[k] = v
				}
				if propBlock.Name != "" {
					props.Name = propBlock.Name
				}
			}
		default:
			fmt.Printf("未知记录类型 %d\n", recordType)
		}

		pos += int(recordLen)
	}

	return props
}

// ColToString 将列值转换为字符串
func (mdb *MdbHandle) ColToString(buf []byte, start int, dataType int, size int) string {
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
	case MDBLongInt:
		return fmt.Sprintf("%d", int32(GetInt32(buf, start)))
	case MDBFloat:
		return fmt.Sprintf("%g", GetSingle(buf, start))
	case MDBDouble:
		return fmt.Sprintf("%g", GetDouble(buf, start))
	case MDBMoney:
		return mdb.MoneyToString(buf, start)
	case MDBDateTime:
		// 简单的日期转换
		return fmt.Sprintf("%v", GetDouble(buf, start))
	case MDBText:
		return string(buf[start : start+size])
	case MDBBinary, MDBOle:
		return fmt.Sprintf("(binary data of length %d)", size)
	case MDBRepId:
		return mdb.UUIDToString(buf, start)
	case MDBNumeric:
		return mdb.NumericToString(buf, start, 0, 0)
	default:
		return string(buf[start : start+size])
	}
}

// UUIDToString 将 UUID 转换为字符串
func (mdb *MdbHandle) UUIDToString(buf []byte, start int) string {
	if start < 0 || start+16 > len(buf) {
		return ""
	}

	// 标准 UUID 格式: {XXXXXXXX-XXXX-XXXX-XXXX-XXXXXXXXXXXX}
	return fmt.Sprintf("{%02X%02X%02X%02X-%02X%02X-%02X%02X-%02X%02X-%02X%02X%02X%02X%02X%02X}",
		buf[start+3], buf[start+2], buf[start+1], buf[start],
		buf[start+5], buf[start+4],
		buf[start+7], buf[start+6],
		buf[start+8], buf[start+9],
		buf[start+10], buf[start+11], buf[start+12], buf[start+13], buf[start+14], buf[start+15])
}

// SetDefaultBackend 设置默认后端
func (mdb *MdbHandle) SetDefaultBackend(name string) {
	mdb.BackendName = name
}

// RemoveBackends 移除所有后端
func (mdb *MdbHandle) RemoveBackends() {
	mdb.Backends = make(map[string]*MdbBackend)
}

// GetColBackendType 获取列的后端类型
func (mdb *MdbHandle) GetColBackendType(col *MdbColumn) *MdbBackendType {
	if col == nil {
		return nil
	}

	// 根据列类型返回后端类型
	switch col.ColType {
	case MDBBool:
		return &MdbBackendType{Name: "BOOLEAN"}
	case MDBByte:
		return &MdbBackendType{Name: "BYTE"}
	case MDBInt:
		return &MdbBackendType{Name: "SHORT"}
	case MDBLongInt:
		return &MdbBackendType{Name: "LONG"}
	case MDBMoney:
		return &MdbBackendType{Name: "CURRENCY", NeedsPrecision: true, NeedsScale: true}
	case MDBFloat:
		return &MdbBackendType{Name: "SINGLE", NeedsPrecision: true, NeedsScale: true}
	case MDBDouble:
		return &MdbBackendType{Name: "DOUBLE", NeedsPrecision: true, NeedsScale: true}
	case MDBDateTime:
		return &MdbBackendType{Name: "DATETIME"}
	case MDBBinary:
		return &MdbBackendType{Name: "BINARY", NeedsByteLength: true}
	case MDBText:
		return &MdbBackendType{Name: "TEXT", NeedsCharLength: true}
	case MDBOle:
		return &MdbBackendType{Name: "OLEOBJECT"}
	case MDBMemo:
		return &MdbBackendType{Name: "MEMO"}
	case MDBRepId:
		return &MdbBackendType{Name: "GUID"}
	case MDBNumeric:
		return &MdbBackendType{Name: "NUMERIC", NeedsPrecision: true, NeedsScale: true}
	default:
		return &MdbBackendType{Name: "UNKNOWN"}
	}
}

// GetColBackendTypeString 获取列的后端类型字符串
func (mdb *MdbHandle) GetColBackendTypeString(col *MdbColumn) string {
	if col == nil {
		return "UNKNOWN"
	}

	t := mdb.GetColBackendType(col)
	if t == nil {
		return "UNKNOWN"
	}
	return strings.ToUpper(t.Name)
}

// ColBackendTypeTakesLength 判断列后端类型是否需要长度
func (mdb *MdbHandle) ColBackendTypeTakesLength(col *MdbColumn) bool {
	if col == nil {
		return false
	}

	t := mdb.GetColBackendType(col)
	if t == nil {
		return false
	}

	return t.NeedsCharLength || t.NeedsByteLength
}

// SetDateFmt 设置日期格式
func (mdb *MdbHandle) SetDateFmtInternal(fmt string) {
	mdb.DateFmt = fmt
}

// SetShortDateFmt 设置短日期格式
func (mdb *MdbHandle) SetShortDateFmtInternal(fmt string) {
	mdb.ShortDateFmt = fmt
}

// SetRepidFmt 设置 UUID 格式
func (mdb *MdbHandle) SetRepidFmtInternal(fmt MdbUuidFormat) {
	mdb.RepidFmt = fmt
}

// SetBooleanFmtNumbers 设置布尔值格式为数字
func (mdb *MdbHandle) SetBooleanFmtNumbersInternal() {
	mdb.BooleanFalse = "0"
	mdb.BooleanTrue = "1"
}

// SetBooleanFmtWords 设置布尔值格式为单词
func (mdb *MdbHandle) SetBooleanFmtWordsInternal() {
	mdb.BooleanFalse = "False"
	mdb.BooleanTrue = "True"
}

// SetBindSize 设置绑定大小
func (mdb *MdbHandle) SetBindSizeInternal(size int) {
	mdb.BindSize = size
}
