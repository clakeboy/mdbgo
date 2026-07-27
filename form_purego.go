package mdbgo

import (
	"fmt"
	"sort"
	"strings"

	purego "github.com/clakeboy/mdbgo/purego"
)

// ---- 辅助函数 ----

func exportFormsFromPurego(db *DB) ([]FormInfo, error) {
	names, err := db.puregoDB.FormNames()
	if err != nil {
		return nil, err
	}
	result := make([]FormInfo, 0, len(names))
	for _, name := range names {
		form, err := exportFormFromPurego(db, name)
		if err != nil {
			continue
		}
		result = append(result, *form)
	}
	return result, nil
}

func exportFormFromPurego(db *DB, formName string) (*FormInfo, error) {
	handle := db.puregoDB.GetHandle()
	entry := handle.GetCatalogEntryByName(formName)
	if entry == nil || entry.ObjectType != purego.MDBForm {
		return nil, fmt.Errorf("未找到窗体: %s", formName)
	}

	form := &FormInfo{
		Name:       entry.ObjectName,
		ObjectType: entry.ObjectType,
		TablePage:  entry.TablePg,
		Flags:      entry.Flags,
	}

	for _, props := range entry.Props {
		if props == nil {
			continue
		}
		items := make([]PropertyItem, 0, len(props.Hash))
		for key, val := range props.Hash {
			items = append(items, PropertyItem{
				Key:   key,
				Value: fmt.Sprintf("%v", val),
			})
		}
		sort.Slice(items, func(i, j int) bool {
			return items[i].Key < items[j].Key
		})
		if props.Name == "" {
			form.Properties = append(form.Properties, items...)
		} else {
			form.Components = append(form.Components, FormComponent{
				Name:       props.Name,
				Properties: items,
			})
		}
	}

	return form, nil
}

func formNamesFromPurego(db *DB) ([]string, error) {
	return db.puregoDB.FormNames()
}

func formStreamsFromPurego(db *DB, formName string) (*FormStreams, error) {
	ps, err := db.puregoDB.ReadFormStreams(formName)
	if err != nil {
		return nil, err
	}
	return &FormStreams{
		FormName: ps.FormName,
		Lv:       ps.Lv,
		LvProp:   ps.LvProp,
		LvExtra:  ps.LvExtra,
	}, nil
}

func formObjectStreamsFromPurego(db *DB, formName string) (*FormObjectStreams, error) {
	po, err := db.puregoDB.ReadFormObjectStreams(formName)
	if err != nil {
		return nil, err
	}
	return &FormObjectStreams{
		FormName:  po.FormName,
		StorageID: po.StorageID,
		Blob:      po.Blob,
		TypeInfo:  po.TypeInfo,
		PropData:  po.PropData,
		BlobDelta: po.BlobDelta,
	}, nil
}

func exportFormContentFromPurego(db *DB, formName string) (*FormContent, error) {
	streams, err := formObjectStreamsFromPurego(db, formName)
	if err != nil {
		return nil, err
	}
	return ParseFormContent(streams)
}

func exportFormContentsFromPurego(db *DB) ([]FormContent, error) {
	entries, err := db.puregoDB.ReadAccessObjectEntries()
	if err != nil {
		return nil, err
	}

	// 转换为根包的类型
	rootEntries := make([]AccessObjectEntry, len(entries))
	for i, e := range entries {
		rootEntries[i] = AccessObjectEntry{
			Path:  e.Path,
			Name:  e.Name,
			IsDir: e.IsDir,
			Size:  e.Size,
			Data:  e.Data,
		}
	}

	formIDs, err := formStorageIDsFromEntries(rootEntries)
	if err != nil {
		return nil, err
	}

	formNames := make([]string, 0, len(formIDs))
	for name := range formIDs {
		formNames = append(formNames, name)
	}
	sort.Slice(formNames, func(i, j int) bool {
		return strings.ToLower(formNames[i]) < strings.ToLower(formNames[j])
	})

	contents := make([]FormContent, 0, len(formNames))
	for _, name := range formNames {
		streams, err := formObjectStreamsFromPurego(db, name)
		if err != nil {
			continue
		}
		content, err := ParseFormContent(streams)
		if err != nil {
			continue
		}
		contents = append(contents, *content)
	}
	return contents, nil
}

func accessObjectStorageKindFromPurego(db *DB) int {
	return db.puregoDB.HasAccessObjectStorage()
}
