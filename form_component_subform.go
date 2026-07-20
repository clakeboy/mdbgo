package mdbgo

import (
	"sort"
	"strconv"
	"strings"
)

func parseJet4SubFormTextProperties(_ FormControlInfo, fields []jet4TaggedTextField) []FormProperty {
	var props []FormProperty
	for _, field := range fields {
		switch field.Tag {
		case 0xDD:
			value := strings.TrimSpace(field.Value)
			if len(value) >= len("Form.") && strings.EqualFold(value[:len("Form.")], "Form.") {
				value = value[len("Form."):]
			}
			if value != "" {
				props = mergeFormProperties(props, []FormProperty{newTextFormProperty(0x0084, value)})
			}
		case 0xDE:
			props = mergeFormProperties(props, []FormProperty{newTextFormProperty(0x0031, field.Value)})
		case 0xDF:
			props = mergeFormProperties(props, []FormProperty{newTextFormProperty(0x0032, field.Value)})
		case 0xE0:
			props = mergeFormProperties(props, []FormProperty{newTextFormProperty(0x0087, field.Value)})
		case 0xE3:
			props = mergeFormProperties(props, []FormProperty{newTextFormProperty(0x0016, field.Value)})
		}
	}
	return props
}

type jet4SubFormNumericProperties struct {
	TabIndex    int
	HasTabIndex bool
	Locked      bool
	CanShrink   bool
	Visible     bool
	Geometry    formControlGeometry
	HasGeometry bool
}

func (props jet4SubFormNumericProperties) formProperties() []FormProperty {
	result := []FormProperty{
		{ID: 0x0038, Name: FormPropertyIDToName(0x0038), ValueType: "Bool", Value: strconv.FormatBool(props.Locked)},
		{ID: 0x0010, Name: FormPropertyIDToName(0x0010), ValueType: "Bool", Value: strconv.FormatBool(props.CanShrink)},
		{ID: 0x0094, Name: FormPropertyIDToName(0x0094), ValueType: "Bool", Value: strconv.FormatBool(props.Visible)},
	}
	if props.HasTabIndex {
		result = append(result, FormProperty{
			ID: 0x0105, Name: FormPropertyIDToName(0x0105), ValueType: "Short", Value: strconv.Itoa(props.TabIndex),
		})
	}
	return result
}

// parseJet4FormSubFormProperties 按物理顺序把 0x70 SubForm 数值记录配回控件。
// SubForm 的数值记录通常保存于前一个 Label、Detail 或 TabPage 块尾部，不能按名称直接取块。
func parseJet4FormSubFormProperties(data []byte, controls []FormControlInfo) map[string]jet4SubFormNumericProperties {
	result := make(map[string]jet4SubFormNumericProperties)
	if len(data) < 8 || len(controls) == 0 || le16(data) > 0x0014 {
		return result
	}

	offsets := orderedFormControlOffsets(data, controls)
	type physicalSubForm struct {
		offset int
		name   string
	}
	subForms := make([]physicalSubForm, 0)
	for i, offset := range offsets {
		if offset >= 0 && controls[i].Type == "SubForm" {
			subForms = append(subForms, physicalSubForm{offset: offset, name: controls[i].Name})
		}
	}
	sort.Slice(subForms, func(i, j int) bool { return subForms[i].offset < subForms[j].offset })

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

	numericRecords := make([]jet4SubFormNumericProperties, 0, len(subForms))
	for _, block := range blocks {
		tail := jet4ControlNumericTailForType(block.block, block.name, block.controlType)
		props, ok := parseJet4SubFormNumericTail(tail)
		if ok {
			numericRecords = append(numericRecords, props)
		}
	}
	for i := 0; i < len(subForms) && i < len(numericRecords); i++ {
		result[strings.ToLower(subForms[i].name)] = numericRecords[i]
	}
	return result
}

func parseJet4SubFormNumericTail(tail []byte) (jet4SubFormNumericProperties, bool) {
	result := jet4SubFormNumericProperties{Visible: true}
	if len(tail) < 12 {
		return result, false
	}

	payloadPos := -1
	for pos := 0; pos+3 <= len(tail) && pos < 12; pos++ {
		if (tail[pos] == 0xFD || tail[pos] == 0xFE) && tail[pos+1] == 0x70 && tail[pos+2] == 0x00 {
			payloadPos = pos + 3
			break
		}
	}
	// Detail 或 TabPage 边界会使用 FF 掩码头，70 00 仍是稳定的 SubForm 类型标记。
	if payloadPos < 0 && tail[0] == 0xFF {
		for pos := 1; pos+2 <= len(tail) && pos < 8; pos++ {
			if tail[pos] == 0x70 && tail[pos+1] == 0x00 {
				payloadPos = pos + 2
				break
			}
		}
	}
	if payloadPos < 0 {
		return result, false
	}
	result.CanShrink = tail[payloadPos] == 0x04

	foundWidth := false
	foundHeight := false
	for pos := payloadPos; pos < len(tail); {
		tag := tail[pos]
		switch tag {
		case 0x60, 0x61, 0x62, 0x63, 0x64:
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
			case 0x64:
				result.TabIndex = value
				result.HasTabIndex = true
			}
			pos += 3
		default:
			pos++
		}
	}
	if !foundWidth || !foundHeight || result.Geometry.Left > 32767 || result.Geometry.Top > 32767 ||
		result.Geometry.Width <= 0 || result.Geometry.Width > 32767 ||
		result.Geometry.Height <= 0 || result.Geometry.Height > 32767 {
		return result, false
	}
	result.HasGeometry = true
	return result, true
}
