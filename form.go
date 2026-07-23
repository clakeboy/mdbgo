package mdbgo

/*
#cgo CFLAGS: -I${SRCDIR}/internal/bundled
#cgo CFLAGS: -I${SRCDIR}/internal/bundled/include
#cgo CFLAGS: -DTLS=__thread
#cgo CFLAGS: -DICONV_CONST=const
#cgo CFLAGS: -DHAVE_STRTOK_R=1
#cgo CFLAGS: -DHAVE_SETLOCALE=1
#cgo CFLAGS: -DHAVE_SYS_STAT_H=1
#cgo CFLAGS: -DHAVE_SYS_TYPES_H=1
#cgo darwin CFLAGS: -DHAVE_REALLOCF=1
#include <stdlib.h>
#include "bridge.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unsafe"
)

// PropertyItem 表示 Access 对象属性的键值对。
type PropertyItem struct {
	Key   string
	Value string
}

// FormComponent 表示窗体上的一个组件及其属性。
type FormComponent struct {
	Name       string
	Properties []PropertyItem
}

// FormInfo 表示 Access 窗体及其组件信息。
type FormInfo struct {
	Name           string
	ObjectType     int
	ObjectTypeName string
	TablePage      uint32
	Flags          int
	Properties     []PropertyItem
	Components     []FormComponent
}

// FormContent 是从 Access 内部 Forms 存储解析出的窗体内容。
type FormContent struct {
	FormName        string
	StorageID       int
	Width           int
	Height          int
	Caption         string
	DefaultView     int
	RecordSource    string
	BackColor       string
	BackColorValue  uint32
	BackGroundColor string
	Properties      []FormProperty
	Sections        []FormSectionContent
	// Controls 保留 TypeInfo 原始顺序的平面列表，以兼容已有调用；
	// 分区标记可通过 IsSection 区分，普通控件通过 Section 标明所属分区。
	Controls []FormControlContent
}

// ExportForms 导出 Access 窗体及其组件信息。
//
// 返回结果中：
// 1. `Properties` 是窗体级别属性（属性块名为空）。
// 2. `Components` 是组件级属性（属性块名非空，通常为控件名）。
func (db *DB) ExportForms() ([]FormInfo, error) {
	if db == nil || db.ptr == nil {
		return nil, errors.New("db is closed")
	}

	errBuf := make([]byte, cErrBufSize)
	var raw C.mdbgo_forms_data_t

	rc := C.mdbgo_export_forms(db.ptr, &raw, (*C.char)(unsafe.Pointer(&errBuf[0])), C.size_t(len(errBuf)))
	if rc != 0 {
		return nil, errors.New(cStringFromBuf(errBuf))
	}
	defer C.mdbgo_free_forms_data(&raw)

	formCount := int(raw.form_count)
	result := make([]FormInfo, formCount)
	if formCount == 0 {
		return result, nil
	}

	cForms := unsafe.Slice((*C.mdbgo_form_info_t)(unsafe.Pointer(raw.forms)), formCount)
	for i := 0; i < formCount; i++ {
		form := FormInfo{
			Name:           C.GoString(cForms[i].name),
			ObjectType:     int(cForms[i].object_type),
			ObjectTypeName: C.GoString(cForms[i].object_type_name),
			TablePage:      uint32(cForms[i].table_pg),
			Flags:          int(cForms[i].flags),
		}

		blocksCount := int(cForms[i].prop_block_count)
		if blocksCount > 0 {
			cBlocks := unsafe.Slice((*C.mdbgo_property_block_t)(unsafe.Pointer(cForms[i].prop_blocks)), blocksCount)
			components := make([]FormComponent, 0, blocksCount)

			for j := 0; j < blocksCount; j++ {
				blockName := C.GoString(cBlocks[j].name)
				props := propertyItemsFromC(cBlocks[j].items, int(cBlocks[j].item_count))

				// 空块名按窗体属性处理，非空块名按组件处理。
				if blockName == "" {
					form.Properties = append(form.Properties, props...)
					continue
				}
				components = append(components, FormComponent{
					Name:       blockName,
					Properties: props,
				})
			}

			form.Components = components
		}

		result[i] = form
	}

	return result, nil
}

// ExportForm 按名称导出单个 Access 窗体及其组件信息。
//
// 窗体名称按 Access 的规则不区分大小写；返回的 Name 保留数据库中的原始名称。
func (db *DB) ExportForm(formName string) (*FormInfo, error) {
	if db == nil || db.ptr == nil {
		return nil, errors.New("db is closed")
	}
	if strings.TrimSpace(formName) == "" {
		return nil, errors.New("form name is empty")
	}

	forms, err := db.ExportForms()
	if err != nil {
		return nil, err
	}
	for i := range forms {
		if strings.EqualFold(forms[i].Name, formName) {
			return &forms[i], nil
		}
	}

	return nil, fmt.Errorf("form not found: %s", formName)
}

// ReadFormStreams 读取指定窗体的原始设计流（Lv/LvProp/LvExtra）。
//
// 这些字节是 Access 内部二进制格式，供 Go 侧自行解析。
func (db *DB) ReadFormStreams(formName string) (*FormStreams, error) {
	if db == nil || db.ptr == nil {
		return nil, errors.New("db is closed")
	}
	if strings.TrimSpace(formName) == "" {
		return nil, errors.New("form name is empty")
	}

	cName := C.CString(formName)
	defer C.free(unsafe.Pointer(cName))

	errBuf := make([]byte, cErrBufSize)
	var raw C.mdbgo_form_streams_t

	rc := C.mdbgo_read_form_streams(db.ptr, cName, &raw, (*C.char)(unsafe.Pointer(&errBuf[0])), C.size_t(len(errBuf)))
	if rc != 0 {
		return nil, errors.New(cStringFromBuf(errBuf))
	}
	defer C.mdbgo_free_form_streams(&raw)

	return &FormStreams{
		FormName: C.GoString(raw.form_name),
		Lv:       cBytesToGo(raw.lv, int(raw.lv_len)),
		LvProp:   cBytesToGo(raw.lv_prop, int(raw.lv_prop_len)),
		LvExtra:  cBytesToGo(raw.lv_extra, int(raw.lv_extra_len)),
	}, nil
}

// ReadAccessObjectDataByID 按 ID 读取 MSysAccessObjects.Data 原始字节。
func (db *DB) ReadAccessObjectDataByID(objectID int) (*AccessObjectData, error) {
	if db == nil || db.ptr == nil {
		return nil, errors.New("db is closed")
	}
	if objectID < 0 {
		return nil, errors.New("object id must be >= 0")
	}

	errBuf := make([]byte, cErrBufSize)
	var raw C.mdbgo_blob_data_t
	rc := C.mdbgo_read_access_object_data_by_id(
		db.ptr,
		C.int(objectID),
		&raw,
		(*C.char)(unsafe.Pointer(&errBuf[0])),
		C.size_t(len(errBuf)),
	)
	if rc != 0 {
		return nil, errors.New(cStringFromBuf(errBuf))
	}
	defer C.mdbgo_free_blob_data(&raw)

	return &AccessObjectData{
		ObjectID: objectID,
		Data:     cBytesToGo(raw.data, int(raw.len)),
	}, nil
}

// readAccessObjectDataAll 单次顺序扫描 MSysAccessObjects，避免按 ID 重复整表扫描。
func (db *DB) readAccessObjectDataAll() ([]AccessObjectData, error) {
	if db == nil || db.ptr == nil {
		return nil, errors.New("db is closed")
	}

	errBuf := make([]byte, cErrBufSize)
	var raw C.mdbgo_access_object_data_array_t
	rc := C.mdbgo_read_access_object_data_all(
		db.ptr,
		&raw,
		(*C.char)(unsafe.Pointer(&errBuf[0])),
		C.size_t(len(errBuf)),
	)
	if rc != 0 {
		return nil, errors.New(cStringFromBuf(errBuf))
	}
	defer C.mdbgo_free_access_object_data_array(&raw)

	n := int(raw.count)
	if n <= 0 || raw.values == nil {
		return nil, nil
	}
	cValues := unsafe.Slice((*C.mdbgo_access_object_data_t)(unsafe.Pointer(raw.values)), n)
	result := make([]AccessObjectData, n)
	for i := range cValues {
		result[i] = AccessObjectData{
			ObjectID: int(cValues[i].object_id),
			Data:     cBytesToGo(cValues[i].data, int(cValues[i].len)),
		}
	}
	return result, nil
}

// ExportFormContent 读取并导出指定窗体的完整内容。
//
// 此方法只解析指定窗体，不会调用 ExportFormContents 或解析其他窗体。
func (db *DB) ExportFormContent(formName string) (*FormContent, error) {
	streams, err := db.ReadFormObjectStreams(formName)
	if err != nil {
		return nil, err
	}
	return ParseFormContent(streams)
}

// ExportFormContents 一次读取内部 OLE 容器并导出全部窗体内容。
// 相比循环调用 ReadFormContent，此方法不会为每个窗体重复重组 MSysAccessObjects。
func (db *DB) ExportFormContents() ([]FormContent, error) {
	if db == nil || db.ptr == nil {
		return nil, errors.New("db is closed")
	}

	entries, err := db.ReadAccessObjectEntries()
	if err != nil {
		return nil, err
	}
	formIDs, err := formStorageIDsFromEntries(entries)
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

	contents := make([]FormContent, len(formNames))
	workers := runtime.NumCPU()
	if workers < 1 {
		workers = 1
	}
	if workers > 16 {
		workers = 16
	}
	if workers > len(formNames) {
		workers = len(formNames)
	}

	taskCh := make(chan int, len(formNames))
	for i := 0; i < len(formNames); i++ {
		taskCh <- i
	}
	close(taskCh)

	var wg sync.WaitGroup
	var errOnce sync.Once
	var firstErr error

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range taskCh {
				formName := formNames[idx]
				streams, err := formObjectStreamsFromEntries(entries, formName)
				if err != nil {
					errOnce.Do(func() { firstErr = err })
					return
				}
				content, err := ParseFormContent(streams)
				if err != nil {
					errOnce.Do(func() { firstErr = err })
					return
				}
				contents[idx] = *content
			}
		}()
	}
	wg.Wait()

	if firstErr != nil {
		return nil, firstErr
	}
	return contents, nil
}

// ReadFormContent 读取并解析指定窗体的控件目录和设计属性。
func (db *DB) ReadFormContent(formName string) (*FormContent, error) {
	return db.ExportFormContent(formName)
}

// ParseFormContent 解析 ReadFormObjectStreams 返回的原始设计流。
func ParseFormContent(streams *FormObjectStreams) (*FormContent, error) {
	if streams == nil {
		return nil, errors.New("form object streams is nil")
	}

	controls, err := ParseFormTypeInfo(streams.TypeInfo)
	if err != nil {
		return nil, fmt.Errorf("parse TypeInfo for %s: %w", streams.FormName, err)
	}
	controlOffsets := orderedFormControlOffsets(streams.Blob, controls)
	formProps, controlGroups := parseFormBlob(streams.Blob)
	jet4FormProps, jet4ControlProps := parseJet4FormTextProperties(streams.Blob, controls)
	jet4NumericProps := parseJet4FormNumericProperties(streams.Blob, controls)
	jet4LabelProps := parseJet4FormLabelProperties(streams.Blob, controls)
	jet4ComboBoxProps := parseJet4FormComboBoxProperties(streams.Blob, controls)
	jet4ButtonProps := parseJet4FormButtonProperties(streams.Blob, controls)
	jet4CheckBoxProps := parseJet4FormCheckBoxProperties(streams.Blob, controls)
	jet4RectangleProps := parseJet4FormRectangleProperties(streams.Blob, controls)
	jet4OptionGroupProps := parseJet4FormOptionGroupProperties(streams.Blob, controls)
	jet4OptionButtonProps := parseJet4FormOptionButtonProperties(streams.Blob, controls)
	jet4SubFormProps := parseJet4FormSubFormProperties(streams.Blob, controls)
	jet4TabControlProps := parseJet4FormTabControlProperties(streams.Blob, controls)
	jet4TabPageProps := parseJet4FormTabPageProperties(streams.Blob, controls)
	jet4SectionProps := parseJet4FormSectionProperties(streams.Blob, controls)
	jet4FormWidth, jet4Geometries := parseJet4FormGeometries(streams.Blob, controls)
	formProps = mergeFormProperties(formProps, jet4FormProps)
	if defaultView, ok := parseJet4FormDefaultView(streams.Blob); ok {
		formProps = mergeFormProperties(formProps, []FormProperty{{
			ID:        0x0093,
			Name:      FormPropertyIDToName(0x0093),
			ValueType: "Byte",
			Value:     strconv.Itoa(defaultView),
		}})
	}

	content := &FormContent{
		FormName:    streams.FormName,
		StorageID:   streams.StorageID,
		Width:       jet4FormWidth,
		DefaultView: formPropertyInt(formProps, 0x0093),
		Properties:  formProps,
		Controls:    make([]FormControlContent, 0, len(controls)+len(controlGroups)),
	}
	usedGroups := make([]bool, len(controlGroups))
	for i, control := range controls {
		controlName := control.Name
		if control.Type == "TextBox" && i < len(controlOffsets) {
			controlName = jet4ControlNameAt(streams.Blob, controlOffsets[i], control.Name)
		}
		parsed := FormControlContent{
			Name:       controlName,
			Type:       control.Type,
			TypeCode:   control.TypeCode,
			Index:      control.Index,
			BlobOffset: -1,
			Visible:    true,
		}
		if i < len(controlOffsets) {
			parsed.BlobOffset = controlOffsets[i]
		}
		groupIndex := findFormPropertyGroupByName(controlGroups, control.Name)
		if groupIndex < 0 && i < len(controlGroups) {
			groupIndex = i
		}
		if groupIndex >= 0 {
			parsed.Properties = controlGroups[groupIndex]
			usedGroups[groupIndex] = true
		}
		parsed.Properties = mergeFormProperties(parsed.Properties, jet4ControlProps[strings.ToLower(control.Name)])
		if numeric, ok := jet4NumericProps[strings.ToLower(control.Name)]; ok {
			parsed.Properties = mergeFormProperties(parsed.Properties, numeric.formProperties())
			parsed.Locked = numeric.Locked
			parsed.Underline = numeric.Underline
			parsed.TextAlign = accessTextAlignName(numeric.TextAlign)
			parsed.TextAlignValue = numeric.TextAlign
			parsed.TabIndex = numeric.TabIndex
			parsed.ScrollBars = numeric.ScrollBars
			parsed.BackStyle = int(numeric.BackStyle)
			parsed.BackColor = numeric.BackColor
			parsed.BackColorValue = numeric.BackColorValue
			parsed.ForeColor = numeric.ForeColor
			parsed.ForeColorValue = numeric.ForeColorValue
			parsed.BackGroundColor = numeric.BackColor
			if numeric.BackStyle == 0 {
				parsed.BackGroundColor = ""
			}
		}
		if geometry, ok := jet4Geometries[strings.ToLower(control.Name)]; ok {
			parsed.Left = geometry.Left
			parsed.Top = geometry.Top
			parsed.Width = geometry.Width
			parsed.Height = geometry.Height
			parsed.HasGeometry = true
		}
		if numeric, ok := jet4NumericProps[strings.ToLower(control.Name)]; ok && numeric.HasGeometry {
			parsed.Left = numeric.Geometry.Left
			parsed.Top = numeric.Geometry.Top
			parsed.Width = numeric.Geometry.Width
			parsed.Height = numeric.Geometry.Height
			parsed.HasGeometry = true
		}
		if label, ok := jet4LabelProps[strings.ToLower(control.Name)]; ok {
			parsed.Properties = mergeFormProperties(parsed.Properties, label.formProperties())
			parsed.TextAlign = accessTextAlignName(label.TextAlign)
			parsed.TextAlignValue = label.TextAlign
			parsed.FontSize = label.FontSize
			parsed.BackStyle = int(label.BackStyle)
			parsed.BackColor = label.BackColor
			parsed.BackColorValue = label.BackColorValue
			parsed.ForeColor = label.ForeColor
			parsed.ForeColorValue = label.ForeColorValue
			parsed.BackGroundColor = label.BackColor
			if label.BackStyle == 0 {
				parsed.BackGroundColor = ""
			}
			if label.HasGeometry {
				parsed.Left = label.Geometry.Left
				parsed.Top = label.Geometry.Top
				parsed.Width = label.Geometry.Width
				parsed.Height = label.Geometry.Height
				parsed.HasGeometry = true
			}
		}
		if combo, ok := jet4ComboBoxProps[strings.ToLower(control.Name)]; ok {
			parsed.Properties = mergeFormProperties(parsed.Properties, combo.formProperties())
			parsed.ColumnCount = combo.ColumnCount
			parsed.ListRows = combo.ListRows
			parsed.ListWidth = combo.ListWidth
			parsed.BoundColumn = combo.BoundColumn
			parsed.BackStyle = int(combo.BackStyle)
			parsed.TextAlign = accessTextAlignName(combo.TextAlign)
			parsed.TextAlignValue = combo.TextAlign
			parsed.TabIndex = combo.TabIndex
			parsed.Locked = combo.Locked
			parsed.Visible = combo.Visible
			if combo.HasGeometry {
				parsed.Left = combo.Geometry.Left
				parsed.Top = combo.Geometry.Top
				parsed.Width = combo.Geometry.Width
				parsed.Height = combo.Geometry.Height
				parsed.HasGeometry = true
			}
		}
		if button, ok := jet4ButtonProps[strings.ToLower(control.Name)]; ok {
			parsed.Properties = mergeFormProperties(parsed.Properties, button.formProperties())
			parsed.TabIndex = button.TabIndex
			parsed.BackStyle = int(button.BackStyle)
			parsed.BackColor = button.BackColor
			parsed.BackColorValue = button.BackColorValue
			parsed.BackGroundColor = button.BackColor
			parsed.Picture = formPropertyText(parsed.Properties, 0x0007)
			if parsed.Picture == "" {
				parsed.Picture = "(无)"
			}
			if button.HasGeometry {
				parsed.Left = button.Geometry.Left
				parsed.Top = button.Geometry.Top
				parsed.Width = button.Geometry.Width
				parsed.Height = button.Geometry.Height
				parsed.HasGeometry = true
			}
		}
		if checkBox, ok := jet4CheckBoxProps[strings.ToLower(control.Name)]; ok {
			parsed.Properties = mergeFormProperties(parsed.Properties, checkBox.formProperties())
			parsed.TabIndex = checkBox.TabIndex
			parsed.Locked = checkBox.Locked
			parsed.Visible = checkBox.Visible
			if checkBox.HasGeometry {
				parsed.Left = checkBox.Geometry.Left
				parsed.Top = checkBox.Geometry.Top
				parsed.Width = checkBox.Geometry.Width
				parsed.Height = checkBox.Geometry.Height
				parsed.HasGeometry = true
			}
		}
		if rectangle, ok := jet4RectangleProps[strings.ToLower(control.Name)]; ok {
			parsed.Properties = mergeFormProperties(parsed.Properties, rectangle.formProperties())
			parsed.SpecialEffect = int(rectangle.SpecialEffect)
			parsed.BackStyle = int(rectangle.BackStyle)
			parsed.BackColor = rectangle.BackColor
			parsed.BackColorValue = rectangle.BackColorValue
			parsed.BackGroundColor = rectangle.BackColor
			parsed.BorderStyle = int(rectangle.BorderStyle)
			parsed.BorderWidth = int(rectangle.BorderWidth)
			parsed.BorderColor = rectangle.BorderColor
			parsed.BorderColorValue = rectangle.BorderColorValue
			parsed.Visible = rectangle.Visible
			if rectangle.HasGeometry {
				parsed.Left = rectangle.Geometry.Left
				parsed.Top = rectangle.Geometry.Top
				parsed.Width = rectangle.Geometry.Width
				parsed.Height = rectangle.Geometry.Height
				parsed.HasGeometry = true
			}
		}
		if optionGroup, ok := jet4OptionGroupProps[strings.ToLower(control.Name)]; ok {
			parsed.Properties = mergeFormProperties(parsed.Properties, optionGroup.formProperties())
			parsed.SpecialEffect = int(optionGroup.SpecialEffect)
			parsed.BackStyle = int(optionGroup.BackStyle)
			parsed.BorderStyle = int(optionGroup.BorderStyle)
			parsed.BorderWidth = int(optionGroup.BorderWidth)
			parsed.TabIndex = optionGroup.TabIndex
			parsed.Locked = optionGroup.Locked
			parsed.Visible = optionGroup.Visible
			if optionGroup.HasBackColor {
				parsed.BackColor = optionGroup.BackColor
				parsed.BackColorValue = optionGroup.BackColorValue
				parsed.BackGroundColor = optionGroup.BackColor
				if optionGroup.BackStyle == 0 {
					parsed.BackGroundColor = ""
				}
			}
			if optionGroup.HasBorderColor {
				parsed.BorderColor = optionGroup.BorderColor
				parsed.BorderColorValue = optionGroup.BorderColorValue
			}
			if optionGroup.HasGeometry {
				parsed.Left = optionGroup.Geometry.Left
				parsed.Top = optionGroup.Geometry.Top
				parsed.Width = optionGroup.Geometry.Width
				parsed.Height = optionGroup.Geometry.Height
				parsed.HasGeometry = true
			}
		}
		if optionButton, ok := jet4OptionButtonProps[strings.ToLower(control.Name)]; ok {
			parsed.Properties = mergeFormProperties(parsed.Properties, optionButton.formProperties())
			parsed.OptionValue = optionButton.OptionValue
			parsed.TabIndex = optionButton.TabIndex
			parsed.Locked = optionButton.Locked
			parsed.Visible = optionButton.Visible
			if optionButton.HasSpecialEffect {
				parsed.SpecialEffect = int(optionButton.SpecialEffect)
			}
			if optionButton.HasBorderStyle {
				parsed.BorderStyle = int(optionButton.BorderStyle)
			}
			if optionButton.HasBorderWidth {
				parsed.BorderWidth = int(optionButton.BorderWidth)
			}
			if optionButton.HasBorderColor {
				parsed.BorderColor = optionButton.BorderColor
				parsed.BorderColorValue = optionButton.BorderColorValue
			}
			if optionButton.HasGeometry {
				parsed.Left = optionButton.Geometry.Left
				parsed.Top = optionButton.Geometry.Top
				parsed.Width = optionButton.Geometry.Width
				parsed.Height = optionButton.Geometry.Height
				parsed.HasGeometry = true
			}
		}
		if subForm, ok := jet4SubFormProps[strings.ToLower(control.Name)]; ok {
			parsed.Properties = mergeFormProperties(parsed.Properties, subForm.formProperties())
			parsed.TabIndex = subForm.TabIndex
			parsed.Locked = subForm.Locked
			parsed.CanShrink = subForm.CanShrink
			parsed.Visible = subForm.Visible
			if subForm.HasGeometry {
				parsed.Left = subForm.Geometry.Left
				parsed.Top = subForm.Geometry.Top
				parsed.Width = subForm.Geometry.Width
				parsed.Height = subForm.Geometry.Height
				parsed.HasGeometry = true
			}
		}
		if tabControl, ok := jet4TabControlProps[strings.ToLower(control.Name)]; ok {
			parsed.Properties = mergeFormProperties(parsed.Properties, tabControl.formProperties())
			parsed.FontSize = tabControl.FontSize
			parsed.FontWeight = tabControl.FontWeight
			parsed.Visible = tabControl.Visible
			if tabControl.HasGeometry {
				parsed.Left = tabControl.Geometry.Left
				parsed.Top = tabControl.Geometry.Top
				parsed.Width = tabControl.Geometry.Width
				parsed.Height = tabControl.Geometry.Height
				parsed.HasGeometry = true
			}
		}
		if tabPage, ok := jet4TabPageProps[strings.ToLower(control.Name)]; ok {
			parsed.Properties = mergeFormProperties(parsed.Properties, tabPage.formProperties())
			parsed.PageIndex = tabPage.PageIndex
			parsed.Visible = tabPage.Visible
			if tabPage.HasGeometry {
				parsed.Left = tabPage.Geometry.Left
				parsed.Top = tabPage.Geometry.Top
				parsed.Width = tabPage.Geometry.Width
				parsed.Height = tabPage.Geometry.Height
				parsed.HasGeometry = true
			}
		}
		if section, ok := jet4SectionProps[strings.ToLower(control.Name)]; ok {
			parsed.Properties = mergeFormProperties(parsed.Properties, section.formProperties())
			parsed.Height = section.Height
			parsed.BackColor = section.BackColor
			parsed.BackColorValue = section.BackColorValue
			parsed.BackGroundColor = section.BackColor
			parsed.SpecialEffect = int(section.SpecialEffect)
			parsed.Visible = section.Visible
			parsed.EventProcPrefix = section.EventProcPrefix
		}
		parsed.Caption = formPropertyText(parsed.Properties, 0x0011)
		if control.Type == "Label" {
			if caption := formPropertyText(jet4ControlProps[strings.ToLower(control.Name)], 0x0011); caption != "" {
				// 紧凑 Label Caption 保留 Access 原生尾部空格，Blob 通用属性组可能已规范化。
				parsed.Caption = caption
			}
		}
		parsed.ControlSource = formPropertyText(parsed.Properties, 0x001B)
		parsed.SourceObject = formPropertyText(parsed.Properties, 0x0084)
		parsed.LinkChildFields = formPropertyText(parsed.Properties, 0x0031)
		parsed.LinkMasterFields = formPropertyText(parsed.Properties, 0x0032)
		parsed.EventProcPrefix = formPropertyText(parsed.Properties, 0x0016)
		parsed.Format = formPropertyText(parsed.Properties, 0x0026)
		parsed.Tag = formPropertyText(parsed.Properties, 0x010A)
		parsed.FontName = formPropertyTextAny(parsed.Properties, 0x0022, 0x00A0)
		parsed.StatusBarText = formPropertyText(parsed.Properties, 0x0087)
		parsed.ControlTipText = formPropertyText(parsed.Properties, 0x013D)
		parsed.OnClick = formPropertyText(parsed.Properties, 0x007E)
		parsed.RowSourceType = formPropertyText(parsed.Properties, 0x005D)
		parsed.RowSource = formPropertyText(parsed.Properties, 0x005B)
		parsed.ColumnWidths = formPropertyText(parsed.Properties, 0x0012)
		if control.Type == "ComboBox" && parsed.ColumnWidths != "" {
			parsed.ColumnCount = len(strings.Split(parsed.ColumnWidths, ";"))
			parsed.Properties = mergeFormProperties(parsed.Properties, []FormProperty{{
				ID:        0x0046,
				Name:      FormPropertyIDToName(0x0046),
				ValueType: "Short",
				Value:     strconv.Itoa(parsed.ColumnCount),
			}})
		}
		content.Controls = append(content.Controls, parsed)
	}
	for i, group := range controlGroups {
		if usedGroups[i] {
			continue
		}
		name := formPropertyText(group, 0x0014)
		if name == "" {
			name = fmt.Sprintf("Control_%d", len(content.Controls))
		}
		content.Controls = append(content.Controls, FormControlContent{
			Name:          name,
			Type:          "Unknown",
			BlobOffset:    -1,
			Caption:       formPropertyText(group, 0x0011),
			ControlSource: formPropertyText(group, 0x001B),
			Properties:    group,
		})
	}
	content.RecordSource = formPropertyText(content.Properties, 0x009C)
	content.Caption = formPropertyText(content.Properties, 0x0011)
	content.Sections = assignFormControlSections(content.Controls)
	for _, section := range content.Sections {
		if section.Type != "Detail" {
			continue
		}
		// AccessExport 的 Form.Height 与 Form.BackGroundColor 均来自主体 Detail Section。
		content.Height = section.Height
		content.BackColor = section.BackColor
		content.BackColorValue = section.BackColorValue
		content.BackGroundColor = section.BackGroundColor
		break
	}
	return content, nil
}

// assignFormControlSections 根据 Blob 中的分区标记给控件分组。
// TypeInfo 允许把后创建的控件追加到目录末尾，即使其实际位于 FormFooter 之前；
// Blob 的物理顺序才稳定保存“分区标记，随后是该分区控件”的结构。
func assignFormControlSections(controls []FormControlContent) []FormSectionContent {
	sections := make([]FormSectionContent, 0, 3)
	currentSection := -1
	indices := make([]int, len(controls))
	allOffsetsKnown := true
	for i := range controls {
		indices[i] = i
		if controls[i].BlobOffset < 0 {
			allOffsetsKnown = false
		}
	}
	if allOffsetsKnown {
		sort.SliceStable(indices, func(i, j int) bool {
			return controls[indices[i]].BlobOffset < controls[indices[j]].BlobOffset
		})
	}

	for _, i := range indices {
		control := &controls[i]
		if isFormSectionTypeCode(control.TypeCode) {
			control.Section = control.Type
			control.IsSection = true
			sections = append(sections, FormSectionContent{
				Name:            control.Name,
				Type:            control.Type,
				TypeCode:        control.TypeCode,
				Index:           control.Index,
				Height:          control.Height,
				BackColor:       control.BackColor,
				BackColorValue:  control.BackColorValue,
				BackGroundColor: control.BackGroundColor,
				SpecialEffect:   control.SpecialEffect,
				Visible:         control.Visible,
				EventProcPrefix: control.EventProcPrefix,
				Tag:             control.Tag,
				Properties:      control.Properties,
				Controls:        make([]FormControlContent, 0),
			})
			currentSection = len(sections) - 1
			continue
		}

		if currentSection < 0 {
			// 极少数损坏或旧格式 TypeInfo 可能缺少 Detail 标记；仍保证控件可归类。
			sections = append(sections, FormSectionContent{
				Name:     "Detail",
				Type:     "Detail",
				TypeCode: 0x1898,
				Controls: make([]FormControlContent, 0),
			})
			currentSection = 0
		}
		control.Section = sections[currentSection].Type
		sections[currentSection].Controls = append(sections[currentSection].Controls, *control)
	}

	return sections
}

func isFormSectionTypeCode(typeCode uint16) bool {
	switch typeCode {
	case 0x1898, 0x1998, // Detail
		0x1899, 0x189A, // FormHeader, FormFooter
		0x1999, 0x199A, // ReportHeader, ReportFooter
		0x199D, 0x199E, // GroupHeader, GroupFooter
		0x1F9B, 0x1F9C: // PageHeader, PageFooter
		return true
	default:
		return false
	}
}

// propertyItemsFromC 把 C 侧属性项数组深拷贝为 Go 切片。
func propertyItemsFromC(ptr *C.mdbgo_property_item_t, count int) []PropertyItem {
	if ptr == nil || count <= 0 {
		return nil
	}

	out := make([]PropertyItem, count)
	items := unsafe.Slice((*C.mdbgo_property_item_t)(unsafe.Pointer(ptr)), count)
	for i := 0; i < count; i++ {
		out[i] = PropertyItem{
			Key:   C.GoString(items[i].key),
			Value: C.GoString(items[i].value),
		}
	}
	return out
}

// cBytesToGo 把 C 的字节缓冲区深拷贝到 Go。
func cBytesToGo(ptr *C.uchar, n int) []byte {
	if ptr == nil || n <= 0 {
		return nil
	}
	src := unsafe.Slice((*byte)(unsafe.Pointer(ptr)), n)
	out := make([]byte, n)
	copy(out, src)
	return out
}

func (db *DB) listAccessObjectIDs() ([]int, error) {
	errBuf := make([]byte, cErrBufSize)
	var raw C.mdbgo_int_array_t
	rc := C.mdbgo_list_access_object_ids(
		db.ptr,
		&raw,
		(*C.char)(unsafe.Pointer(&errBuf[0])),
		C.size_t(len(errBuf)),
	)
	if rc != 0 {
		return nil, errors.New(cStringFromBuf(errBuf))
	}
	defer C.mdbgo_free_int_array(&raw)

	n := int(raw.count)
	if n <= 0 || raw.values == nil {
		return nil, nil
	}
	cVals := unsafe.Slice((*C.int)(unsafe.Pointer(raw.values)), n)
	out := make([]int, n)
	for i := 0; i < n; i++ {
		out[i] = int(cVals[i])
	}
	return out, nil
}
