package mdbgo

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// accessRawJSONForm 对齐 AccessExport.TestClass1.OutputRawFormControlsJson 的输出层级。
// Hash 来自 Windows COM 包装对象的运行时 GetHashCode，不存在于 MDB 持久数据中，因此不输出。
type accessRawJSONForm struct {
	Controls        []accessRawJSONControl `json:"Controls"`
	Name            string                 `json:"Name"`
	Title           string                 `json:"Title"`
	Width           int                    `json:"Width"`
	View            int                    `json:"View"`
	Source          string                 `json:"Source"`
	Height          int                    `json:"Height"`
	BackGroundColor int64                  `json:"BackGroundColor"`
}

type accessRawJSONControl struct {
	ClassType string `json:"ClassType"`
	Width     int    `json:"Width"`
	Height    int    `json:"Height"`
	Name      string `json:"Name"`
	Top       int    `json:"Top"`
	Left      int    `json:"Left"`

	Locked    *bool `json:"Locked,omitempty"`
	BackStyle *int  `json:"BackStyle,omitempty"`

	Text            *string `json:"Text,omitempty"`
	BackGroundColor *int64  `json:"BackGroundColor,omitempty"`
	FrontColor      *int64  `json:"FrontColor,omitempty"`
	FontSize        *int    `json:"FontSize,omitempty"`
	TextAlign       *int    `json:"TextAlign,omitempty"`
	Source          *string `json:"Source,omitempty"`
	Tag             *string `json:"Tag,omitempty"`

	IsTextArea *int    `json:"IsTextArea,omitempty"`
	IsRichText *int    `json:"IsRichText,omitempty"`
	IsReadOnly *bool   `json:"IsReadOnly,omitempty"`
	IsVisible  *bool   `json:"IsVisible,omitempty"`
	TabIndex   *int    `json:"TabIndex,omitempty"`
	Format     *string `json:"Format,omitempty"`
	Length     *int    `json:"Length,omitempty"`
	Underline  *bool   `json:"Underline,omitempty"`

	SourceField  *string `json:"SourceField,omitempty"`
	RowSource    *string `json:"RowSource,omitempty"`
	BoundField   *int    `json:"BoundField,omitempty"`
	SearchColumn *int    `json:"SearchColumn,omitempty"`
	Columns      *string `json:"Columns,omitempty"`

	Icon    *string `json:"Icon,omitempty"`
	Color   *int64  `json:"Color,omitempty"`
	Tip     *string `json:"Tip,omitempty"`
	Outline *int    `json:"Outline,omitempty"`

	LineWidth       *int   `json:"LineWidth,omitempty"`
	LineColor       *int64 `json:"LineColor,omitempty"`
	BackTransparent *int   `json:"BackTransparent,omitempty"`
	LineTransparent *int   `json:"LineTransparent,omitempty"`

	Value  *string `json:"Value,omitempty"`
	NoWarp *bool   `json:"NoWarp,omitempty"`

	EditMode  *bool   `json:"EditMode,omitempty"`
	OrderBy   *string `json:"OrderBy,omitempty"`
	RowHeight *int    `json:"RowHeight,omitempty"`

	Controls []accessRawJSONControl `json:"Controls,omitempty"`
	Tabs     []accessRawJSONControl `json:"Tabs,omitempty"`
}

type accessRawJSONField struct {
	name  string
	value any
}

// MarshalJSON mirrors the insertion order used by AccessExport.TestClass1.
// The C# exporter writes the common BaseControl properties first and then
// writes the properties for the concrete Access control type. A single Go
// struct declaration cannot represent those different per-type orders.
func (control accessRawJSONControl) MarshalJSON() ([]byte, error) {
	fields := make([]accessRawJSONField, 0, 24)
	add := func(name string, value any) {
		fields = append(fields, accessRawJSONField{name: name, value: value})
	}
	addPtr := func(name string, value any, present bool) {
		if present {
			add(name, value)
		}
	}

	add("ClassType", control.ClassType)
	add("Width", control.Width)
	add("Height", control.Height)
	add("Name", control.Name)
	add("Top", control.Top)
	add("Left", control.Left)
	addPtr("Locked", control.Locked, control.Locked != nil)
	addPtr("BackStyle", control.BackStyle, control.BackStyle != nil)

	switch control.ClassType {
	case "Label":
		addPtr("Text", control.Text, control.Text != nil)
		addPtr("BackGroundColor", control.BackGroundColor, control.BackGroundColor != nil)
		addPtr("FrontColor", control.FrontColor, control.FrontColor != nil)
		addPtr("FontSize", control.FontSize, control.FontSize != nil)
		addPtr("TextAlign", control.TextAlign, control.TextAlign != nil)
		addPtr("Source", control.Source, control.Source != nil)
		addPtr("Tag", control.Tag, control.Tag != nil)
	case "TextBox":
		addPtr("Tag", control.Tag, control.Tag != nil)
		addPtr("Source", control.Source, control.Source != nil)
		addPtr("TextAlign", control.TextAlign, control.TextAlign != nil)
		addPtr("IsTextArea", control.IsTextArea, control.IsTextArea != nil)
		addPtr("IsRichText", control.IsRichText, control.IsRichText != nil)
		addPtr("IsReadOnly", control.IsReadOnly, control.IsReadOnly != nil)
		addPtr("IsVisible", control.IsVisible, control.IsVisible != nil)
		addPtr("TabIndex", control.TabIndex, control.TabIndex != nil)
		addPtr("Format", control.Format, control.Format != nil)
		addPtr("Length", control.Length, control.Length != nil)
		addPtr("Underline", control.Underline, control.Underline != nil)
		addPtr("FrontColor", control.FrontColor, control.FrontColor != nil)
		addPtr("BackGroundColor", control.BackGroundColor, control.BackGroundColor != nil)
	case "ComboBox":
		addPtr("TextAlign", control.TextAlign, control.TextAlign != nil)
		addPtr("Source", control.Source, control.Source != nil)
		addPtr("SourceField", control.SourceField, control.SourceField != nil)
		addPtr("RowSource", control.RowSource, control.RowSource != nil)
		addPtr("BoundField", control.BoundField, control.BoundField != nil)
		addPtr("IsReadOnly", control.IsReadOnly, control.IsReadOnly != nil)
		addPtr("IsVisible", control.IsVisible, control.IsVisible != nil)
		addPtr("TabIndex", control.TabIndex, control.TabIndex != nil)
		addPtr("SearchColumn", control.SearchColumn, control.SearchColumn != nil)
		addPtr("Columns", control.Columns, control.Columns != nil)
	case "Button":
		addPtr("Text", control.Text, control.Text != nil)
		addPtr("Icon", control.Icon, control.Icon != nil)
		addPtr("Color", control.Color, control.Color != nil)
		addPtr("Tip", control.Tip, control.Tip != nil)
		addPtr("Outline", control.Outline, control.Outline != nil)
	case "CheckBox", "RadioGroup":
		addPtr("Source", control.Source, control.Source != nil)
	case "Rectangle":
		addPtr("BackGroundColor", control.BackGroundColor, control.BackGroundColor != nil)
		addPtr("LineWidth", control.LineWidth, control.LineWidth != nil)
		addPtr("LineColor", control.LineColor, control.LineColor != nil)
		addPtr("BackTransparent", control.BackTransparent, control.BackTransparent != nil)
		addPtr("LineTransparent", control.LineTransparent, control.LineTransparent != nil)
	case "Radio":
		addPtr("Value", control.Value, control.Value != nil)
	case "TabPage":
		addPtr("Text", control.Text, control.Text != nil)
	case "Table":
		addPtr("Source", control.Source, control.Source != nil)
		addPtr("SourceField", control.SourceField, control.SourceField != nil)
		addPtr("EditMode", control.EditMode, control.EditMode != nil)
		addPtr("OrderBy", control.OrderBy, control.OrderBy != nil)
		addPtr("RowHeight", control.RowHeight, control.RowHeight != nil)
		addPtr("NoWarp", control.NoWarp, control.NoWarp != nil)
	}

	// TestClass1 creates these lists after CreateRawControl, even when empty.
	switch control.ClassType {
	case "TabControl":
		tabs := control.Tabs
		if tabs == nil {
			tabs = []accessRawJSONControl{}
		}
		add("Tabs", tabs)
	case "TabPage", "Table":
		controls := control.Controls
		if controls == nil {
			controls = []accessRawJSONControl{}
		}
		add("Controls", controls)
	}

	return marshalAccessRawJSONFields(fields)
}

func marshalAccessRawJSONFields(fields []accessRawJSONField) ([]byte, error) {
	var output bytes.Buffer
	output.WriteByte('{')
	for index, field := range fields {
		if index > 0 {
			output.WriteByte(',')
		}
		name, err := marshalAccessRawJSONValue(field.name)
		if err != nil {
			return nil, err
		}
		value, err := marshalAccessRawJSONValue(field.value)
		if err != nil {
			return nil, err
		}
		output.Write(name)
		output.WriteByte(':')
		output.Write(value)
	}
	output.WriteByte('}')
	return output.Bytes(), nil
}

func marshalAccessRawJSONValue(value any) ([]byte, error) {
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(output.Bytes(), []byte{'\n'}), nil
}

func marshalAccessRawJSONIndent(value any) ([]byte, error) {
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(output.Bytes(), []byte{'\n'}), nil
}

func TestAccessRawJSONFieldOrder(t *testing.T) {
	tests := []struct {
		classType string
		want      []string
	}{
		{
			classType: "TabControl",
			want:      []string{"ClassType", "Width", "Height", "Name", "Top", "Left", "Locked", "BackStyle", "Tabs"},
		},
		{
			classType: "TabPage",
			want:      []string{"ClassType", "Width", "Height", "Name", "Top", "Left", "Locked", "Text", "Controls"},
		},
		{
			classType: "TextBox",
			want: []string{
				"ClassType", "Width", "Height", "Name", "Top", "Left", "Locked", "BackStyle",
				"Tag", "Source", "TextAlign", "IsTextArea", "IsRichText", "IsReadOnly",
				"IsVisible", "TabIndex", "Format", "Length", "Underline", "FrontColor", "BackGroundColor",
			},
		},
		{
			classType: "Label",
			want: []string{
				"ClassType", "Width", "Height", "Name", "Top", "Left", "Locked", "BackStyle",
				"Text", "BackGroundColor", "FrontColor", "FontSize", "TextAlign", "Source", "Tag",
			},
		},
		{
			classType: "ComboBox",
			want: []string{
				"ClassType", "Width", "Height", "Name", "Top", "Left", "Locked", "BackStyle",
				"TextAlign", "Source", "SourceField", "RowSource", "BoundField", "IsReadOnly",
				"IsVisible", "TabIndex", "SearchColumn", "Columns",
			},
		},
		{
			classType: "Button",
			want:      []string{"ClassType", "Width", "Height", "Name", "Top", "Left", "Locked", "BackStyle", "Text", "Icon", "Color", "Tip", "Outline"},
		},
		{
			classType: "CheckBox",
			want:      []string{"ClassType", "Width", "Height", "Name", "Top", "Left", "Locked", "Source"},
		},
		{
			classType: "Rectangle",
			want:      []string{"ClassType", "Width", "Height", "Name", "Top", "Left", "Locked", "BackStyle", "BackGroundColor", "LineWidth", "LineColor", "BackTransparent", "LineTransparent"},
		},
		{
			classType: "RadioGroup",
			want:      []string{"ClassType", "Width", "Height", "Name", "Top", "Left", "Locked", "BackStyle", "Source"},
		},
		{
			classType: "Radio",
			want:      []string{"ClassType", "Width", "Height", "Name", "Top", "Left", "Locked", "Value"},
		},
		{
			classType: "Table",
			want: []string{
				"ClassType", "Width", "Height", "Name", "Top", "Left", "Locked",
				"Source", "SourceField", "EditMode", "OrderBy", "RowHeight", "NoWarp", "Controls",
			},
		},
		{
			classType: "ToggleButtonClass",
			want:      []string{"ClassType", "Width", "Height", "Name", "Top", "Left", "Locked"},
		},
	}

	for _, test := range tests {
		t.Run(test.classType, func(t *testing.T) {
			data, err := json.Marshal(accessRawJSONControlForOrder(test.classType))
			if err != nil {
				t.Fatalf("marshal control failed: %v", err)
			}
			got, err := accessRawJSONKeys(data)
			if err != nil {
				t.Fatalf("read JSON keys failed: %v", err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("JSON keys=%v want=%v; JSON=%s", got, test.want, data)
			}
		})
	}
}

func TestAccessRawJSONFormFieldOrder(t *testing.T) {
	data, err := json.Marshal(accessRawJSONForm{
		Controls:        []accessRawJSONControl{},
		Name:            "form",
		Title:           "title",
		Width:           1,
		View:            2,
		Source:          "source",
		Height:          3,
		BackGroundColor: 4,
	})
	if err != nil {
		t.Fatalf("marshal form failed: %v", err)
	}
	got, err := accessRawJSONKeys(data)
	if err != nil {
		t.Fatalf("read JSON keys failed: %v", err)
	}
	want := []string{"Controls", "Name", "Title", "Width", "View", "Source", "Height", "BackGroundColor"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("JSON keys=%v want=%v; JSON=%s", got, want, data)
	}
}

func TestAccessRawJSONEmptyFormUsesArray(t *testing.T) {
	data, err := marshalAccessRawJSONIndent(accessRawJSONForm{Controls: []accessRawJSONControl{}})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"Controls": []`)) || bytes.Contains(data, []byte(`"Controls": null`)) {
		t.Fatalf("empty form controls must match C# List serialization: %s", data)
	}
}

func TestAccessRawJSONClassTypeMatchesTestClass1(t *testing.T) {
	tests := map[string]string{
		"Label":        "Label",
		"TextBox":      "TextBox",
		"Button":       "Button",
		"OptionGroup":  "RadioGroup",
		"OptionButton": "Radio",
		"TabControl":   "TabControl",
		"TabPage":      "TabPage",
		"SubForm":      "Table",
		"ToggleButton": "ToggleButtonClass",
		"Image":        "ImageClass",
		"Line":         "LineClass",
	}
	for controlType, want := range tests {
		if got := accessRawJSONClassType(controlType); got != want {
			t.Errorf("accessRawJSONClassType(%q)=%q want=%q", controlType, got, want)
		}
	}
}

func TestAccessRawFormSectionsMatchesTestClass1Order(t *testing.T) {
	sections := []FormSectionContent{
		{Type: "FormFooter"},
		{Type: "ReportHeader"},
		{Type: "Detail"},
		{Type: "FormHeader"},
		{Type: "PageFooter"},
	}
	got := accessRawFormSections(sections)
	want := []string{"FormHeader", "Detail", "FormFooter"}
	if len(got) != len(want) {
		t.Fatalf("form sections=%v want=%v", got, want)
	}
	for i := range want {
		if got[i].Type != want[i] {
			t.Fatalf("form section %d=%q want=%q", i, got[i].Type, want[i])
		}
	}
}

func accessRawJSONControlForOrder(classType string) accessRawJSONControl {
	control := accessRawJSONControl{
		ClassType:       classType,
		Width:           1,
		Height:          2,
		Name:            "control",
		Top:             3,
		Left:            4,
		Locked:          jsonBool(true),
		BackStyle:       jsonInt(22),
		Text:            jsonString("text"),
		BackGroundColor: jsonInt64(5),
		FrontColor:      jsonInt64(6),
		FontSize:        jsonInt(7),
		TextAlign:       jsonInt(8),
		Source:          jsonString("source"),
		Tag:             jsonString("tag"),
		IsTextArea:      jsonInt(9),
		IsRichText:      jsonInt(10),
		IsReadOnly:      jsonBool(false),
		IsVisible:       jsonBool(true),
		TabIndex:        jsonInt(11),
		Format:          jsonString("format"),
		Length:          jsonInt(12),
		Underline:       jsonBool(false),
		SourceField:     jsonString("source-field"),
		RowSource:       jsonString("row-source"),
		BoundField:      jsonInt(13),
		SearchColumn:    jsonInt(14),
		Columns:         jsonString("columns"),
		Icon:            jsonString("icon"),
		Color:           jsonInt64(15),
		Tip:             jsonString("tip"),
		Outline:         jsonInt(16),
		LineWidth:       jsonInt(17),
		LineColor:       jsonInt64(18),
		BackTransparent: jsonInt(19),
		LineTransparent: jsonInt(20),
		Value:           jsonString("value"),
		NoWarp:          jsonBool(true),
		EditMode:        jsonBool(true),
		OrderBy:         jsonString("field_name"),
		RowHeight:       jsonInt(21),
	}
	switch classType {
	case "Label", "TextBox", "ComboBox", "Button", "Rectangle", "RadioGroup", "TabControl":
	default:
		control.BackStyle = nil
	}
	return control
}

func TestAccessRawJSONDoesNotEscapeHTMLCharacters(t *testing.T) {
	control := accessRawJSONControlForOrder("TextBox")
	control.Source = jsonString("=[value] <> '&'")
	data, err := marshalAccessRawJSONIndent(control)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(`\u003c`)) || bytes.Contains(data, []byte(`\u003e`)) ||
		bytes.Contains(data, []byte(`\u0026`)) {
		t.Fatalf("Access raw JSON contains Go HTML escapes: %s", data)
	}
	if !bytes.Contains(data, []byte(`=[value] <> '&'`)) {
		t.Fatalf("Access raw JSON source changed: %s", data)
	}
}

func accessRawJSONKeys(data []byte) ([]string, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if token != json.Delim('{') {
		return nil, errors.New("expected JSON object")
	}

	keys := make([]string, 0)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		key, ok := token.(string)
		if !ok {
			return nil, errors.New("expected JSON object key")
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	return keys, nil
}

type accessRawJSONBuilder struct {
	entries       []AccessObjectEntry
	objectStorage string
	cache         map[string]*FormContent
	active        map[string]bool
}

func newAccessRawJSONBuilder(entries []AccessObjectEntry, objectStorage ...string) *accessRawJSONBuilder {
	builder := &accessRawJSONBuilder{
		entries: entries,
		cache:   make(map[string]*FormContent),
		active:  make(map[string]bool),
	}
	if len(objectStorage) > 0 {
		builder.objectStorage = objectStorage[0]
	}
	return builder
}

func (builder *accessRawJSONBuilder) formContent(formName string) (*FormContent, error) {
	key := strings.ToLower(strings.TrimSpace(formName))
	if content := builder.cache[key]; content != nil {
		return content, nil
	}
	streams, err := formObjectStreamsFromEntries(builder.entries, formName)
	if err != nil {
		return nil, err
	}
	streams.ObjectStorage = builder.objectStorage
	content, err := ParseFormContent(streams)
	if err != nil {
		return nil, err
	}
	builder.cache[key] = content
	return content, nil
}

func (builder *accessRawJSONBuilder) buildForm(formName string) (*accessRawJSONForm, error) {
	content, err := builder.formContent(formName)
	if err != nil {
		return nil, err
	}
	result := &accessRawJSONForm{
		Controls:        make([]accessRawJSONControl, 0),
		Name:            content.FormName,
		Title:           content.Caption,
		Width:           content.Width,
		View:            content.DefaultView,
		Source:          content.RecordSource,
		Height:          content.Height,
		BackGroundColor: accessRawColorValue(content.BackColorValue),
	}
	key := strings.ToLower(content.FormName)
	if builder.active[key] {
		return result, nil
	}
	builder.active[key] = true
	defer delete(builder.active, key)
	for _, section := range accessRawFormSections(content.Sections) {
		controls, err := builder.buildControlSequence(section.Controls)
		if err != nil {
			return nil, err
		}
		result.Controls = append(result.Controls, controls...)
	}
	return result, nil
}

func accessRawFormSections(sections []FormSectionContent) []FormSectionContent {
	result := make([]FormSectionContent, 0, 3)
	// 与 TestClass1.OutputRawFormControlsJson 的三次 AddRawSectionControls
	// 调用保持一致；报告分区和其他内部分区不属于 Form 原生输出。
	for _, sectionType := range []string{"FormHeader", "Detail", "FormFooter"} {
		for _, section := range sections {
			if section.Type == sectionType {
				result = append(result, section)
				break
			}
		}
	}
	return result
}

// buildControlSequence 利用 Blob 中的物理顺序重建 TabControl -> TabPage -> Controls。
// TypeInfo 是逻辑目录，部分后创建的控件可能位于另一个 TabPage 条目之后；
// Blob 中每个 TabPage 起点及其后的控件顺序才与 Access Pages.Controls 一致。
func (builder *accessRawJSONBuilder) buildControlSequence(controls []FormControlContent) ([]accessRawJSONControl, error) {
	controls = controlsInBlobOrder(controls)
	result := make([]accessRawJSONControl, 0, len(controls))
	for pos := 0; pos < len(controls); {
		control := controls[pos]
		if control.Type != "TabControl" {
			converted, err := builder.convertControl(control)
			if err != nil {
				return nil, err
			}
			result = append(result, converted)
			pos++
			continue
		}

		tab, err := builder.convertControl(control)
		if err != nil {
			return nil, err
		}
		pos++
		for pos < len(controls) && controls[pos].Type == "TabPage" {
			page, err := builder.convertControl(controls[pos])
			if err != nil {
				return nil, err
			}
			pos++
			childrenStart := pos
			isLastPage := !hasFollowingTabPage(controls, pos)
			for pos < len(controls) && controls[pos].Type != "TabPage" && controls[pos].Type != "TabControl" {
				// Access 允许 Detail 根控件在 TypeInfo/Blob 中追加到最后一个 Page
				// 之后。其坐标位于整个 TabControl 上方，不能归入最后一页。
				if isLastPage && controlIsAboveTabFrame(controls[pos], control) {
					break
				}
				pos++
			}
			page.Controls, err = builder.buildControlSequence(controls[childrenStart:pos])
			if err != nil {
				return nil, err
			}
			tab.Tabs = append(tab.Tabs, page)
		}
		result = append(result, tab)
	}
	return result, nil
}

func hasFollowingTabPage(controls []FormControlContent, start int) bool {
	for pos := start; pos < len(controls); pos++ {
		switch controls[pos].Type {
		case "TabPage":
			return true
		case "TabControl":
			return false
		}
	}
	return false
}

func controlIsAboveTabFrame(control, tabControl FormControlContent) bool {
	return control.HasGeometry && tabControl.HasGeometry && control.Top < tabControl.Top
}

func controlsInBlobOrder(controls []FormControlContent) []FormControlContent {
	if len(controls) < 2 {
		return controls
	}
	for _, control := range controls {
		if control.BlobOffset < 0 {
			// 旧格式或损坏的 Blob 无法可靠定位全部控件时，保留 TypeInfo 顺序。
			return controls
		}
	}
	ordered := append([]FormControlContent(nil), controls...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].BlobOffset < ordered[j].BlobOffset
	})
	return ordered
}

func (builder *accessRawJSONBuilder) convertControl(control FormControlContent) (accessRawJSONControl, error) {
	result := accessRawJSONControl{
		ClassType: accessRawJSONClassType(control.Type),
		Width:     control.Width,
		Height:    control.Height,
		Name:      control.Name,
		Top:       control.Top,
		Left:      control.Left,
	}
	switch control.Type {
	case "Label", "TextBox", "ComboBox", "Button", "Rectangle", "OptionGroup", "TabControl":
		result.BackStyle = jsonInt(control.BackStyle)
	}

	switch control.Type {
	case "Label":
		result.Text = jsonString(control.Caption)
		result.BackGroundColor = jsonInt64(accessRawColorValue(control.BackColorValue))
		result.FrontColor = jsonInt64(accessRawColorValue(control.ForeColorValue))
		result.FontSize = jsonInt(control.FontSize)
		result.TextAlign = jsonInt(int(control.TextAlignValue))
		result.Tag = jsonString(control.Tag)
	case "TextBox":
		result.Locked = jsonBool(control.Locked)
		result.Tag = jsonString(control.Tag)
		result.Source = jsonString(control.ControlSource)
		result.TextAlign = jsonInt(int(control.TextAlignValue))
		// AccessExport 的 IsTextArea 字段直接输出 TextBox.ScrollBars 的
		// 原生枚举值（0=None、1=Horizontal、2=Vertical、3=Both）。
		result.IsTextArea = jsonInt(int(control.ScrollBars))
		result.IsRichText = jsonInt(0)
		result.IsReadOnly = jsonBool(control.Locked)
		result.IsVisible = jsonBool(control.Visible)
		result.TabIndex = jsonInt(control.TabIndex)
		result.Format = jsonString(control.Format)
		result.Underline = jsonBool(control.Underline)
		result.FrontColor = jsonInt64(accessRawColorValue(control.ForeColorValue))
		result.BackGroundColor = jsonInt64(accessRawColorValue(control.BackColorValue))
	case "ComboBox":
		result.Locked = jsonBool(control.Locked)
		result.TextAlign = jsonInt(int(control.TextAlignValue))
		result.Source = jsonString(control.ControlSource)
		result.SourceField = jsonString(control.ControlSource)
		result.RowSource = jsonString(control.RowSource)
		result.BoundField = jsonInt(control.BoundColumn)
		result.IsReadOnly = jsonBool(control.Locked)
		result.IsVisible = jsonBool(control.Visible)
		result.TabIndex = jsonInt(control.TabIndex)
		result.SearchColumn = jsonInt(control.BoundColumn)
		result.Columns = jsonString(control.ColumnWidths)
	case "Button":
		result.Text = jsonString(control.Caption)
		result.Icon = jsonString(control.Picture)
		result.Color = jsonInt64(accessRawColorValue(control.BackColorValue))
		result.Tip = jsonString(control.ControlTipText)
		result.Outline = jsonInt(control.BackStyle)
	case "ToggleButton":
		// C# CreateRawControl 没有 ToggleButtonClass 专属分支，只读取
		// BaseControl 公共属性；ToggleButton 自身支持 Locked。
		result.Locked = jsonBool(control.Locked)
	case "CheckBox":
		result.Locked = jsonBool(control.Locked)
		result.Source = jsonString(control.ControlSource)
	case "Rectangle":
		result.BackGroundColor = jsonInt64(accessRawColorValue(control.BackColorValue))
		result.LineWidth = jsonInt(control.BorderWidth)
		result.LineColor = jsonInt64(accessRawColorValue(control.BorderColorValue))
		result.BackTransparent = jsonInt(control.BackStyle)
		result.LineTransparent = jsonInt(control.BorderStyle)
	case "OptionGroup":
		result.Locked = jsonBool(control.Locked)
		result.Source = jsonString(control.ControlSource)
	case "OptionButton":
		result.Locked = jsonBool(control.Locked)
		result.Value = jsonString(control.StatusBarText)
	case "TabPage":
		result.Text = jsonString(control.Caption)
	case "SubForm":
		result.Locked = jsonBool(control.Locked)
		result.Source = jsonString(control.SourceObject)
		result.SourceField = jsonString(control.LinkChildFields)
		// AccessExport 的原生输出保留模型字段名 NoWarp，但值直接来自 CanShrink。
		result.NoWarp = jsonBool(control.CanShrink)
		if strings.TrimSpace(control.SourceObject) != "" {
			child, err := builder.buildForm(control.SourceObject)
			if err == nil {
				result.Controls = child.Controls
			}
		}
	}
	return result, nil
}

func accessRawJSONClassType(controlType string) string {
	switch controlType {
	case "SubForm":
		return "Table"
	case "OptionGroup":
		return "RadioGroup"
	case "OptionButton":
		return "Radio"
	case "ToggleButton", "Image", "Line":
		// TestClass1.ToModelClassType 未特殊映射这些 Access RCW 类型，
		// 因而保留 control.GetType().Name 返回的 *Class 名称。
		return controlType + "Class"
	default:
		return controlType
	}
}

func jsonString(value string) *string { return &value }
func jsonInt(value int) *int          { return &value }
func jsonInt64(value int64) *int64    { return &value }
func jsonBool(value bool) *bool       { return &value }

func accessRawColorValue(value uint32) int64 {
	// Access/COM 的颜色属性类型为有符号 32 位整数；系统颜色从 0x80000000 开始。
	return int64(int32(value))
}

// TestExportFormAsAccessJSON 可指定任意窗体，并按 t_abia_master_org.json 风格输出原生值。
//
// 示例：
// MDBGO_EXPORT_FORM_NAME=f_abia_master go test -run TestExportFormAsAccessJSON -v -count=1
//
//	MDBGO_EXPORT_FORM_NAME=f_abia_master MDBGO_EXPORT_FORM_OUTPUT=testdb/f_abia_master_mdbgo.json \
//	  go test -run TestExportFormAsAccessJSON -v -count=1
func TestExportFormAsAccessJSON(t *testing.T) {
	formName := strings.TrimSpace(os.Getenv("MDBGO_EXPORT_FORM_NAME"))
	if formName == "" {
		t.Skip("set MDBGO_EXPORT_FORM_NAME to export an Access form")
	}
	db, err := Open(requireDBFile(t))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = db.Close() }()
	entries, err := db.ReadAccessObjectEntries()
	if err != nil {
		t.Fatalf("ReadAccessObjectEntries failed: %v", err)
	}
	exported, err := newAccessRawJSONBuilder(entries).buildForm(formName)
	if err != nil {
		t.Fatalf("build Access JSON for %q failed: %v", formName, err)
	}
	data, err := marshalAccessRawJSONIndent(exported)
	if err != nil {
		t.Fatalf("marshal Access JSON failed: %v", err)
	}
	data = append(data, '\n')

	outputPath := strings.TrimSpace(os.Getenv("MDBGO_EXPORT_FORM_OUTPUT"))
	if outputPath == "" {
		t.Logf("form=%q\n%s", formName, data)
		return
	}
	if err := os.WriteFile(outputPath, data, 0o644); err != nil {
		t.Fatalf("write Access JSON %s failed: %v", outputPath, err)
	}
	t.Logf("form=%q JSON written to %s (%d bytes)", formName, outputPath, len(data))
}

// TestExportAllFormsAsAccessJSON 导出指定 MDB 的全部窗体到指定目录，
// 每个窗体输出一个 <窗体名>.json，内容格式与 TestExportFormAsAccessJSON 一致。
//
// 示例：
//
//	MDBGO_TEST_DB=testdb/mdbs/dms.mdb MDBGO_EXPORT_FORMS_DIR=/tmp/forms_out \
//	  go test -run TestExportAllFormsAsAccessJSON -v -count=1
func TestExportAllFormsAsAccessJSON(t *testing.T) {
	outputDir := strings.TrimSpace(os.Getenv("MDBGO_EXPORT_FORMS_DIR"))
	if outputDir == "" {
		t.Skip("set MDBGO_EXPORT_FORMS_DIR to export all Access forms")
	}
	db, err := Open(requireDBFile(t))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = db.Close() }()
	entries, err := db.ReadAccessObjectEntries()
	if err != nil {
		t.Fatalf("ReadAccessObjectEntries failed: %v", err)
	}
	formIDs, err := formStorageIDsFromEntries(entries)
	if err != nil {
		t.Fatalf("formStorageIDsFromEntries failed: %v", err)
	}
	formNames := make([]string, 0, len(formIDs))
	for name := range formIDs {
		formNames = append(formNames, name)
	}
	sort.Strings(formNames)
	if len(formNames) == 0 {
		t.Skip("database contains no forms")
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("create output dir %s failed: %v", outputDir, err)
	}

	builder := newAccessRawJSONBuilder(entries, db.Format.ObjectStorage)
	exported, failed := 0, 0
	for _, name := range formNames {
		form, err := builder.buildForm(name)
		if err != nil {
			t.Errorf("build Access JSON for %q failed: %v", name, err)
			failed++
			continue
		}
		data, err := marshalAccessRawJSONIndent(form)
		if err != nil {
			t.Errorf("marshal Access JSON for %q failed: %v", name, err)
			failed++
			continue
		}
		data = append(data, '\n')
		outputPath := filepath.Join(outputDir, name+".json")
		if err := os.WriteFile(outputPath, data, 0o644); err != nil {
			t.Errorf("write Access JSON %s failed: %v", outputPath, err)
			failed++
			continue
		}
		exported++
	}
	t.Logf("exported %d forms to %s (%d failed)", exported, outputDir, failed)
	if failed > 0 {
		t.Fail()
	}
}

func TestBuildAccessJSONFOemHbl(t *testing.T) {
	testBuildAccessJSONAgainstRawFixtureAtDB(t,
		filepath.Join("testdb", "mdbs", "mpci_2003.mdb"),
		"f_oem_hbl", filepath.Join("testdb", "f_oem_hbl_org.json"))
}

func TestBuildAccessJSONFDocUpload(t *testing.T) {
	testBuildAccessJSONAgainstRawFixtureAtDB(t,
		filepath.Join("testdb", "mdbs", "mpci_2003.mdb"),
		"f_doc_upload", filepath.Join("testdb", "f_doc_upload_org.json"))
}

func TestBuildAccessJSONFActBranchQuery(t *testing.T) {
	testBuildAccessJSONAgainstRawFixtureAtDB(t,
		filepath.Join("testdb", "mdbs", "eIT.mdb"),
		"f_act_branch_query", filepath.Join("testdb", "f_act_branch_query_org.json"))
}

func TestBuildAccessJSONFAemHsSummary(t *testing.T) {
	testBuildAccessJSONAgainstRawFixtureAtDB(t,
		filepath.Join("testdb", "mdbs", "dms-0805.mdb"),
		"f_aem_hs_summary", filepath.Join("testdb", "f_aem_hs_summary_org.json"))
}

func TestBuildAccessJSONFOemHsSummary(t *testing.T) {
	testBuildAccessJSONAgainstRawFixtureAtDB(t,
		filepath.Join("testdb", "mdbs", "dms-0805.mdb"),
		"f_oem_hs_summary", filepath.Join("testdb", "f_oem_hs_summary_org.json"))
}

func TestBuildAccessJSONFHtsUsQ1Query(t *testing.T) {
	testBuildAccessJSONAgainstRawFixtureAtDB(t,
		filepath.Join("testdb", "mdbs", "HTSUS-0807.mdb"),
		"f_hts_us_q1_query", filepath.Join("testdb", "f_hts_us_q1_query_org.json"))
}

func TestBuildAccessJSONFAbi01HsuPostQuery(t *testing.T) {
	testBuildAccessJSONAgainstRawFixtureAtDB(t,
		filepath.Join("testdb", "mdbs", "HTSUS-0807.mdb"),
		"f_abi_01_hsu_post_query", filepath.Join("testdb", "f_abi_01_hsu_post_query_org.json"))
}

func TestBuildAccessJSONFAbiEntry(t *testing.T) {
	testBuildAccessJSONAgainstRawFixture(t,
		"f_abi_entry", filepath.Join("testdb", "f_abi_entry_org.json"))
}

func TestBuildAccessJSONFOem(t *testing.T) {
	testBuildAccessJSONPropertiesAgainstRawFixture(t,
		"f_oem", filepath.Join("testdb", "f_oem_org.json"))
}

// TestBuildAccessJSONDMSWindowsParity 验证 Access 2000 格式的 dms-0805.mdb
// 全部窗体与 Windows Access COM 导出夹具保持逐字段一致。
func TestBuildAccessJSONDMSWindowsParity(t *testing.T) {
	testBuildAccessJSONWindowsParity(t,
		filepath.Join("testdb", "mdbs", "dms-0805.mdb"),
		filepath.Join("testdb", "dms", "export-format-2000", "*_org.json"))
}

// TestBuildAccessJSONDMSAccess2003WindowsParity 验证 Access 2003 格式的
// dms-0812.mdb 全部窗体与 Windows Access COM 导出夹具保持逐字段一致。
func TestBuildAccessJSONDMSAccess2003WindowsParity(t *testing.T) {
	testBuildAccessJSONWindowsParity(t,
		filepath.Join("testdb", "mdbs", "dms-0812.mdb"),
		filepath.Join("testdb", "dms", "export-format-2003", "*_org.json"))
}

// TestBuildAccessJSONHTSUSWindowsParity 验证 HTSUS-0807.mdb 的全部窗体与
// Windows Access COM 导出夹具保持逐字段一致。
func TestBuildAccessJSONHTSUSWindowsParity(t *testing.T) {
	testBuildAccessJSONWindowsParity(t,
		filepath.Join("testdb", "mdbs", "HTSUS-0807.mdb"),
		filepath.Join("testdb", "HTSUS", "export", "*_org.json"))
}

// TestBuildAccessJSONIEXIC2CWindowsParity 验证 iexi-c2c.mdb 的全部窗体与
// Windows Access COM 导出夹具保持逐字段一致。
func TestBuildAccessJSONIEXIC2CWindowsParity(t *testing.T) {
	testBuildAccessJSONWindowsParity(t,
		filepath.Join("testdb", "mdbs", "iexi-c2c.mdb"),
		filepath.Join("testdb", "iexi-c2c", "export", "*_org.json"))
}

// testBuildAccessJSONWindowsParity 逐个构建指定 MDB 的窗体并与 Windows 夹具比较。
func testBuildAccessJSONWindowsParity(t *testing.T, dbPath, fixturePattern string) {
	t.Helper()
	fixturePaths, err := filepath.Glob(fixturePattern)
	if err != nil {
		t.Fatalf("glob Windows fixtures failed: %v", err)
	}
	if len(fixturePaths) == 0 {
		t.Skipf("skip integration test, no fixtures matched %s", fixturePattern)
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Skipf("skip integration test, db file not found: %s, err=%v", dbPath, err)
	}

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = db.Close() }()
	entries, err := db.ReadAccessObjectEntries()
	if err != nil {
		t.Fatalf("ReadAccessObjectEntries failed: %v", err)
	}
	builder := newAccessRawJSONBuilder(entries, db.Format.ObjectStorage)
	sort.Strings(fixturePaths)
	for _, fixturePath := range fixturePaths {
		baseName := filepath.Base(fixturePath)
		formName := strings.TrimSuffix(baseName, "_org.json")
		t.Run(formName, func(t *testing.T) {
			exported, err := builder.buildForm(formName)
			if err != nil {
				t.Fatalf("build Access JSON failed: %v", err)
			}
			rawFixture, err := os.ReadFile(fixturePath)
			if err != nil {
				t.Fatalf("read Windows fixture failed: %v", err)
			}
			var expected accessRawJSONForm
			if err := json.Unmarshal(rawFixture, &expected); err != nil {
				t.Fatalf("decode Windows fixture failed: %v", err)
			}
			if !reflect.DeepEqual(*exported, expected) {
				t.Fatal("mdbgo form differs from Windows Access export")
			}
		})
	}
}

func TestBuildAccessJSONFCVMRectanglesAgainstRawFixture(t *testing.T) {
	dbPath := filepath.Join("testdb", "mdbs", "dms-0723.mdb")
	fixturePath := filepath.Join("testdb", "f_cvm_org.json")
	if _, err := os.Stat(dbPath); err != nil {
		t.Skipf("skip integration test, db file not found: %s, err=%v", dbPath, err)
	}

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = db.Close() }()
	entries, err := db.ReadAccessObjectEntries()
	if err != nil {
		t.Fatalf("ReadAccessObjectEntries failed: %v", err)
	}
	exported, err := newAccessRawJSONBuilder(entries).buildForm("f_cvm")
	if err != nil {
		t.Fatalf("build Access JSON failed: %v", err)
	}

	rawFixture, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read raw Access fixture failed: %v", err)
	}
	var expected accessRawJSONForm
	if err := json.Unmarshal(rawFixture, &expected); err != nil {
		t.Fatalf("decode raw Access fixture failed: %v", err)
	}
	expectedByKey := accessRawControlsByKey(expected.Controls)

	rectangleNames := make(map[string]bool)
	var compareRectangles func([]accessRawJSONControl)
	compareRectangles = func(controls []accessRawJSONControl) {
		for _, control := range controls {
			if control.ClassType == "Rectangle" && !rectangleNames[control.Name] {
				rectangleNames[control.Name] = true
				key := accessRawControlKey(control)
				if !containsAccessRawControl(expectedByKey[key], accessRawControlScalar(control)) {
					t.Errorf("Rectangle %q differs from raw Access export: %+v", control.Name, control)
				}
			}
			compareRectangles(control.Tabs)
			compareRectangles(control.Controls)
		}
	}
	compareRectangles(exported.Controls)
	if len(rectangleNames) != 18 {
		t.Fatalf("parsed unique Rectangles=%d want=18", len(rectangleNames))
	}
}

func TestBuildControlSequenceKeepsControlsAboveTabFrameAtRoot(t *testing.T) {
	controls := []FormControlContent{
		{Name: "tabs", Type: "TabControl", Top: 420, Height: 8700, HasGeometry: true},
		{Name: "page1", Type: "TabPage", Top: 780, Height: 8243, HasGeometry: true},
		{Name: "page1_field", Type: "TextBox", Top: 900, Height: 300, HasGeometry: true},
		{Name: "page2", Type: "TabPage", Top: 780, Height: 8243, HasGeometry: true},
		{Name: "page2_field", Type: "TextBox", Top: 900, Height: 300, HasGeometry: true},
		{Name: "root_field", Type: "TextBox", Top: 60, Height: 300, HasGeometry: true},
	}
	got, err := (&accessRawJSONBuilder{}).buildControlSequence(controls)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "tabs" || got[1].Name != "root_field" {
		t.Fatalf("root controls=%+v", got)
	}
	if len(got[0].Tabs) != 2 || len(got[0].Tabs[0].Controls) != 1 ||
		len(got[0].Tabs[1].Controls) != 1 || got[0].Tabs[1].Controls[0].Name != "page2_field" {
		t.Fatalf("tab controls=%+v", got[0])
	}
}

func testBuildAccessJSONAgainstRawFixture(t *testing.T, formName, fixturePath string) {
	t.Helper()
	testBuildAccessJSONAgainstRawFixtureAtDB(t,
		filepath.Join("testdb", "mdbs", "dms.mdb"), formName, fixturePath)
}

func testBuildAccessJSONAgainstRawFixtureAtDB(t *testing.T, dbPath, formName, fixturePath string) {
	t.Helper()
	if _, err := os.Stat(dbPath); err != nil {
		t.Skipf("skip integration test, db file not found: %s, err=%v", dbPath, err)
	}
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = db.Close() }()
	entries, err := db.ReadAccessObjectEntries()
	if err != nil {
		t.Fatalf("ReadAccessObjectEntries failed: %v", err)
	}
	exported, err := newAccessRawJSONBuilder(entries).buildForm(formName)
	if err != nil {
		t.Fatalf("build Access JSON failed: %v", err)
	}

	rawFixture, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read raw Access fixture failed: %v", err)
	}
	var expected accessRawJSONForm
	if err := json.Unmarshal(rawFixture, &expected); err != nil {
		t.Fatalf("decode raw Access fixture failed: %v", err)
	}
	clearAccessRawJSONBackStyleForLegacyFixture(exported.Controls, expected.Controls)
	if exported.Name != expected.Name || exported.Title != expected.Title ||
		exported.Width != expected.Width || exported.View != expected.View ||
		exported.Source != expected.Source || exported.Height != expected.Height ||
		exported.BackGroundColor != expected.BackGroundColor {
		t.Fatalf("form properties=%+v want=%+v", exported, expected)
	}
	if len(exported.Controls) == 0 || len(expected.Controls) < len(exported.Controls) {
		t.Fatalf("semantic root controls=%d raw root controls=%d", len(exported.Controls), len(expected.Controls))
	}
	// Access COM 的 Form.Controls 会在根控件之间重复枚举 TabPage 内的对象；
	// MDBGO 输出唯一语义树。按 MDBGO 根控件名称从原生根数组中取对应条目，
	// 嵌套的 Tabs/Controls 仍保留并完整比较。
	expectedSemanticControls := make([]accessRawJSONControl, 0, len(exported.Controls))
	usedExpected := make([]bool, len(expected.Controls))
	for _, exportedRoot := range exported.Controls {
		found := false
		for i, expectedRoot := range expected.Controls {
			if usedExpected[i] || expectedRoot.Name != exportedRoot.Name || expectedRoot.ClassType != exportedRoot.ClassType {
				continue
			}
			expectedSemanticControls = append(expectedSemanticControls, expectedRoot)
			usedExpected[i] = true
			found = true
			break
		}
		if !found {
			t.Fatalf("raw Access root control %q (%s) is missing", exportedRoot.Name, exportedRoot.ClassType)
		}
	}
	if !reflect.DeepEqual(exported.Controls, expectedSemanticControls) {
		t.Fatal("mdbgo semantic control tree differs from raw Access export")
	}
}

// clearAccessRawJSONBackStyleForLegacyFixture keeps older raw exports usable.
// AccessExport only started emitting BackStyle after those fixtures were made;
// a fixture that contains at least one BackStyle remains fully strict.
func clearAccessRawJSONBackStyleForLegacyFixture(actual, expected []accessRawJSONControl) {
	if accessRawJSONControlsHaveBackStyle(expected) {
		return
	}
	var clear func([]accessRawJSONControl)
	clear = func(controls []accessRawJSONControl) {
		for i := range controls {
			controls[i].BackStyle = nil
			clear(controls[i].Tabs)
			clear(controls[i].Controls)
		}
	}
	clear(actual)
}

func accessRawJSONControlsHaveBackStyle(controls []accessRawJSONControl) bool {
	for i := range controls {
		if controls[i].BackStyle != nil ||
			accessRawJSONControlsHaveBackStyle(controls[i].Tabs) ||
			accessRawJSONControlsHaveBackStyle(controls[i].Controls) {
			return true
		}
	}
	return false
}

// testBuildAccessJSONPropertiesAgainstRawFixture verifies the persisted values
// independently of Access's runtime Page.Controls membership. The decoded
// TypeInfo/Blob records expose controls and properties but no page-owner field,
// so controls drawn over a TabPage are currently placed by physical order.
// Every native page member must still be on the same local page, and every
// native control must have identical raw values.
func testBuildAccessJSONPropertiesAgainstRawFixture(t *testing.T, formName, fixturePath string) {
	t.Helper()
	dbPath := filepath.Join("testdb", "mdbs", "dms.mdb")
	if _, err := os.Stat(dbPath); err != nil {
		t.Skipf("skip integration test, db file not found: %s, err=%v", dbPath, err)
	}
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = db.Close() }()
	entries, err := db.ReadAccessObjectEntries()
	if err != nil {
		t.Fatalf("ReadAccessObjectEntries failed: %v", err)
	}
	exported, err := newAccessRawJSONBuilder(entries).buildForm(formName)
	if err != nil {
		t.Fatalf("build Access JSON failed: %v", err)
	}

	rawFixture, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read raw Access fixture failed: %v", err)
	}
	var expected accessRawJSONForm
	if err := json.Unmarshal(rawFixture, &expected); err != nil {
		t.Fatalf("decode raw Access fixture failed: %v", err)
	}
	if exported.Name != expected.Name || exported.Title != expected.Title ||
		exported.Width != expected.Width || exported.View != expected.View ||
		exported.Source != expected.Source || exported.Height != expected.Height ||
		exported.BackGroundColor != expected.BackGroundColor {
		t.Fatalf("form properties=%+v want=%+v", exported, expected)
	}

	actualByKey := accessRawControlsByKey(exported.Controls)
	expectedByKey := accessRawControlsByKey(expected.Controls)
	verified := 0
	for key, expectedControls := range expectedByKey {
		actualControls := actualByKey[key]
		if len(actualControls) == 0 {
			expectedControl := expectedControls[0]
			t.Fatalf("native control %q (%s) is missing", expectedControl.Name, expectedControl.ClassType)
		}
		for _, expectedControl := range expectedControls {
			if !containsAccessRawControl(actualControls, expectedControl) {
				t.Fatalf("raw properties differ for %s %q", expectedControl.ClassType, expectedControl.Name)
			}
			verified++
		}
	}
	if verified < 350 {
		t.Fatalf("only %d native controls were verified", verified)
	}

	actualTab := findAccessRawJSONControl(exported.Controls, "TabCtl989")
	expectedTab := findAccessRawJSONControl(expected.Controls, "TabCtl989")
	if actualTab == nil || expectedTab == nil {
		t.Fatal("TabCtl989 is missing")
	}
	for _, expectedPage := range expectedTab.Tabs {
		var actualPage *accessRawJSONControl
		for i := range actualTab.Tabs {
			if actualTab.Tabs[i].Name == expectedPage.Name {
				actualPage = &actualTab.Tabs[i]
				break
			}
		}
		if actualPage == nil {
			t.Fatalf("TabPage %q is missing", expectedPage.Name)
		}
		actualPageControls := accessRawControlsByKey(actualPage.Controls)
		for _, expectedControl := range expectedPage.Controls {
			key := accessRawControlKey(expectedControl)
			if !containsAccessRawControl(actualPageControls[key], accessRawControlScalar(expectedControl)) {
				t.Fatalf("native page control %q (%s) is missing or differs on page %q",
					expectedControl.Name, expectedControl.ClassType, expectedPage.Name)
			}
		}
	}
}

func accessRawControlsByKey(controls []accessRawJSONControl) map[string][]accessRawJSONControl {
	result := make(map[string][]accessRawJSONControl)
	var appendControls func([]accessRawJSONControl)
	appendControls = func(items []accessRawJSONControl) {
		for _, control := range items {
			scalar := accessRawControlScalar(control)
			key := accessRawControlKey(scalar)
			result[key] = append(result[key], scalar)
			appendControls(control.Tabs)
			appendControls(control.Controls)
		}
	}
	appendControls(controls)
	return result
}

func accessRawControlScalar(control accessRawJSONControl) accessRawJSONControl {
	control.Tabs = nil
	control.Controls = nil
	return control
}

func accessRawControlKey(control accessRawJSONControl) string {
	return control.ClassType + "\x00" + control.Name
}

func containsAccessRawControl(controls []accessRawJSONControl, want accessRawJSONControl) bool {
	for _, control := range controls {
		if reflect.DeepEqual(control, want) {
			return true
		}
	}
	return false
}

func findAccessRawJSONControl(controls []accessRawJSONControl, name string) *accessRawJSONControl {
	for i := range controls {
		if controls[i].Name == name {
			return &controls[i]
		}
		if found := findAccessRawJSONControl(controls[i].Tabs, name); found != nil {
			return found
		}
		if found := findAccessRawJSONControl(controls[i].Controls, name); found != nil {
			return found
		}
	}
	return nil
}

func exportAllFormsJSON(t *testing.T, dbPath string) (map[string]json.RawMessage, error) {
	t.Helper()
	db, err := OpenPureGo(dbPath)
	if err != nil {
		return nil, fmt.Errorf("OpenPureGo(%q) failed: %w", dbPath, err)
	}
	defer db.Close()

	entries, err := db.ReadAccessObjectEntries()
	if err != nil {
		return nil, fmt.Errorf("ReadAccessObjectEntries failed: %w", err)
	}

	formIDs, err := formStorageIDsFromEntries(entries)
	if err != nil {
		return nil, fmt.Errorf("formStorageIDsFromEntries failed: %w", err)
	}

	formNames := make([]string, 0, len(formIDs))
	for name := range formIDs {
		formNames = append(formNames, name)
	}
	sort.Strings(formNames)

	builder := newAccessRawJSONBuilder(entries)
	result := make(map[string]json.RawMessage, len(formNames))
	for _, name := range formNames {
		form, err := builder.buildForm(name)
		if err != nil {
			return nil, fmt.Errorf("buildForm(%q) failed: %w", name, err)
		}
		data, err := json.Marshal(form)
		if err != nil {
			return nil, fmt.Errorf("json.Marshal(%q) failed: %w", name, err)
		}
		result[name] = data
	}
	return result, nil
}

func exportAllFormsJSONWithOpen(t *testing.T, dbPath string) (map[string]json.RawMessage, error) {
	t.Helper()
	db, err := Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("Open(%q) failed: %w", dbPath, err)
	}
	defer db.Close()

	entries, err := db.ReadAccessObjectEntries()
	if err != nil {
		return nil, fmt.Errorf("ReadAccessObjectEntries failed: %w", err)
	}

	formIDs, err := formStorageIDsFromEntries(entries)
	if err != nil {
		return nil, fmt.Errorf("formStorageIDsFromEntries failed: %w", err)
	}

	formNames := make([]string, 0, len(formIDs))
	for name := range formIDs {
		formNames = append(formNames, name)
	}
	sort.Strings(formNames)

	builder := newAccessRawJSONBuilder(entries)
	result := make(map[string]json.RawMessage, len(formNames))
	for _, name := range formNames {
		form, err := builder.buildForm(name)
		if err != nil {
			return nil, fmt.Errorf("buildForm(%q) failed: %w", name, err)
		}
		data, err := json.Marshal(form)
		if err != nil {
			return nil, fmt.Errorf("json.Marshal(%q) failed: %w", name, err)
		}
		result[name] = data
	}
	return result, nil
}

func TestPureGoExportAllFormsAccess2000Vs2003(t *testing.T) {
	dbPaths := []struct {
		name string
		path string
	}{
		{"Access 2000 MSysAccessObjects", "testdb/mdbs/mpci_2000.mdb"},
		{"Access 2003 MSysAccessStorage", "testdb/mdbs/mpci_2003.mdb"},
	}
	results := make(map[string]map[string]json.RawMessage)
	for _, p := range dbPaths {
		if _, err := os.Stat(p.path); err != nil {
			t.Skipf("fixture not found: %s", p.path)
			return
		}
		forms, err := exportAllFormsJSON(t, p.path)
		if err != nil {
			t.Fatalf("[%s] %v", p.name, err)
		}
		results[p.name] = forms
		t.Logf("[%s] 共 %d 个窗体，全部导出成功", p.name, len(forms))
	}

	ref := results["Access 2000 MSysAccessObjects"]
	got := results["Access 2003 MSysAccessStorage"]

	allNames := make(map[string]bool)
	for name := range ref {
		allNames[name] = true
	}
	for name := range got {
		allNames[name] = true
	}

	names := make([]string, 0, len(allNames))
	for name := range allNames {
		names = append(names, name)
	}
	sort.Strings(names)

	diffCount := 0
	matchCount := 0
	for _, name := range names {
		refData, refOK := ref[name]
		gotData, gotOK := got[name]

		if !refOK {
			t.Logf("注意: 窗体 %q 仅在 2003 中存在", name)
			continue
		}
		if !gotOK {
			t.Logf("注意: 窗体 %q 仅在 2000 中存在", name)
			continue
		}

		if bytes.Equal(refData, gotData) {
			matchCount++
		} else {
			diffCount++
			var refRaw, gotRaw accessRawJSONForm
			json.Unmarshal(refData, &refRaw)
			json.Unmarshal(gotData, &gotRaw)

			var diffs []string
			if refRaw.Title != gotRaw.Title {
				diffs = append(diffs, fmt.Sprintf("Title=%q→%q", refRaw.Title, gotRaw.Title))
			}
			if refRaw.Source != gotRaw.Source {
				diffs = append(diffs, fmt.Sprintf("Source=%q→%q", refRaw.Source, gotRaw.Source))
			}
			if refRaw.View != gotRaw.View {
				diffs = append(diffs, fmt.Sprintf("View=%d→%d", refRaw.View, gotRaw.View))
			}
			if refRaw.Width != gotRaw.Width {
				diffs = append(diffs, fmt.Sprintf("Width=%d→%d", refRaw.Width, gotRaw.Width))
			}
			if refRaw.Height != gotRaw.Height {
				diffs = append(diffs, fmt.Sprintf("Height=%d→%d", refRaw.Height, gotRaw.Height))
			}
			if refRaw.BackGroundColor != gotRaw.BackGroundColor {
				diffs = append(diffs, fmt.Sprintf("BackGroundColor=%d→%d", refRaw.BackGroundColor, gotRaw.BackGroundColor))
			}
			if diffCount <= 15 {
				t.Logf("窗体 %q 差异: %s", name, strings.Join(diffs, ", "))
			}
		}
	}

	t.Logf("统计: %d 个完全一致, %d 个有元数据差异（Access 版本间自然差异）, 共 %d 个窗体",
		matchCount, diffCount, len(names))
}

func TestPureGoVsCGOExportAllForms(t *testing.T) {
	testDBs := []struct {
		name string
		path string
	}{
		{"mpci_2000.mdb (MSysAccessObjects)", "testdb/mdbs/mpci_2000.mdb"},
		{"mpci_2003.mdb (MSysAccessStorage)", "testdb/mdbs/mpci_2003.mdb"},
	}

	for _, d := range testDBs {
		if _, err := os.Stat(d.path); err != nil {
			t.Skipf("fixture not found: %s", d.path)
			return
		}

		t.Run(d.name, func(t *testing.T) {
			cgoForms, err := exportAllFormsJSONWithOpen(t, d.path)
			if err != nil {
				t.Fatalf("CGO export failed: %v", err)
			}

			pureForms, err := exportAllFormsJSON(t, d.path)
			if err != nil {
				t.Fatalf("PureGo export failed: %v", err)
			}

			if len(cgoForms) != len(pureForms) {
				t.Fatalf("窗体数量不一致: CGO=%d, PureGo=%d", len(cgoForms), len(pureForms))
			}
			t.Logf("窗体数: %d", len(cgoForms))

			allNames := make(map[string]bool)
			for name := range cgoForms {
				allNames[name] = true
			}
			for name := range pureForms {
				allNames[name] = true
			}
			names := make([]string, 0, len(allNames))
			for name := range allNames {
				names = append(names, name)
			}
			sort.Strings(names)

			diffCount := 0
			matchCount := 0
			for _, name := range names {
				cgoData, cgoOK := cgoForms[name]
				pureData, pureOK := pureForms[name]

				if !cgoOK {
					t.Errorf("窗体 %q 仅在 PureGo 中存在", name)
					continue
				}
				if !pureOK {
					t.Errorf("窗体 %q 仅在 CGO 中存在", name)
					continue
				}

				if bytes.Equal(cgoData, pureData) {
					matchCount++
				} else {
					diffCount++
					if diffCount <= 10 {
						diffFields := compareFormJSON(cgoData, pureData)
						t.Errorf("窗体 %q CGO 与 PureGo 输出不一致:\n  %s", name, strings.Join(diffFields, "\n  "))
					}
				}
			}

			if diffCount == 0 {
				t.Logf("CGO vs PureGo: %d 个窗体完全一致", matchCount)
			} else {
				t.Errorf("CGO vs PureGo: %d 个一致, %d 个不一致", matchCount, diffCount)
			}
		})
	}
}

func compareFormJSON(a, b []byte) []string {
	var formA, formB accessRawJSONForm
	json.Unmarshal(a, &formA)
	json.Unmarshal(b, &formB)

	var diffs []string
	addDiff := func(field string, vA, vB interface{}) {
		diffs = append(diffs, fmt.Sprintf("%s: CGO=%v  PureGo=%v", field, vA, vB))
	}

	if formA.Name != formB.Name {
		addDiff("Name", formA.Name, formB.Name)
	}
	if formA.Title != formB.Title {
		addDiff("Title", formA.Title, formB.Title)
	}
	if formA.Source != formB.Source {
		addDiff("Source", formA.Source, formB.Source)
	}
	if formA.View != formB.View {
		addDiff("View", formA.View, formB.View)
	}
	if formA.Width != formB.Width {
		addDiff("Width", formA.Width, formB.Width)
	}
	if formA.Height != formB.Height {
		addDiff("Height", formA.Height, formB.Height)
	}
	if formA.BackGroundColor != formB.BackGroundColor {
		addDiff("BackGroundColor", formA.BackGroundColor, formB.BackGroundColor)
	}

	if len(formA.Controls) != len(formB.Controls) {
		addDiff("Controls数量", len(formA.Controls), len(formB.Controls))
	} else {
		for i := range formA.Controls {
			ca, cb := formA.Controls[i], formB.Controls[i]
			if ca.Name != cb.Name {
				addDiff(fmt.Sprintf("Controls[%d].Name", i), ca.Name, cb.Name)
				break
			}
			compareControlJSON(&ca, &cb, fmt.Sprintf("Controls[%d]", i), &diffs, addDiff)
		}
	}
	return diffs
}

func compareControlJSON(a, b *accessRawJSONControl, prefix string, diffs *[]string, addDiff func(string, interface{}, interface{})) {
	if a.ClassType != b.ClassType {
		addDiff(prefix+".ClassType", a.ClassType, b.ClassType)
	}
	if a.Name != b.Name {
		addDiff(prefix+".Name", a.Name, b.Name)
	}
	if a.Width != b.Width {
		addDiff(prefix+".Width", a.Width, b.Width)
	}
	if a.Height != b.Height {
		addDiff(prefix+".Height", a.Height, b.Height)
	}
	if a.Top != b.Top {
		addDiff(prefix+".Top", a.Top, b.Top)
	}
	if a.Left != b.Left {
		addDiff(prefix+".Left", a.Left, b.Left)
	}
	if a.Source != b.Source {
		addDiff(prefix+".Source", a.Source, b.Source)
	}
	if a.Text != b.Text {
		addDiff(prefix+".Text", a.Text, b.Text)
	}
	if len(a.Controls) != len(b.Controls) {
		addDiff(prefix+".Controls数量", len(a.Controls), len(b.Controls))
	} else {
		for i := range a.Controls {
			compareControlJSON(&a.Controls[i], &b.Controls[i], fmt.Sprintf("%s.Controls[%d]", prefix, i), diffs, addDiff)
		}
	}
	if len(a.Tabs) != len(b.Tabs) {
		addDiff(prefix+".Tabs数量", len(a.Tabs), len(b.Tabs))
	} else {
		for i := range a.Tabs {
			compareControlJSON(&a.Tabs[i], &b.Tabs[i], fmt.Sprintf("%s.Tabs[%d]", prefix, i), diffs, addDiff)
		}
	}
}
