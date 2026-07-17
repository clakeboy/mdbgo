package mdbgo

import (
	"sort"
	"strconv"
	"strings"
)

func parseJet4TextBoxTextProperties(control FormControlInfo, fields []jet4TaggedTextField) []FormProperty {
	var props []FormProperty
	for _, field := range fields {
		switch field.Tag {
		case 0xDD:
			if source, ok := normalizeControlSource(field.Value, control.Name); ok {
				props = mergeFormProperties(props, []FormProperty{newTextFormProperty(0x001B, source)})
			}
		case 0xDE:
			props = mergeFormProperties(props, []FormProperty{newTextFormProperty(0x0026, field.Value)})
		case 0xDF:
			props = mergeFormProperties(props, []FormProperty{newTextFormProperty(0x0087, field.Value)})
		case 0xE8:
			props = mergeFormProperties(props, []FormProperty{newTextFormProperty(0x0022, field.Value)})
		case 0xF4:
			props = mergeFormProperties(props, []FormProperty{newTextFormProperty(0x010A, field.Value)})
		}
	}
	return props
}

type jet4FormNumericProperties struct {
	TextAlign      byte
	TabIndex       int
	HasTabIndex    bool
	BackStyle      byte
	BackColor      string
	BackColorValue uint32
	ForeColor      string
	ForeColorValue uint32
	Geometry       formControlGeometry
	HasGeometry    bool
}

func (props jet4FormNumericProperties) formProperties() []FormProperty {
	return []FormProperty{
		{ID: 0x001D, Name: FormPropertyIDToName(0x001D), ValueType: "Byte", Value: strconv.Itoa(int(props.BackStyle))},
		{ID: 0x0088, Name: FormPropertyIDToName(0x0088), ValueType: "Byte", Value: strconv.Itoa(int(props.TextAlign))},
		{ID: 0x0105, Name: FormPropertyIDToName(0x0105), ValueType: "Short", Value: strconv.Itoa(props.TabIndex)},
		{ID: 0x001C, Name: FormPropertyIDToName(0x001C), ValueType: "Color", Value: props.BackColor},
		{ID: 0x00CC, Name: FormPropertyIDToName(0x00CC), ValueType: "Color", Value: props.ForeColor},
	}
}

// parseJet4FormNumericProperties 解析 TextBox 的紧凑数值属性。
// Jet4 Blob 中文本目录和紧凑数值记录是两套顺序：文本块尾部附带的数值记录
// 不一定属于相邻 TypeInfo 项。因此先用 TextBox 特有的 BackColor/ForeColor 组合
// 筛选物理记录，再按物理顺序与 TextBox 文本块配对。
func parseJet4FormNumericProperties(data []byte, controls []FormControlInfo) map[string]jet4FormNumericProperties {
	result := make(map[string]jet4FormNumericProperties)
	if len(data) < 8 || len(controls) == 0 || le16(data) > 0x0014 {
		return result
	}

	offsets := orderedFormControlOffsets(data, controls)
	type physicalTextBox struct {
		offset int
		name   string
	}
	textBoxes := make([]physicalTextBox, 0)
	for i, offset := range offsets {
		if offset < 0 || controls[i].Type != "TextBox" {
			continue
		}
		textBoxes = append(textBoxes, physicalTextBox{offset: offset, name: controls[i].Name})
	}
	sort.Slice(textBoxes, func(i, j int) bool { return textBoxes[i].offset < textBoxes[j].offset })

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
	numericRecords := make([]jet4FormNumericProperties, 0, len(textBoxes))
	for _, block := range blocks {
		tail := jet4ControlNumericTailForType(block.block, block.name, block.controlType)
		props, ok := parseJet4TextBoxNumericTail(tail)
		if ok {
			numericRecords = append(numericRecords, props)
		}
	}

	previousTabIndex := -1
	previousOffset := -1
	for i := 0; i < len(textBoxes) && i < len(numericRecords); i++ {
		props := numericRecords[i]
		if previousTabIndex >= 0 {
			if !props.HasTabIndex {
				if hasJet4ControlTypeBetween(controls, offsets, previousOffset, textBoxes[i].offset, "TabPage") {
					props.TabIndex = 0
				} else {
					props.TabIndex = previousTabIndex + 1
				}
			} else if props.TabIndex > previousTabIndex+1 &&
				!hasJet4FocusableControlBetween(controls, offsets, previousOffset, textBoxes[i].offset) {
				// Access 会压缩已删除/不可聚焦控件留下的 TabIndex 空洞。
				props.TabIndex = previousTabIndex + 1
			}
		}
		result[strings.ToLower(textBoxes[i].name)] = props
		previousTabIndex = props.TabIndex
		previousOffset = textBoxes[i].offset
	}
	return result
}

func hasJet4ControlTypeBetween(controls []FormControlInfo, offsets []int, start, end int, controlType string) bool {
	for i, offset := range offsets {
		if offset > start && offset < end && controls[i].Type == controlType {
			return true
		}
	}
	return false
}

func hasJet4FocusableControlBetween(controls []FormControlInfo, offsets []int, start, end int) bool {
	for i, offset := range offsets {
		if offset <= start || offset >= end {
			continue
		}
		switch controls[i].Type {
		case "Button", "ToggleButton", "CheckBox", "OptionButton", "ComboBox", "ListBox", "SubForm":
			return true
		}
	}
	return false
}

func parseJet4TextBoxNumericTail(tail []byte) (jet4FormNumericProperties, bool) {
	var result jet4FormNumericProperties
	if len(tail) < 10 {
		return result, false
	}

	colorPos := -1
	for pos := 0; pos+10 <= len(tail); pos++ {
		if tail[pos] == 0x9C && tail[pos+5] == 0x9F {
			colorPos = pos
			break
		}
	}
	if colorPos < 0 {
		return result, false
	}

	layoutPos := colorPos
	for pos := 0; pos+2 < colorPos; pos++ {
		if tail[pos] >= 0x60 && tail[pos] <= 0x63 {
			layoutPos = pos
			break
		}
	}
	result.BackStyle = 1 // Access TextBox 默认背景样式为 Normal。
	for pos := 0; pos+1 < layoutPos; pos++ {
		switch tail[pos] {
		case 0x3B:
			if tail[pos+1] <= 4 {
				result.TextAlign = tail[pos+1]
			}
		case 0x43:
			if tail[pos+1] <= 1 {
				result.BackStyle = tail[pos+1]
			}
		}
	}

	// Access TextBox 省略与默认值相同的尺寸项。
	result.Geometry.Width = 1440
	result.Geometry.Height = 288
	result.HasGeometry = true
	for pos := layoutPos; pos+2 < colorPos; {
		tag := tail[pos]
		value := int(le16(tail[pos+1:]))
		switch tag {
		case 0x60:
			result.Geometry.Left = value
		case 0x61:
			result.Geometry.Top = value
		case 0x62:
			result.Geometry.Width = value
		case 0x63:
			result.Geometry.Height = value
		case 0x6B:
			result.TabIndex = value
			result.HasTabIndex = true
		default:
			pos++
			continue
		}
		pos += 3
	}
	result.BackColorValue = le32(tail[colorPos+1:])
	result.ForeColorValue = le32(tail[colorPos+6:])
	result.BackColor = accessColorHex(result.BackColorValue)
	result.ForeColor = accessColorHex(result.ForeColorValue)
	return result, true
}
