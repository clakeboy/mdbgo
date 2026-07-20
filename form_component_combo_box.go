package mdbgo

import (
	"sort"
	"strconv"
	"strings"
)

func parseJet4ComboBoxTextProperties(control FormControlInfo, fields []jet4TaggedTextField) []FormProperty {
	var props []FormProperty
	for _, field := range fields {
		switch field.Tag {
		case 0xDD:
			if source, ok := normalizeControlSource(field.Value, control.Name); ok {
				props = mergeFormProperties(props, []FormProperty{newTextFormProperty(0x001B, source)})
			}
		case 0xDE:
			props = mergeFormProperties(props, []FormProperty{newTextFormProperty(0x005D, field.Value)})
		case 0xDF:
			props = mergeFormProperties(props, []FormProperty{newTextFormProperty(0x005B, field.Value)})
		case 0xE0:
			props = mergeFormProperties(props, []FormProperty{newTextFormProperty(0x0012, field.Value)})
		case 0xEA:
			props = mergeFormProperties(props, []FormProperty{newTextFormProperty(0x0022, field.Value)})
		case 0xF5:
			props = mergeFormProperties(props, []FormProperty{newTextFormProperty(0x010A, field.Value)})
		}
	}
	return props
}

type jet4ComboBoxNumericProperties struct {
	ColumnCount int
	ListRows    int
	ListWidth   int
	BoundColumn int
	TextAlign   byte
	TabIndex    int
	HasTabIndex bool
	Locked      bool
	Visible     bool
	Geometry    formControlGeometry
	HasGeometry bool
}

func (props jet4ComboBoxNumericProperties) formProperties() []FormProperty {
	result := []FormProperty{
		{ID: 0x000D, Name: FormPropertyIDToName(0x000D), ValueType: "Long", Value: strconv.Itoa(props.BoundColumn)},
		{ID: 0x0099, Name: FormPropertyIDToName(0x0099), ValueType: "Short", Value: strconv.Itoa(props.ListRows)},
		{ID: 0x009A, Name: FormPropertyIDToName(0x009A), ValueType: "Long", Value: strconv.Itoa(props.ListWidth)},
		{ID: 0x0038, Name: FormPropertyIDToName(0x0038), ValueType: "Bool", Value: strconv.FormatBool(props.Locked)},
		{ID: 0x0094, Name: FormPropertyIDToName(0x0094), ValueType: "Bool", Value: strconv.FormatBool(props.Visible)},
		{ID: 0x0088, Name: FormPropertyIDToName(0x0088), ValueType: "Byte", Value: strconv.Itoa(int(props.TextAlign))},
	}
	if props.ColumnCount > 0 {
		result = append(result, FormProperty{
			ID: 0x0046, Name: FormPropertyIDToName(0x0046), ValueType: "Short", Value: strconv.Itoa(props.ColumnCount),
		})
	}
	if props.HasTabIndex {
		result = append(result, FormProperty{
			ID: 0x0105, Name: FormPropertyIDToName(0x0105), ValueType: "Short", Value: strconv.Itoa(props.TabIndex),
		})
	}
	return result
}

// parseJet4FormComboBoxProperties 把 ComboBox 的紧凑数值记录按物理顺序配回控件。
// ComboBox 的记录不一定保存在自己的文本块尾部；其稳定特征是 0x60..0x66
// 连续保存 BoundColumn、ListRows、ListWidth 和四项 twips 几何，随后以 0x6E
// 保存 TabIndex。ColumnCount 不在这段固定布局中，调用方会从 ColumnWidths 推导。
func parseJet4FormComboBoxProperties(data []byte, controls []FormControlInfo) map[string]jet4ComboBoxNumericProperties {
	result := make(map[string]jet4ComboBoxNumericProperties)
	if len(data) < 8 || len(controls) == 0 || le16(data) > 0x0014 {
		return result
	}

	offsets := orderedFormControlOffsets(data, controls)
	type physicalComboBox struct {
		offset int
		name   string
	}
	comboBoxes := make([]physicalComboBox, 0)
	for i, offset := range offsets {
		if offset >= 0 && controls[i].Type == "ComboBox" {
			comboBoxes = append(comboBoxes, physicalComboBox{offset: offset, name: controls[i].Name})
		}
	}
	sort.Slice(comboBoxes, func(i, j int) bool { return comboBoxes[i].offset < comboBoxes[j].offset })

	type physicalControlBlock struct {
		offset      int
		name        string
		controlType string
		block       []byte
	}
	blocks := make([]physicalControlBlock, 0, len(controls))
	seenOffsets := make(map[int]bool, len(offsets))
	for i, offset := range offsets {
		if offset < 0 || seenOffsets[offset] {
			continue
		}
		seenOffsets[offset] = true
		block := jet4FormControlBlock(data, offsets, i)
		if len(block) == 0 {
			continue
		}
		blocks = append(blocks, physicalControlBlock{
			offset: offset, name: controls[i].Name, controlType: controls[i].Type, block: block,
		})
	}
	sort.Slice(blocks, func(i, j int) bool { return blocks[i].offset < blocks[j].offset })

	numericRecords := make([]jet4ComboBoxNumericProperties, 0, len(comboBoxes))
	for _, block := range blocks {
		tail := jet4ControlNumericTailForType(block.block, block.name, block.controlType)
		props, ok := parseJet4ComboBoxNumericTail(tail)
		if ok {
			numericRecords = append(numericRecords, props)
		}
	}
	for i := 0; i < len(comboBoxes) && i < len(numericRecords); i++ {
		result[strings.ToLower(comboBoxes[i].name)] = numericRecords[i]
	}
	return result
}

func parseJet4ComboBoxNumericTail(tail []byte) (jet4ComboBoxNumericProperties, bool) {
	result := jet4ComboBoxNumericProperties{
		ListRows:    8,
		BoundColumn: 1,
		Visible:     true,
	}
	if len(tail) < 12 {
		return result, false
	}

	typePos := -1
	for pos := 0; pos+2 <= len(tail) && pos < 12; pos++ {
		if tail[pos] == 0x6F && tail[pos+1] == 0x00 {
			typePos = pos
			break
		}
	}
	if typePos < 0 {
		return result, false
	}
	payloadPos := typePos + 2
	for pos := payloadPos; pos < len(tail) && tail[pos] != 0x60; pos++ {
		if tail[pos] == 0x04 {
			result.Locked = true
		}
		if tail[pos] == 0x39 && pos+1 < len(tail) && tail[pos+1] <= 4 {
			result.TextAlign = tail[pos+1]
		}
	}

	foundWidth := false
	foundHeight := false
	for pos := payloadPos; pos < len(tail); {
		tag := tail[pos]
		switch tag {
		case 0x60, 0x61, 0x62, 0x63, 0x64, 0x65, 0x66, 0x6E:
			if pos+3 > len(tail) {
				return result, false
			}
			value := int(le16(tail[pos+1:]))
			switch tag {
			case 0x60:
				result.ColumnCount = value
			case 0x61:
				result.ListRows = value
			case 0x62:
				result.ListWidth = value
			case 0x63:
				result.Geometry.Left = value
			case 0x64:
				result.Geometry.Top = value
			case 0x65:
				result.Geometry.Width = value
				foundWidth = true
			case 0x66:
				result.Geometry.Height = value
				foundHeight = true
			case 0x6E:
				result.TabIndex = value
				result.HasTabIndex = true
			}
			pos += 3
		case 0x9C:
			// ComboBox 在 0x9C 中以零基值保存 BoundColumn；Access COM
			// 返回一基值。记录前缀的 0x01 是其他标志，不能当成 BoundColumn。
			if pos+5 > len(tail) {
				return result, false
			}
			result.BoundColumn = int(le32(tail[pos+1:])) + 1
			pos += 5
		case 0x9D, 0xA0:
			// BackColor/ForeColor，不属于当前导出模型，但需跨过完整值。
			if pos+5 > len(tail) {
				return result, false
			}
			pos += 5
		case 0xBC:
			// 后续是 ColumnHeads/列格式文本及对象 GUID。
			pos = len(tail)
		case 0xDC:
			pos = len(tail)
		default:
			pos++
		}
	}
	// Jet4 会省略值为默认值的字段。ColumnCount 可由 ColumnWidths 推导，
	// TabIndex 缺省时为 0，Left/Top 缺省时同样为 0。
	if !foundWidth || !foundHeight ||
		result.ColumnCount < 0 || result.ColumnCount > 255 ||
		result.ListRows <= 0 || result.ListRows > 255 ||
		result.Geometry.Left > 32767 || result.Geometry.Top > 32767 ||
		result.Geometry.Width <= 0 || result.Geometry.Width > 32767 ||
		result.Geometry.Height <= 0 || result.Geometry.Height > 32767 {
		return result, false
	}
	result.HasGeometry = true
	return result, true
}
