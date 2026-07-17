package mdbgo

import (
	"sort"
	"strconv"
	"strings"
)

func parseJet4TabControlTextProperties(_ FormControlInfo, fields []jet4TaggedTextField) []FormProperty {
	var props []FormProperty
	for _, field := range fields {
		switch field.Tag {
		case 0xDF:
			props = mergeFormProperties(props, []FormProperty{newTextFormProperty(0x0022, field.Value)})
		case 0xE8:
			props = mergeFormProperties(props, []FormProperty{newTextFormProperty(0x0016, field.Value)})
		}
	}
	return props
}

type jet4TabControlNumericProperties struct {
	FontSize    int
	FontWeight  int
	Visible     bool
	Geometry    formControlGeometry
	HasGeometry bool
}

func (props jet4TabControlNumericProperties) formProperties() []FormProperty {
	return []FormProperty{
		{ID: 0x0023, Name: FormPropertyIDToName(0x0023), ValueType: "Short", Value: strconv.Itoa(props.FontSize)},
		{ID: 0x0025, Name: FormPropertyIDToName(0x0025), ValueType: "Short", Value: strconv.Itoa(props.FontWeight)},
		{ID: 0x0094, Name: FormPropertyIDToName(0x0094), ValueType: "Bool", Value: strconv.FormatBool(props.Visible)},
	}
}

// parseJet4FormTabControlProperties 按物理顺序把 0x7B TabControl 数值记录配回控件。
// 数值记录通常保存在 Detail 块尾部，而 TabControl 自己的块尾部是第一个 TabPage 的记录。
func parseJet4FormTabControlProperties(data []byte, controls []FormControlInfo) map[string]jet4TabControlNumericProperties {
	result := make(map[string]jet4TabControlNumericProperties)
	if len(data) < 8 || len(controls) == 0 || le16(data) > 0x0014 {
		return result
	}

	offsets := orderedFormControlOffsets(data, controls)
	type physicalTabControl struct {
		offset int
		name   string
	}
	tabControls := make([]physicalTabControl, 0)
	for i, offset := range offsets {
		if offset >= 0 && controls[i].Type == "TabControl" {
			tabControls = append(tabControls, physicalTabControl{offset: offset, name: controls[i].Name})
		}
	}
	sort.Slice(tabControls, func(i, j int) bool { return tabControls[i].offset < tabControls[j].offset })

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

	numericRecords := make([]jet4TabControlNumericProperties, 0, len(tabControls))
	for _, block := range blocks {
		tail := jet4ControlNumericTailForType(block.block, block.name, block.controlType)
		props, ok := parseJet4TabControlNumericTail(tail)
		if ok {
			numericRecords = append(numericRecords, props)
		}
	}
	for i := 0; i < len(tabControls) && i < len(numericRecords); i++ {
		result[strings.ToLower(tabControls[i].name)] = numericRecords[i]
	}
	return result
}

func parseJet4TabControlNumericTail(tail []byte) (jet4TabControlNumericProperties, bool) {
	result := jet4TabControlNumericProperties{Visible: true}
	if len(tail) < 12 {
		return result, false
	}

	payloadPos := -1
	for pos := 0; pos+3 <= len(tail) && pos < 12; pos++ {
		if (tail[pos] == 0xFD || tail[pos] == 0xFE) && tail[pos+1] == 0x7B && tail[pos+2] == 0x00 {
			payloadPos = pos + 3
			break
		}
	}
	if payloadPos < 0 && tail[0] == 0xFF {
		for pos := 1; pos+2 <= len(tail) && pos < 8; pos++ {
			if tail[pos] == 0x7B && tail[pos+1] == 0x00 {
				payloadPos = pos + 2
				break
			}
		}
	}
	if payloadPos < 0 {
		return result, false
	}

	foundWidth := false
	foundHeight := false
	foundFontSize := false
	foundFontWeight := false
	for pos := payloadPos; pos < len(tail); {
		tag := tail[pos]
		switch tag {
		case 0x60, 0x61, 0x62, 0x63, 0x64, 0x65:
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
				result.FontSize = value
				foundFontSize = true
			case 0x65:
				result.FontWeight = value
				foundFontWeight = true
			}
			pos += 3
		case 0xDC:
			pos = len(tail)
		default:
			pos++
		}
	}
	if !foundWidth || !foundHeight || !foundFontSize || !foundFontWeight ||
		result.FontSize <= 0 || result.FontSize > 255 || result.FontWeight < 0 || result.FontWeight > 1000 ||
		result.Geometry.Left > 32767 || result.Geometry.Top > 32767 ||
		result.Geometry.Width <= 0 || result.Geometry.Width > 32767 ||
		result.Geometry.Height <= 0 || result.Geometry.Height > 32767 {
		return result, false
	}
	result.HasGeometry = true
	return result, true
}
