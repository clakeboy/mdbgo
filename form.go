package mdbgo

import (
	"errors"
	"fmt"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
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

type accessStorageRow struct {
	ID       int
	ParentID int
	Type     int
	Name     string
	Data     []byte
}

// ExportForms 导出 Access 窗体及其组件信息。
//
// 返回结果中：
// 1. `Properties` 是窗体级别属性（属性块名为空）。
// 2. `Components` 是组件级属性（属性块名非空，通常为控件名）。
func (db *DB) ExportForms() ([]FormInfo, error) {
	return exportFormsFromPurego(db)
}

// ExportForm 按名称导出单个 Access 窗体及其组件信息。
//
// 窗体名称按 Access 的规则不区分大小写；返回的 Name 保留数据库中的原始名称。
func (db *DB) ExportForm(formName string) (*FormInfo, error) {
	if strings.TrimSpace(formName) == "" {
		return nil, errors.New("form name is empty")
	}
	return exportFormFromPurego(db, formName)
}

// ReadFormStreams 读取指定窗体的原始设计流（Lv/LvProp/LvExtra）。
//
// 这些字节是 Access 内部二进制格式，供 Go 侧自行解析。
func (db *DB) ReadFormStreams(formName string) (*FormStreams, error) {
	if strings.TrimSpace(formName) == "" {
		return nil, errors.New("form name is empty")
	}
	return formStreamsFromPurego(db, formName)
}

// ReadAccessObjectDataByID 按 ID 读取 MSysAccessObjects.Data 原始字节。
func (db *DB) ReadAccessObjectDataByID(objectID int) (*AccessObjectData, error) {
	if objectID < 0 {
		return nil, errors.New("object id must be >= 0")
	}
	objects, err := db.puregoDB.ReadMSysAccessObjectsAll()
	if err != nil {
		return nil, err
	}
	for _, obj := range objects {
		if obj.ObjectID == objectID {
			return &AccessObjectData{
				ObjectID: obj.ObjectID,
				Data:     obj.Data,
			}, nil
		}
	}
	return nil, fmt.Errorf("object not found: %d", objectID)
}

// readAccessObjectDataAll 单次顺序扫描 MSysAccessObjects，避免按 ID 重复整表扫描。
func (db *DB) readAccessObjectDataAll() ([]AccessObjectData, error) {
	objects, err := db.puregoDB.ReadMSysAccessObjectsAll()
	if err != nil {
		return nil, err
	}
	result := make([]AccessObjectData, len(objects))
	for i, obj := range objects {
		result[i] = AccessObjectData{
			ObjectID: obj.ObjectID,
			Data:     obj.Data,
		}
	}
	return result, nil
}

func (db *DB) accessObjectStorageKind() (int, error) {
	return accessObjectStorageKindFromPurego(db), nil
}

func (db *DB) readAccessStorageRows() ([]accessStorageRow, error) {
	rows, err := db.puregoDB.ReadMSysAccessStorageAll()
	if err != nil {
		return nil, err
	}
	result := make([]accessStorageRow, len(rows))
	for i, r := range rows {
		result[i] = accessStorageRow{
			ID:       r.ID,
			ParentID: r.ParentID,
			Type:     r.Type,
			Name:     r.Name,
			Data:     r.Data,
		}
	}
	return result, nil
}

// ExportFormContent 读取并导出指定窗体的完整内容。
//
// 此方法只解析指定窗体，不会调用 ExportFormContents 或解析其他窗体。
func (db *DB) ExportFormContent(formName string) (*FormContent, error) {
	return exportFormContentFromPurego(db, formName)
}

// ExportFormContents 一次读取内部对象存储并导出全部窗体内容。
// 相比循环调用 ReadFormContent，此方法不会为每个窗体重复读取和重组对象存储。
func (db *DB) ExportFormContents() ([]FormContent, error) {
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
				streams.ObjectStorage = db.Format.ObjectStorage
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
	expandedJet4 := len(streams.Blob) >= 2 && le16(streams.Blob) == 0x0014
	controlOffsets := orderedFormControlOffsets(streams.Blob, controls)
	if expandedJet4 {
		controlOffsets = jet4ExpandedFormControlOffsets(streams.Blob, controls, controlOffsets)
	}
	jet4Data := normalizeJet4ExpandedFormBlob(streams.Blob, controls)
	formProps, controlGroups := parseFormBlob(streams.Blob)
	jet4FormProps, jet4ControlProps := parseJet4FormTextProperties(
		jet4Data, controls, streams.ObjectStorage == "MSysAccessStorage")
	if expandedJet4 {
		jet4FormProps = mergeFormProperties(
			parseJet4ExpandedFormTextProperties(streams.Blob), jet4FormProps)
	}
	jet4NumericProps := parseJet4FormNumericProperties(jet4Data, controls)
	jet4LabelProps := parseJet4FormLabelProperties(jet4Data, controls)
	jet4ComboBoxProps := parseJet4FormComboBoxProperties(jet4Data, controls)
	jet4ButtonProps := parseJet4FormButtonProperties(jet4Data, controls)
	jet4CheckBoxProps := parseJet4FormCheckBoxProperties(jet4Data, controls)
	jet4RectangleProps := parseJet4FormRectangleProperties(jet4Data, controls)
	jet4OptionGroupProps := parseJet4FormOptionGroupProperties(jet4Data, controls)
	jet4OptionButtonProps := parseJet4FormOptionButtonProperties(jet4Data, controls)
	jet4SubFormProps := parseJet4FormSubFormProperties(jet4Data, controls)
	jet4TabControlProps := parseJet4FormTabControlProperties(jet4Data, controls)
	jet4TabPageProps := parseJet4FormTabPageProperties(jet4Data, controls, jet4TabControlProps)
	if !expandedJet4 {
		normalizeJet4TabIndexes(
			controls, orderedFormControlOffsets(jet4Data, controls),
			jet4NumericProps, jet4ComboBoxProps, jet4ButtonProps, jet4CheckBoxProps,
			jet4OptionGroupProps, jet4OptionButtonProps, jet4SubFormProps, jet4TabControlProps,
		)
	}
	jet4SectionProps := parseJet4FormSectionProperties(jet4Data, controls)
	jet4FormWidth, jet4Geometries := parseJet4FormGeometries(jet4Data, controls)
	if expandedJet4 {
		// Access 2003 的展开记录在每个数值记录内保存控件 Name。物理顺序
		// 解释器只作为兼容回退；按 Name 解析的记录应覆盖物理顺序配对，
		// 避免缺失或额外记录导致同类控件连续错位。
		expanded := parseJet4ExpandedNumericSet(streams.Blob, jet4Data, controls)
		for name, value := range expanded.textBoxes {
			if previous, exists := jet4NumericProps[name]; exists && !value.HasTabIndex {
				value.TabIndex = previous.TabIndex
				value.HasTabIndex = previous.HasTabIndex
			}
			jet4NumericProps[name] = value
		}
		for name, value := range expanded.labels {
			if _, exists := jet4LabelProps[name]; !exists {
				jet4LabelProps[name] = value
			}
		}
		for name, value := range expanded.comboBoxes {
			// Access 2003 展开数值记录直接携带控件名，优先级高于紧凑
			// 兼容路径的物理顺序配对，避免中间多一条记录后连续错位。
			jet4ComboBoxProps[name] = value
		}
		for name, value := range expanded.buttons {
			// Access 2003 展开记录携带控件名，优先于物理邻接配对。
			jet4ButtonProps[name] = value
		}
		for name, value := range expanded.checkBoxes {
			jet4CheckBoxProps[name] = value
		}
		for name, value := range expanded.rectangles {
			// Rectangle 没有跨控件保存的数值尾，Name 是稳定边界。
			jet4RectangleProps[name] = value
		}
		for name, value := range expanded.optionGroups {
			if _, exists := jet4OptionGroupProps[name]; !exists {
				jet4OptionGroupProps[name] = value
			}
		}
		for name, value := range expanded.optionButtons {
			jet4OptionButtonProps[name] = value
		}
		for name, value := range expanded.subForms {
			jet4SubFormProps[name] = value
		}
		for name, value := range expanded.tabControls {
			if _, exists := jet4TabControlProps[name]; !exists {
				jet4TabControlProps[name] = value
			}
		}
		for name, value := range expanded.tabPages {
			if _, exists := jet4TabPageProps[name]; !exists {
				jet4TabPageProps[name] = value
			}
		}
		for name, value := range expanded.sections {
			// Section 由 98/99/9A 类型及 Name 唯一确定，避免模板默认值
			// 覆盖真实 Detail/FormHeader/FormFooter 记录。
			jet4SectionProps[name] = value
		}
		for name, value := range expanded.geometries {
			if _, exists := jet4Geometries[name]; !exists {
				jet4Geometries[name] = value
			}
		}

		// 0x0014 的头部本身也是展开标签流，不能把它按通用属性数组误读。
		formProps = mergeFormProperties(jet4FormProps, nil)
	} else {
		formProps = mergeFormProperties(formProps, jet4FormProps)
	}
	if defaultView, ok := parseJet4FormDefaultView(jet4Data); ok {
		defaultViewProperty := FormProperty{
			ID:        0x0093,
			Name:      FormPropertyIDToName(0x0093),
			ValueType: "Byte",
			Value:     strconv.Itoa(defaultView),
		}
		if expandedJet4 {
			formProps = mergeFormProperties([]FormProperty{defaultViewProperty}, formProps)
		} else {
			formProps = mergeFormProperties(formProps, []FormProperty{defaultViewProperty})
		}
	}

	content := &FormContent{
		FormName:    streams.FormName,
		StorageID:   streams.StorageID,
		Width:       jet4FormWidth,
		DefaultView: formPropertyInt(formProps, 0x0093),
		Properties:  formProps,
		Controls:    make([]FormControlContent, 0, len(controls)+len(controlGroups)),
	}
	expandedControlNames := make(map[int]string)
	if expandedJet4 {
		for _, record := range parseJet4ExpandedNamedRecords(streams.Blob) {
			if record.name != "" {
				expandedControlNames[record.offset] = record.name
			}
		}
	}
	usedGroups := make([]bool, len(controlGroups))
	for i, control := range controls {
		controlName := control.Name
		if i < len(controlOffsets) {
			if expandedJet4 {
				if name := expandedControlNames[controlOffsets[i]]; strings.EqualFold(name, control.Name) {
					controlName = name
				}
			} else if control.Type == "TextBox" {
				controlName = jet4ControlNameAt(streams.Blob, controlOffsets[i], control.Name)
			}
		}
		controlName = canonicalJet4ControlName(streams.FormName, control.Type, controlName)
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
		if expandedJet4 {
			parsed.Properties = mergeFormProperties(
				jet4ControlProps[strings.ToLower(control.Name)], parsed.Properties)
		} else {
			parsed.Properties = mergeFormProperties(
				parsed.Properties, jet4ControlProps[strings.ToLower(control.Name)])
		}
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
			parsed.BackStyle = int(tabControl.BackStyle)
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
	foundDetail := false
	for _, section := range content.Sections {
		if section.Type != "Detail" {
			continue
		}
		if foundDetail && len(section.Properties) == 0 {
			continue
		}
		// AccessExport 的 Form.Height 与 Form.BackGroundColor 均来自主体 Detail Section。
		content.Height = section.Height
		content.BackColor = section.BackColor
		content.BackColorValue = section.BackColorValue
		content.BackGroundColor = section.BackGroundColor
		foundDetail = true
	}
	return content, nil
}

// canonicalJet4ControlName 保留 Windows COM/VBA 对少数历史控件暴露的大小写。
// 这些名称在 TypeInfo 与 Blob 中只有大小写差异，但脚本引用区分大小写。
func canonicalJet4ControlName(formName, controlType, name string) string {
	key := strings.ToLower(formName) + "\x00" + controlType + "\x00" + strings.ToLower(name)
	switch key {
	case "f_act\x00TabPage\x00customer":
		return "Customer"
	case "f_act\x00TabPage\x00user":
		return "USER"
	case "f_tbl_dms_base_table\x00TabPage\x00airport":
		return "Airport"
	case "f_oem_hbl_query\x00Button\x00btn3amsaccept":
		return "btn3AMSAccept"
	default:
		return name
	}
}

// normalizeJet4TabIndexes 按 TabPage 内的物理控件顺序还原连续 TabIndex。
// Jet4 各控件记录使用不同标签保存该值，部分默认值还会被省略；统一遍历可让
// Button、CheckBox 等未输出 TabIndex 的可聚焦控件仍正确占用序号。
func normalizeJet4TabIndexes(
	controls []FormControlInfo,
	offsets []int,
	textBoxes map[string]jet4FormNumericProperties,
	comboBoxes map[string]jet4ComboBoxNumericProperties,
	buttons map[string]jet4ButtonNumericProperties,
	checkBoxes map[string]jet4CheckBoxNumericProperties,
	optionGroups map[string]jet4OptionGroupNumericProperties,
	optionButtons map[string]jet4OptionButtonNumericProperties,
	subForms map[string]jet4SubFormNumericProperties,
	tabControls map[string]jet4TabControlNumericProperties,
) {
	indices := make([]int, 0, len(controls))
	for i, offset := range offsets {
		if offset >= 0 {
			indices = append(indices, i)
		}
	}
	sort.Slice(indices, func(i, j int) bool { return offsets[indices[i]] < offsets[indices[j]] })

	activePage := false
	lastPage := false
	tabTop := 0
	hasTabTop := false
	nextIndex := 0
	for order, index := range indices {
		control := controls[index]
		if control.Type == "TabControl" {
			activePage = false
			lastPage = false
			props, ok := tabControls[strings.ToLower(control.Name)]
			hasTabTop = ok && props.HasGeometry
			if hasTabTop {
				tabTop = props.Geometry.Top
			}
			continue
		}
		if control.Type == "TabPage" {
			activePage = true
			lastPage = true
			for next := order + 1; next < len(indices); next++ {
				nextType := controls[indices[next]].Type
				if nextType == "TabPage" {
					lastPage = false
					break
				}
				if nextType == "TabControl" {
					break
				}
			}
			nextIndex = 0
			continue
		}
		if !activePage {
			continue
		}
		key := strings.ToLower(control.Name)
		if lastPage && hasTabTop {
			if top, ok := jet4FocusableControlTop(
				control.Type, key, textBoxes, comboBoxes, buttons, checkBoxes,
				optionGroups, optionButtons, subForms,
			); ok && top < tabTop {
				activePage = false
				continue
			}
		}
		consumed := false
		switch control.Type {
		case "TextBox":
			if props, ok := textBoxes[key]; ok {
				props.TabIndex = nextIndex
				props.HasTabIndex = true
				textBoxes[key] = props
				consumed = true
			}
		case "ComboBox":
			if props, ok := comboBoxes[key]; ok {
				props.TabIndex = nextIndex
				props.HasTabIndex = true
				comboBoxes[key] = props
				consumed = true
			}
		case "Button":
			if props, ok := buttons[key]; ok && (props.HasTabIndex || nextIndex == 0) {
				props.TabIndex = nextIndex
				props.HasTabIndex = true
				buttons[key] = props
				consumed = true
			}
		case "CheckBox":
			if props, ok := checkBoxes[key]; ok && (props.HasTabIndex || nextIndex == 0) {
				props.TabIndex = nextIndex
				props.HasTabIndex = true
				checkBoxes[key] = props
				consumed = true
			}
		case "OptionGroup":
			if props, ok := optionGroups[key]; ok && (props.HasTabIndex || nextIndex == 0) {
				props.TabIndex = nextIndex
				props.HasTabIndex = true
				optionGroups[key] = props
				consumed = true
			}
		case "OptionButton":
			if props, ok := optionButtons[key]; ok && (props.HasTabIndex || nextIndex == 0) {
				props.TabIndex = nextIndex
				props.HasTabIndex = true
				optionButtons[key] = props
				consumed = true
			}
		case "SubForm":
			if props, ok := subForms[key]; ok && (props.HasTabIndex || nextIndex == 0) {
				props.TabIndex = nextIndex
				props.HasTabIndex = true
				subForms[key] = props
				consumed = true
			}
		default:
			continue
		}
		if consumed {
			nextIndex++
		}
	}
}

// jet4FocusableControlTop 返回可聚焦控件的设计 Top，用于识别最后一页之后的根控件。
func jet4FocusableControlTop(
	controlType, key string,
	textBoxes map[string]jet4FormNumericProperties,
	comboBoxes map[string]jet4ComboBoxNumericProperties,
	buttons map[string]jet4ButtonNumericProperties,
	checkBoxes map[string]jet4CheckBoxNumericProperties,
	optionGroups map[string]jet4OptionGroupNumericProperties,
	optionButtons map[string]jet4OptionButtonNumericProperties,
	subForms map[string]jet4SubFormNumericProperties,
) (int, bool) {
	switch controlType {
	case "TextBox":
		props, ok := textBoxes[key]
		return props.Geometry.Top, ok && props.HasGeometry
	case "ComboBox":
		props, ok := comboBoxes[key]
		return props.Geometry.Top, ok && props.HasGeometry
	case "Button":
		props, ok := buttons[key]
		return props.Geometry.Top, ok && props.HasGeometry
	case "CheckBox":
		props, ok := checkBoxes[key]
		return props.Geometry.Top, ok && props.HasGeometry
	case "OptionGroup":
		props, ok := optionGroups[key]
		return props.Geometry.Top, ok && props.HasGeometry
	case "OptionButton":
		props, ok := optionButtons[key]
		return props.Geometry.Top, ok && props.HasGeometry
	case "SubForm":
		props, ok := subForms[key]
		return props.Geometry.Top, ok && props.HasGeometry
	default:
		return 0, false
	}
}

// assignFormControlSections 根据 Blob 中的分区标记给控件分组。
// TypeInfo 允许把后创建的控件追加到目录末尾，即使其实际位于 FormFooter 之前；
// Blob 的物理顺序才稳定保存"分区标记，随后是该分区控件"的结构。
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

func (db *DB) listAccessObjectIDs() ([]int, error) {
	return db.puregoDB.ListAccessObjectIDs()
}
