package mdbgo

import (
	"sort"
	"strconv"
	"strings"
)

// parseJet4OptionGroupTextProperties 解析 OptionGroup 的字符串属性。
// Jet4 紧凑文本标签 0xDD 对应 Access _OptionGroup.ControlSource (DispId 27)。
func parseJet4OptionGroupTextProperties(control FormControlInfo, fields []jet4TaggedTextField) []FormProperty {
	var props []FormProperty
	for _, field := range fields {
		if field.Tag != 0xDD {
			continue
		}
		if source, ok := normalizeControlSource(field.Value, control.Name); ok {
			props = mergeFormProperties(props, []FormProperty{newTextFormProperty(0x001B, source)})
		}
	}
	return props
}

type jet4OptionGroupNumericProperties struct {
	SpecialEffect    byte
	BackStyle        byte
	BorderStyle      byte
	BorderWidth      byte
	BackColor        string
	BackColorValue   uint32
	HasBackColor     bool
	BorderColor      string
	BorderColorValue uint32
	HasBorderColor   bool
	TabIndex         int
	HasTabIndex      bool
	Locked           bool
	Visible          bool
	Geometry         formControlGeometry
	HasGeometry      bool
}

func (props jet4OptionGroupNumericProperties) formProperties() []FormProperty {
	result := []FormProperty{
		{ID: 0x0004, Name: FormPropertyIDToName(0x0004), ValueType: "Byte", Value: strconv.Itoa(int(props.SpecialEffect))},
		{ID: 0x001D, Name: FormPropertyIDToName(0x001D), ValueType: "Byte", Value: strconv.Itoa(int(props.BackStyle))},
		{ID: 0x0009, Name: FormPropertyIDToName(0x0009), ValueType: "Byte", Value: strconv.Itoa(int(props.BorderStyle))},
		{ID: 0x000A, Name: FormPropertyIDToName(0x000A), ValueType: "Byte", Value: strconv.Itoa(int(props.BorderWidth))},
		{ID: 0x0038, Name: FormPropertyIDToName(0x0038), ValueType: "Bool", Value: strconv.FormatBool(props.Locked)},
		{ID: 0x0094, Name: FormPropertyIDToName(0x0094), ValueType: "Bool", Value: strconv.FormatBool(props.Visible)},
	}
	if props.HasBackColor {
		result = append(result, FormProperty{
			ID: 0x001C, Name: FormPropertyIDToName(0x001C), ValueType: "Color", Value: props.BackColor,
		})
	}
	if props.HasBorderColor {
		result = append(result, FormProperty{
			ID: 0x0008, Name: FormPropertyIDToName(0x0008), ValueType: "Color", Value: props.BorderColor,
		})
	}
	if props.HasTabIndex {
		result = append(result, FormProperty{
			ID: 0x0105, Name: FormPropertyIDToName(0x0105), ValueType: "Short", Value: strconv.Itoa(props.TabIndex),
		})
	}
	return result
}

// parseJet4FormOptionGroupProperties 按物理顺序把 FD 6B OptionGroup 数值记录配回控件。
// Access 有时把一个控件的数值记录放在相邻控件文本块的尾部，因此不能只检查
// OptionGroup 自己的文本块；这里扫描所有物理块后，再按出现顺序与 OptionGroup 配对。
func parseJet4FormOptionGroupProperties(data []byte, controls []FormControlInfo) map[string]jet4OptionGroupNumericProperties {
	result := make(map[string]jet4OptionGroupNumericProperties)
	if len(data) < 8 || len(controls) == 0 || le16(data) > 0x0014 {
		return result
	}

	offsets := orderedFormControlOffsets(data, controls)
	type physicalOptionGroup struct {
		offset int
		name   string
	}
	optionGroups := make([]physicalOptionGroup, 0)
	for i, offset := range offsets {
		if offset >= 0 && controls[i].Type == "OptionGroup" {
			optionGroups = append(optionGroups, physicalOptionGroup{offset: offset, name: controls[i].Name})
		}
	}
	sort.Slice(optionGroups, func(i, j int) bool { return optionGroups[i].offset < optionGroups[j].offset })

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

	numericRecords := make([]jet4OptionGroupNumericProperties, 0, len(optionGroups))
	for _, block := range blocks {
		tail := jet4ControlNumericTailForType(block.block, block.name, block.controlType)
		props, ok := parseJet4OptionGroupNumericTail(tail)
		if ok {
			numericRecords = append(numericRecords, props)
		}
	}
	for i := 0; i < len(optionGroups) && i < len(numericRecords); i++ {
		result[strings.ToLower(optionGroups[i].name)] = numericRecords[i]
	}
	return result
}

func parseJet4OptionGroupNumericTail(tail []byte) (jet4OptionGroupNumericProperties, bool) {
	result := jet4OptionGroupNumericProperties{
		BorderStyle: 1,
		BorderWidth: 1,
		Visible:     true,
	}
	if len(tail) < 12 {
		return result, false
	}

	recordPos := -1
	for pos := 0; pos+3 <= len(tail) && pos < 12; pos++ {
		if tail[pos] == 0xFD && tail[pos+1] == 0x6B && tail[pos+2] == 0x00 {
			recordPos = pos
			break
		}
	}
	if recordPos < 0 {
		return result, false
	}

	foundWidth := false
	foundHeight := false
	for pos := recordPos + 3; pos < len(tail); {
		tag := tail[pos]
		switch tag {
		case 0x31, 0x32, 0x33, 0x34:
			if pos+2 > len(tail) {
				return result, false
			}
			value := tail[pos+1]
			switch tag {
			case 0x31:
				result.SpecialEffect = value
			case 0x32:
				result.BackStyle = value
			case 0x33:
				result.BorderStyle = value
			case 0x34:
				result.BorderWidth = value
			}
			pos += 2
		case 0x60, 0x61, 0x62, 0x63, 0x69:
			if pos+3 > len(tail) {
				return result, false
			}
			value := int(le16(tail[pos+1:]))
			switch tag {
			case 0x60:
				result.Geometry.Left = value
			case 0x61:
				result.Geometry.Top = value
			case 0x62:
				result.Geometry.Width = value
				foundWidth = true
			case 0x63:
				result.Geometry.Height = value
				foundHeight = true
			case 0x69:
				result.TabIndex = value
				result.HasTabIndex = true
			}
			pos += 3
		case 0x9C, 0x9D:
			if pos+5 > len(tail) {
				return result, false
			}
			value := le32(tail[pos+1:])
			if tag == 0x9C {
				result.BackColorValue = value
				result.BackColor = accessColorHex(value)
				result.HasBackColor = true
			} else {
				result.BorderColorValue = value
				result.BorderColor = accessColorHex(value)
				result.HasBorderColor = true
			}
			pos += 5
		default:
			pos++
		}
	}
	if !foundWidth || !foundHeight || result.SpecialEffect > 3 || result.BackStyle > 1 ||
		result.BorderStyle > 1 || result.Geometry.Left > 32767 || result.Geometry.Top > 32767 ||
		result.Geometry.Width <= 0 || result.Geometry.Width > 32767 ||
		result.Geometry.Height <= 0 || result.Geometry.Height > 32767 {
		return result, false
	}
	result.HasGeometry = true
	return result, true
}
