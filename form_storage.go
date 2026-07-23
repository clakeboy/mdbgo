package mdbgo

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/richardlehane/mscfb"
)

// AccessObjectData 是 MSysAccessObjects.Data 的原始内容。
type AccessObjectData struct {
	ObjectID int
	Data     []byte
}

// AccessObjectContainer 是由 MSysAccessObjects.Data 分片按 ID 顺序重组的
// Access 内部 OLE Compound 容器。
type AccessObjectContainer struct {
	FirstObjectID int
	LastObjectID  int
	Data          []byte
}

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

// ReadAccessObjectContainer 按 ID 顺序重组 MSysAccessObjects.Data。
//
// Jet/Access 会把 VBA、Form 等对象流保存在同一个 OLE Compound 文件中，
// MSysAccessObjects 的每一行只是该文件的一个连续分片，不能独立映射到某个窗体。
func (db *DB) ReadAccessObjectContainer() (*AccessObjectContainer, error) {
	if db == nil || db.ptr == nil {
		return nil, errors.New("db is closed")
	}

	objects, err := db.readAccessObjectDataAll()
	if err != nil {
		return nil, err
	}
	if len(objects) == 0 {
		return nil, errors.New("MSysAccessObjects is empty")
	}
	sort.Slice(objects, func(i, j int) bool {
		return objects[i].ObjectID < objects[j].ObjectID
	})

	compoundMagic := []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}
	result := &AccessObjectContainer{FirstObjectID: -1, LastObjectID: -1}
	started := false
	for i := range objects {
		obj := &objects[i]
		if len(obj.Data) == 0 {
			continue
		}
		if !started {
			off := bytes.Index(obj.Data, compoundMagic)
			if off < 0 {
				continue
			}
			started = true
			result.FirstObjectID = obj.ObjectID
			result.Data = append(result.Data, obj.Data[off:]...)
		} else {
			result.Data = append(result.Data, obj.Data...)
		}
		result.LastObjectID = obj.ObjectID
	}
	if len(result.Data) == 0 {
		return nil, errors.New("MSysAccessObjects contains no OLE Compound container")
	}
	return result, nil
}

// ReadAccessObjectEntries 读取 Access 内部 OLE Compound 容器的全部目录和流。
func (db *DB) ReadAccessObjectEntries() ([]AccessObjectEntry, error) {
	container, err := db.ReadAccessObjectContainer()
	if err != nil {
		return nil, err
	}

	reader, err := mscfb.New(bytes.NewReader(container.Data))
	if err != nil {
		return nil, fmt.Errorf("parse Access OLE Compound container: %w", err)
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
				return nil, fmt.Errorf("read Access OLE stream %q: %w", entry.Path, err)
			}
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// ReadFormObjectStreams 按窗体名读取 Blob、TypeInfo、PropData 和 BlobDelta。
func (db *DB) ReadFormObjectStreams(formName string) (*FormObjectStreams, error) {
	if db == nil || db.ptr == nil {
		return nil, errors.New("db is closed")
	}
	if strings.TrimSpace(formName) == "" {
		return nil, errors.New("form name is empty")
	}

	entries, err := db.ReadAccessObjectEntries()
	if err != nil {
		return nil, err
	}
	return formObjectStreamsFromEntries(entries, formName)
}

func formStorageIDsFromEntries(entries []AccessObjectEntry) (map[string]int, error) {
	var dirData []byte
	for _, entry := range entries {
		if !entry.IsDir && (entry.Path == "Forms/DirData" || strings.HasSuffix(entry.Path, "/DirData")) {
			dirData = entry.Data
			break
		}
	}
	if len(dirData) == 0 {
		return nil, errors.New("Forms/DirData stream not found")
	}

	formIDs, err := parseAccessStorageDirData(dirData)
	if err != nil {
		return nil, fmt.Errorf("parse Forms/DirData: %w", err)
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
		return nil, fmt.Errorf("form not found in Access Forms storage: %s", formName)
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
		return nil, fmt.Errorf("form storage Forms/%d has no Blob or TypeInfo: %s", storageID, formName)
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
			return nil, fmt.Errorf("invalid DirData entry length %d at offset %d", declaredLen, pos-2)
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
				return nil, fmt.Errorf("unterminated DirData entry at offset %d", pos-2)
			}
		}
		if actualEnd < pos+4 {
			return nil, fmt.Errorf("DirData entry is too short at offset %d", pos-2)
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
