package mdbgo

import (
	"sort"
	"strconv"
	"strings"
)

func parseJet4CheckBoxTextProperties(control FormControlInfo, fields []jet4TaggedTextField) []FormProperty {
	var props []FormProperty
	for _, field := range fields {
		switch field.Tag {
		case 0xDD:
			if source, ok := normalizeControlSource(field.Value, control.Name); ok {
				props = mergeFormProperties(props, []FormProperty{newTextFormProperty(0x001B, source)})
			}
		case 0xF0:
			props = mergeFormProperties(props, []FormProperty{newTextFormProperty(0x010A, field.Value)})
		}
	}
	return props
}

type jet4CheckBoxNumericProperties struct {
	TabIndex    int
	HasTabIndex bool
	Locked      bool
	Visible     bool
	Geometry    formControlGeometry
	HasGeometry bool
}

func (props jet4CheckBoxNumericProperties) formProperties() []FormProperty {
	result := []FormProperty{
		{ID: 0x0038, Name: FormPropertyIDToName(0x0038), ValueType: "Bool", Value: strconv.FormatBool(props.Locked)},
		{ID: 0x0094, Name: FormPropertyIDToName(0x0094), ValueType: "Bool", Value: strconv.FormatBool(props.Visible)},
	}
	if props.HasTabIndex {
		result = append(result, FormProperty{
			ID: 0x0105, Name: FormPropertyIDToName(0x0105), ValueType: "Short", Value: strconv.Itoa(props.TabIndex),
		})
	}
	return result
}

// parseJet4FormCheckBoxProperties 按物理顺序把 CheckBox 专属数值记录配回控件。
// CheckBox 记录以 FD 6A 00 开始，0x60..0x63 保存 twips 几何，0x69 保存 TabIndex。
func parseJet4FormCheckBoxProperties(data []byte, controls []FormControlInfo) map[string]jet4CheckBoxNumericProperties {
	result := make(map[string]jet4CheckBoxNumericProperties)
	if len(data) < 8 || len(controls) == 0 || le16(data) > 0x0014 {
		return result
	}

	offsets := orderedFormControlOffsets(data, controls)
	type physicalCheckBox struct {
		offset int
		name   string
	}
	checkBoxes := make([]physicalCheckBox, 0)
	for i, offset := range offsets {
		if offset >= 0 && controls[i].Type == "CheckBox" {
			checkBoxes = append(checkBoxes, physicalCheckBox{offset: offset, name: controls[i].Name})
		}
	}
	sort.Slice(checkBoxes, func(i, j int) bool { return checkBoxes[i].offset < checkBoxes[j].offset })

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

	numericRecords := make([]jet4CheckBoxNumericProperties, 0, len(checkBoxes))
	for _, block := range blocks {
		tail := jet4ControlNumericTailForType(block.block, block.name, block.controlType)
		props, ok := parseJet4CheckBoxNumericTail(tail)
		if ok {
			numericRecords = append(numericRecords, props)
		}
	}
	for i := 0; i < len(checkBoxes) && i < len(numericRecords); i++ {
		result[strings.ToLower(checkBoxes[i].name)] = numericRecords[i]
	}
	return result
}

func parseJet4CheckBoxNumericTail(tail []byte) (jet4CheckBoxNumericProperties, bool) {
	result := jet4CheckBoxNumericProperties{Visible: true}
	if len(tail) < 12 {
		return result, false
	}

	recordPos := -1
	for pos := 0; pos+3 <= len(tail) && pos < 12; pos++ {
		if tail[pos] == 0xFD && tail[pos+1] == 0x6A && tail[pos+2] == 0x00 {
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
