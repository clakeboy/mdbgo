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
	if len(tail) < 21 {
		return result, false
	}

	layoutPos := -1
	for pos := 0; pos+21 <= len(tail); pos++ {
		matched := true
		for tag := byte(0x60); tag <= 0x66; tag++ {
			if tail[pos+int(tag-0x60)*3] != tag {
				matched = false
				break
			}
		}
		if matched {
			layoutPos = pos
			break
		}
	}
	if layoutPos < 0 {
		return result, false
	}

	values := make([]int, 7)
	for i := range values {
		values[i] = int(le16(tail[layoutPos+i*3+1:]))
	}
	if values[0] <= 0 || values[0] > 255 || values[1] <= 0 || values[1] > 255 ||
		values[3] > 32767 || values[4] > 32767 ||
		values[5] <= 0 || values[5] > 32767 || values[6] <= 0 || values[6] > 32767 {
		return result, false
	}

	result.BoundColumn = values[0]
	result.ListRows = values[1]
	result.ListWidth = values[2]
	result.Geometry = formControlGeometry{Left: values[3], Top: values[4], Width: values[5], Height: values[6]}
	result.HasGeometry = true

	for pos := layoutPos + 21; pos < len(tail); {
		switch tail[pos] {
		case 0x6E:
			if pos+3 > len(tail) {
				return result, false
			}
			result.TabIndex = int(le16(tail[pos+1:]))
			result.HasTabIndex = true
			pos += 3
		default:
			pos++
		}
	}
	if !result.HasTabIndex || result.BoundColumn <= 0 {
		return result, false
	}
	return result, true
}
