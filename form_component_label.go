package mdbgo

import (
	"sort"
	"strconv"
	"strings"
)

func parseJet4LabelTextProperties(control FormControlInfo, fields []jet4TaggedTextField) []FormProperty {
	var props []FormProperty
	for _, field := range fields {
		switch field.Tag {
		case 0xDD:
			value := field.Value
			trimmed := strings.TrimSpace(value)
			if trimmed != "" && !strings.EqualFold(trimmed, control.Name) && !isKnownFormFont(trimmed) {
				props = mergeFormProperties(props, []FormProperty{newTextFormProperty(0x0011, value)})
			}
		case 0xDE:
			props = mergeFormProperties(props, []FormProperty{newTextFormProperty(0x0022, field.Value)})
		case 0xE4:
			props = mergeFormProperties(props, []FormProperty{newTextFormProperty(0x010A, field.Value)})
		}
	}
	return props
}

type jet4LabelNumericProperties struct {
	TextAlign      byte
	FontSize       int
	BackStyle      byte
	BackColor      string
	BackColorValue uint32
	ForeColor      string
	ForeColorValue uint32
	Geometry       formControlGeometry
	HasGeometry    bool
}

func (props jet4LabelNumericProperties) formProperties() []FormProperty {
	return []FormProperty{
		{ID: 0x001D, Name: FormPropertyIDToName(0x001D), ValueType: "Byte", Value: strconv.Itoa(int(props.BackStyle))},
		{ID: 0x0023, Name: FormPropertyIDToName(0x0023), ValueType: "Short", Value: strconv.Itoa(props.FontSize)},
		{ID: 0x0088, Name: FormPropertyIDToName(0x0088), ValueType: "Byte", Value: strconv.Itoa(int(props.TextAlign))},
		{ID: 0x001C, Name: FormPropertyIDToName(0x001C), ValueType: "Color", Value: props.BackColor},
		{ID: 0x00CC, Name: FormPropertyIDToName(0x00CC), ValueType: "Color", Value: props.ForeColor},
	}
}

// parseJet4FormLabelProperties 把 Label 紧凑数值记录按物理顺序与 Label 文本块配对。
// Label 的颜色组合标记是 0x9C (BackColor) 和 0x9E (ForeColor)。
func parseJet4FormLabelProperties(data []byte, controls []FormControlInfo) map[string]jet4LabelNumericProperties {
	result := make(map[string]jet4LabelNumericProperties)
	if len(data) < 8 || len(controls) == 0 || le16(data) > 0x0014 {
		return result
	}

	offsets := orderedFormControlOffsets(data, controls)
	type physicalLabel struct {
		offset int
		name   string
	}
	labels := make([]physicalLabel, 0)
	for i, offset := range offsets {
		if offset >= 0 && controls[i].Type == "Label" {
			labels = append(labels, physicalLabel{offset: offset, name: controls[i].Name})
		}
	}
	sort.Slice(labels, func(i, j int) bool { return labels[i].offset < labels[j].offset })

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

	numericRecords := make([]jet4LabelNumericProperties, 0, len(labels))
	for _, block := range blocks {
		tail := jet4ControlNumericTailForType(block.block, block.name, block.controlType)
		props, ok := parseJet4LabelNumericTail(tail)
		if ok {
			numericRecords = append(numericRecords, props)
		}
	}
	for i := 0; i < len(labels) && i < len(numericRecords); i++ {
		result[strings.ToLower(labels[i].name)] = numericRecords[i]
	}
	return result
}

func parseJet4LabelNumericTail(tail []byte) (jet4LabelNumericProperties, bool) {
	var result jet4LabelNumericProperties
	if len(tail) < 10 {
		return result, false
	}

	colorPos := -1
	for pos := 0; pos+10 <= len(tail); pos++ {
		if tail[pos] == 0x9C && tail[pos+5] == 0x9E {
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
	for pos := 0; pos+1 < layoutPos; pos++ {
		switch tail[pos] {
		case 0x37, 0x3B:
			if tail[pos+1] <= 4 {
				result.TextAlign = tail[pos+1]
			}
		case 0x43:
			if tail[pos+1] <= 1 {
				result.BackStyle = tail[pos+1]
			}
		}
	}

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
		case 0x64:
			result.FontSize = value
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
