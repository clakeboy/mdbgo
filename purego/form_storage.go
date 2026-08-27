package purego

import (
	"bytes"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode"

	"github.com/richardlehane/mscfb"
)

// AccessObjectEntry 是 Access 内部 OLE Compound 容器中的目录或数据流。
type AccessObjectEntry struct {
	Path  string
	Name  string
	IsDir bool
	Size  int64
	Data  []byte
}

// FormObjectStreams 是一个窗体在 Access 内部存储中的完整设计流。
type FormObjectStreams struct {
	FormName  string
	StorageID int
	Blob      []byte
	TypeInfo  []byte
	PropData  []byte
	BlobDelta []byte
}

// AccessObjectContainer 是由 MSysAccessObjects.Data 分片按 ID 顺序重组的
// Access 内部 OLE Compound 容器。
type AccessObjectContainer struct {
	FirstObjectID int
	LastObjectID  int
	Data          []byte
}

// ReadAccessObjectContainer 将 MSysAccessObjects 的分片数据重组为 OLE Compound 容器。
func (mdb *MDB) ReadAccessObjectContainer() (*AccessObjectContainer, error) {
	if mdb.handle == nil {
		return nil, fmt.Errorf("数据库未打开")
	}

	kind := mdb.HasAccessObjectStorage()
	switch kind {
	case AccessStorageTree:
		return nil, fmt.Errorf("MSysAccessStorage 没有 OLE Compound 容器，请使用 ReadAccessObjectEntries")
	case AccessStorageNone:
		return nil, fmt.Errorf("数据库没有 MSysAccessObjects 或 MSysAccessStorage")
	case AccessStorageObjects:
	default:
		return nil, fmt.Errorf("不支持的 Access 对象存储类型: %d", kind)
	}

	objects, err := mdb.handle.readMSysAccessObjectsAll()
	if err != nil {
		return nil, err
	}
	if len(objects) == 0 {
		return nil, fmt.Errorf("MSysAccessObjects 为空")
	}

	sort.Slice(objects, func(i, j int) bool {
		return objects[i].ObjectID < objects[j].ObjectID
	})

	compoundMagic := []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}
	result := &AccessObjectContainer{FirstObjectID: -1, LastObjectID: -1}
	startIndex, startOffset := accessObjectContainerStart(objects, compoundMagic)
	if startIndex < 0 {
		return nil, fmt.Errorf("MSysAccessObjects 中没有 OLE Compound 容器")
	}

	for i, obj := range objects[startIndex:] {
		if len(obj.Data) == 0 {
			continue
		}
		if i == 0 {
			result.FirstObjectID = obj.ObjectID
			result.Data = append(result.Data, obj.Data[startOffset:]...)
		} else {
			result.Data = append(result.Data, obj.Data...)
		}
		result.LastObjectID = obj.ObjectID
	}

	if len(result.Data) == 0 {
		return nil, fmt.Errorf("MSysAccessObjects 中没有 OLE Compound 容器")
	}
	return result, nil
}

// accessObjectContainerStart 定位 Access Forms 主 OLE Compound 容器的起始分片和偏移。
//
// Access 数据库的较早记录中可能包含内嵌 CFB 对象，不能把分片中任意位置出现的
// CFB 签名直接当成主容器。主容器通常从一个分片的首字节开始，因此优先选择这种
// 边界；仅在旧数据库没有首字节签名时，才兼容原有的分片内部签名布局。
func accessObjectContainerStart(objects []*AccessObjectData, compoundMagic []byte) (int, int) {
	for i, obj := range objects {
		if bytes.HasPrefix(obj.Data, compoundMagic) {
			return i, 0
		}
	}

	for i, obj := range objects {
		if offset := bytes.Index(obj.Data, compoundMagic); offset >= 0 {
			return i, offset
		}
	}

	return -1, -1
}

// ReadAccessObjectEntries 读取 Access 内部对象存储的全部目录和流。
//
// Access 2000 的 MSysAccessObjects 保存 OLE Compound 分片；Access 2003 的
// MSysAccessStorage 直接保存父子目录树。两种布局都归一化为 AccessObjectEntry。
func (mdb *MDB) ReadAccessObjectEntries() ([]AccessObjectEntry, error) {
	if mdb.handle == nil {
		return nil, fmt.Errorf("数据库未打开")
	}

	kind := mdb.HasAccessObjectStorage()
	switch kind {
	case AccessStorageTree:
		rows, err := mdb.handle.readMSysAccessStorageAll()
		if err != nil {
			return nil, err
		}
		return accessObjectEntriesFromStorage(rows)
	case AccessStorageNone:
		return nil, fmt.Errorf("数据库没有 MSysAccessObjects 或 MSysAccessStorage")
	case AccessStorageObjects:
	default:
		return nil, fmt.Errorf("不支持的 Access 对象存储类型: %d", kind)
	}

	container, err := mdb.ReadAccessObjectContainer()
	if err != nil {
		return nil, err
	}

	reader, err := mscfb.New(bytes.NewReader(container.Data))
	if err != nil {
		return nil, fmt.Errorf("解析 Access OLE Compound 容器: %w", err)
	}

	entries := make([]AccessObjectEntry, 0, len(reader.File)-1)
	for _, file := range reader.File[1:] {
		entry := AccessObjectEntry{
			Path:  strings.Join(append(append([]string(nil), file.Path...), file.Name), "/"),
			Name:  file.Name,
			IsDir: file.FileInfo().IsDir(),
			Size:  file.Size,
		}
		if !entry.IsDir && entry.Size > 0 {
			entry.Data, err = io.ReadAll(file)
			if err != nil {
				return nil, fmt.Errorf("读取 Access OLE 流 %q: %w", entry.Path, err)
			}
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func accessObjectEntriesFromStorage(rows []*AccessStorageRow) ([]AccessObjectEntry, error) {
	if len(rows) == 0 {
		return nil, fmt.Errorf("MSysAccessStorage 为空")
	}

	rowsByID := make(map[int]*AccessStorageRow, len(rows))
	for _, row := range rows {
		if _, exists := rowsByID[row.ID]; exists {
			return nil, fmt.Errorf("MSysAccessStorage 中存在重复的 Id: %d", row.ID)
		}
		rowsByID[row.ID] = row
	}

	paths := make(map[int]string, len(rows))
	visiting := make(map[int]bool)

	var pathForID func(int) (string, error)
	pathForID = func(id int) (string, error) {
		if path, ok := paths[id]; ok {
			return path, nil
		}
		row, ok := rowsByID[id]
		if !ok {
			return "", fmt.Errorf("MSysAccessStorage 的父节点未找到: %d", id)
		}
		if visiting[id] {
			return "", fmt.Errorf("MSysAccessStorage 在 Id=%d 处存在循环", id)
		}
		if row.ParentID == row.ID {
			paths[id] = ""
			return "", nil
		}

		visiting[id] = true
		parentPath, err := pathForID(row.ParentID)
		delete(visiting, id)
		if err != nil {
			return "", err
		}
		name := strings.TrimLeftFunc(row.Name, unicode.IsControl)
		if name == "" {
			return "", fmt.Errorf("MSysAccessStorage 条目 Name 为空: Id=%d", id)
		}
		path := name
		if parentPath != "" {
			path = parentPath + "/" + name
		}
		paths[id] = path
		return path, nil
	}

	entries := make([]AccessObjectEntry, 0, len(rows))
	for _, row := range rows {
		if row.ParentID == row.ID {
			continue
		}
		path, err := pathForID(row.ID)
		if err != nil {
			return nil, err
		}
		name := strings.TrimLeftFunc(row.Name, unicode.IsControl)
		entry := AccessObjectEntry{
			Path: path,
			Name: name,
			Size: int64(len(row.Data)),
			Data: row.Data,
		}
		switch row.Type {
		case 1:
			entry.IsDir = true
		case 2:
		default:
			return nil, fmt.Errorf("MSysAccessStorage 不支持的 Type=%d: Id=%d", row.Type, row.ID)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// ReadFormObjectStreams 按窗体名读取 Blob、TypeInfo、PropData 和 BlobDelta。
func (mdb *MDB) ReadFormObjectStreams(formName string) (*FormObjectStreams, error) {
	if mdb.handle == nil {
		return nil, fmt.Errorf("数据库未打开")
	}
	if strings.TrimSpace(formName) == "" {
		return nil, fmt.Errorf("窗体名为空")
	}

	entries, err := mdb.ReadAccessObjectEntries()
	if err != nil {
		return nil, err
	}
	return formObjectStreamsFromEntries(entries, formName)
}

func formStorageIDsFromEntries(entries []AccessObjectEntry) (map[string]int, error) {
	var dirData []byte
	for _, entry := range entries {
		if !entry.IsDir && entry.Path == "Forms/DirData" {
			dirData = entry.Data
			break
		}
	}
	if len(dirData) == 0 {
		for _, entry := range entries {
			if !entry.IsDir && strings.HasSuffix(entry.Path, "/Forms/DirData") {
				dirData = entry.Data
				break
			}
		}
	}
	if len(dirData) == 0 {
		return nil, fmt.Errorf("未找到 Forms/DirData 流")
	}

	formIDs, err := parseAccessStorageDirData(dirData)
	if err != nil {
		return nil, fmt.Errorf("解析 Forms/DirData: %w", err)
	}
	return formIDs, nil
}

func formObjectStreamsFromEntries(entries []AccessObjectEntry, formName string) (*FormObjectStreams, error) {
	formIDs, err := formStorageIDsFromEntries(entries)
	if err != nil {
		return nil, err
	}

	storageID := -1
	for name, id := range formIDs {
		if strings.EqualFold(name, formName) {
			storageID = id
			break
		}
	}
	if storageID < 0 {
		return nil, fmt.Errorf("在 Access Forms 存储中未找到窗体: %s", formName)
	}

	result := &FormObjectStreams{FormName: formName, StorageID: storageID}
	prefix := fmt.Sprintf("Forms/%d/", storageID)
	for _, entry := range entries {
		if entry.IsDir || !strings.HasPrefix(entry.Path, prefix) {
			continue
		}
		switch entry.Name {
		case "Blob":
			result.Blob = entry.Data
		case "TypeInfo":
			result.TypeInfo = entry.Data
		case "PropData":
			result.PropData = entry.Data
		case "BlobDelta":
			result.BlobDelta = entry.Data
		}
	}
	if len(result.Blob) == 0 && len(result.TypeInfo) == 0 {
		return nil, fmt.Errorf("窗体存储 Forms/%d 没有 Blob 或 TypeInfo: %s", storageID, formName)
	}
	return result, nil
}

func parseAccessStorageDirData(data []byte) (map[string]int, error) {
	result := make(map[string]int)
	if len(data) < 4 {
		return result, nil
	}
	pos := 4
	for pos+2 <= len(data) {
		if data[pos] != 0x04 {
			break
		}
		declaredLen := int(data[pos+1])
		pos += 2
		if declaredLen < 4 || pos+declaredLen > len(data) {
			return nil, fmt.Errorf("DirData 条目长度无效 %d 在偏移 %d", declaredLen, pos-2)
		}

		actualEnd := pos + declaredLen
		if le16(data[actualEnd-2:]) != 0 {
			found := false
			for scan := actualEnd; scan+2 <= len(data); scan += 2 {
				if le16(data[scan:]) == 0 {
					actualEnd = scan + 2
					found = true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("DirData 条目未终止，偏移 %d", pos-2)
			}
		}
		if actualEnd < pos+4 {
			return nil, fmt.Errorf("DirData 条目过短，偏移 %d", pos-2)
		}

		name := strings.TrimSpace(decodeUTF16LE(data[pos : actualEnd-4]))
		storageID := int(le16(data[actualEnd-4:]))
		if name != "" {
			result[name] = storageID
		}
		pos = actualEnd
	}
	return result, nil
}

// le16 读取小端序 16 位整数。
func le16(data []byte) uint16 {
	if len(data) < 2 {
		return 0
	}
	return uint16(data[0]) | uint16(data[1])<<8
}

// decodeUTF16LE 将 UTF-16LE 字节解码为 Go 字符串。
func decodeUTF16LE(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	end := len(data)
	for i := 0; i+1 < len(data); i += 2 {
		if data[i] == 0 && data[i+1] == 0 {
			end = i
			break
		}
	}
	if end == 0 {
		return ""
	}
	u16s := make([]uint16, end/2)
	for i := range u16s {
		u16s[i] = uint16(data[i*2]) | uint16(data[i*2+1])<<8
	}
	return string(runesToString(u16s))
}

func runesToString(u16s []uint16) string {
	runes := make([]rune, len(u16s))
	for i, v := range u16s {
		runes[i] = rune(v)
	}
	return string(runes)
}
