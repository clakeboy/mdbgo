package mdbgo

import (
	"sort"
	"strconv"
	"strings"
)

func parseJet4TabPageTextProperties(_ FormControlInfo, fields []jet4TaggedTextField) []FormProperty {
	var props []FormProperty
	for _, field := range fields {
		switch field.Tag {
		case 0xE3:
			props = mergeFormProperties(props, []FormProperty{newTextFormProperty(0x0016, field.Value)})
		case 0xE8:
			props = mergeFormProperties(props, []FormProperty{newTextFormProperty(0x0011, field.Value)})
		}
	}
	return props
}

type jet4TabPageNumericProperties struct {
	PageIndex   int
	Visible     bool
	Geometry    formControlGeometry
	HasGeometry bool
}

func (props jet4TabPageNumericProperties) formProperties() []FormProperty {
	return []FormProperty{
		{ID: 0x0160, Name: FormPropertyIDToName(0x0160), ValueType: "Short", Value: strconv.Itoa(props.PageIndex)},
		{ID: 0x0094, Name: FormPropertyIDToName(0x0094), ValueType: "Bool", Value: strconv.FormatBool(props.Visible)},
	}
}

// parseJet4FormTabPageProperties 按物理顺序把 0x7C Page 数值记录配回 TabPage。
// 第一页的记录通常位于 TabControl 块尾部，其余页可能位于前一页最后一个控件的块尾部。
func parseJet4FormTabPageProperties(data []byte, controls []FormControlInfo) map[string]jet4TabPageNumericProperties {
	result := make(map[string]jet4TabPageNumericProperties)
	if len(data) < 8 || len(controls) == 0 || le16(data) > 0x0014 {
		return result
	}

	offsets := orderedFormControlOffsets(data, controls)
	type physicalTabPage struct {
		offset int
		name   string
	}
	tabPages := make([]physicalTabPage, 0)
	for i, offset := range offsets {
		if offset >= 0 && controls[i].Type == "TabPage" {
			tabPages = append(tabPages, physicalTabPage{offset: offset, name: controls[i].Name})
		}
	}
	sort.Slice(tabPages, func(i, j int) bool { return tabPages[i].offset < tabPages[j].offset })

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

	numericRecords := make([]jet4TabPageNumericProperties, 0, len(tabPages))
	for _, block := range blocks {
		tail := jet4ControlNumericTailForType(block.block, block.name, block.controlType)
		props, ok := parseJet4TabPageNumericTail(tail)
		if ok {
			numericRecords = append(numericRecords, props)
		}
	}
	for i := 0; i < len(tabPages) && i < len(numericRecords); i++ {
		props := numericRecords[i]
		props.PageIndex = i
		result[strings.ToLower(tabPages[i].name)] = props
	}
	return result
}

func parseJet4TabPageNumericTail(tail []byte) (jet4TabPageNumericProperties, bool) {
	result := jet4TabPageNumericProperties{Visible: true}
	if len(tail) < 12 {
		return result, false
	}

	payloadPos := -1
	for pos := 0; pos+3 <= len(tail) && pos < 12; pos++ {
		if (tail[pos] == 0xFD || tail[pos] == 0xFE) && tail[pos+1] == 0x7C && tail[pos+2] == 0x00 {
			payloadPos = pos + 3
			break
		}
	}
	if payloadPos < 0 && tail[0] == 0xFF {
		for pos := 1; pos+2 <= len(tail) && pos < 8; pos++ {
			if tail[pos] == 0x7C && tail[pos+1] == 0x00 {
				payloadPos = pos + 2
				break
			}
		}
	}
	if payloadPos < 0 {
		return result, false
	}

	var internal formControlGeometry
	foundLeft := false
	foundTop := false
	foundWidth := false
	foundHeight := false
	for pos := payloadPos; pos < len(tail); {
		tag := tail[pos]
		switch tag {
		case 0x60, 0x61, 0x62, 0x63:
			if pos+3 > len(tail) {
				return result, false
			}
			value := int(le16(tail[pos+1:]))
			switch tag {
			case 0x60:
				internal.Left = value
				foundLeft = true
			case 0x61:
				internal.Top = value
				foundTop = true
			case 0x62:
				internal.Width = value
				foundWidth = true
			case 0x63:
				internal.Height = value
				foundHeight = true
			}
			pos += 3
		case 0xDC:
			pos = len(tail)
		default:
			pos++
		}
	}
	if !foundLeft || !foundTop || !foundWidth || !foundHeight ||
		internal.Left < 37 || internal.Top < 30 ||
		internal.Width <= 0 || internal.Height <= 0 {
		return result, false
	}

	// Access COM 的 Page.Left/Top/Width/Height 是包含页边框的外框；Jet4 记录保存内框。
	// 对仓库中的 Access 2010 原生样本，外框稳定地向左扩 37、向上扩 30，
	// 宽度增加 75、高度增加 68 twips。该值同时覆盖不同大小和位置的 TabControl。
	result.Geometry = formControlGeometry{
		Left:   internal.Left - 37,
		Top:    internal.Top - 30,
		Width:  internal.Width + 75,
		Height: internal.Height + 68,
	}
	if result.Geometry.Left > 32767 || result.Geometry.Top > 32767 ||
		result.Geometry.Width <= 0 || result.Geometry.Width > 32767 ||
		result.Geometry.Height <= 0 || result.Geometry.Height > 32767 {
		return result, false
	}
	result.HasGeometry = true
	return result, true
}
