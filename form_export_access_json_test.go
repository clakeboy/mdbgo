package mdbgo

import (
	"bytes"
	"encoding/json"
	"errors"
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
	entries []AccessObjectEntry
	cache   map[string]*FormContent
	active  map[string]bool
}

func newAccessRawJSONBuilder(entries []AccessObjectEntry) *accessRawJSONBuilder {
	return &accessRawJSONBuilder{
		entries: entries,
		cache:   make(map[string]*FormContent),
		active:  make(map[string]bool),
	}
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

func TestBuildAccessJSONFAbiaMaster(t *testing.T) {
	db, err := Open(requireDBFile(t))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = db.Close() }()
	entries, err := db.ReadAccessObjectEntries()
	if err != nil {
		t.Fatalf("ReadAccessObjectEntries failed: %v", err)
	}
	exported, err := newAccessRawJSONBuilder(entries).buildForm("f_abia_master")
	if err != nil {
		t.Fatalf("build Access JSON failed: %v", err)
	}
	if exported.Name != "f_abia_master" || exported.Title != "Query MAWB Manifest / ABI Status" {
		t.Fatalf("form identity=%+v", exported)
	}
	if exported.Height != 8640 || exported.BackGroundColor != -2147483633 {
		t.Fatalf("form visual properties=%+v", exported)
	}
	if len(exported.Controls) != 1 || exported.Controls[0].ClassType != "TabControl" ||
		len(exported.Controls[0].Tabs) != 3 {
		t.Fatalf("root controls=%d first=%+v", len(exported.Controls), exported.Controls[0])
	}
	rawFixture, err := os.ReadFile("testdb/t_abia_master_org.json")
	if err != nil {
		t.Fatalf("read raw Access fixture failed: %v", err)
	}
	var expected accessRawJSONForm
	if err := json.Unmarshal(rawFixture, &expected); err != nil {
		t.Fatalf("decode raw Access fixture failed: %v", err)
	}
	clearAccessRawJSONBackStyleForLegacyFixture(exported.Controls, expected.Controls)
	if len(expected.Controls) == 0 || !reflect.DeepEqual(exported.Controls[0], expected.Controls[0]) {
		// Access 根级 Controls 还会重复枚举部分 COM 子对象，语义树以首个 TabControl 为准；
		// Hash 是 COM 包装对象的运行时值，解码到 accessRawJSONControl 时会自动忽略。
		t.Fatal("mdbgo semantic control tree differs from raw Access export")
	}
	wantPageControlCounts := map[string]int{
		"ManifestStatus": 30,
		"MasterInbond":   52,
		"HouseList":      6,
	}
	for _, page := range exported.Controls[0].Tabs {
		if want, ok := wantPageControlCounts[page.Name]; !ok || len(page.Controls) != want {
			t.Errorf("page %q controls=%d want=%d", page.Name, len(page.Controls), want)
		}
	}
	manifestTable := findAccessRawJSONControl(exported.Controls, "sub_abia_master_list_manifest")
	if manifestTable == nil || manifestTable.ClassType != "Table" || len(manifestTable.Controls) != 22 {
		t.Fatalf("manifest Table=%+v", manifestTable)
	}
	eventTable := findAccessRawJSONControl(exported.Controls, "sub_abia_master_list_2_event")
	if eventTable == nil || eventTable.NoWarp == nil || !*eventTable.NoWarp {
		t.Fatalf("event Table NoWarp=%+v", eventTable)
	}
	houseNumber := findAccessRawJSONControl(exported.Controls, "house_no")
	if houseNumber == nil || houseNumber.Underline == nil || !*houseNumber.Underline {
		t.Fatalf("house_no Underline=%+v", houseNumber)
	}
}

func TestBuildAccessJSONFAbiEntryLineReview(t *testing.T) {
	testBuildAccessJSONAgainstRawFixture(t,
		"f_abi_entry_line_review", filepath.Join("testdb", "f_abi_entry_line_review_org.json"))
}

func TestBuildAccessJSONFOemHblQuery(t *testing.T) {
	testBuildAccessJSONAgainstRawFixture(t,
		"f_oem_hbl_query", filepath.Join("testdb", "f_oem_hbl_query_org.json"))
}

func TestBuildAccessJSONFOemHbl(t *testing.T) {
	testBuildAccessJSONAgainstRawFixture(t,
		"f_oem_hbl", filepath.Join("testdb", "f_oem_hbl_org.json"))
}

func TestBuildAccessJSONFAbiEntry(t *testing.T) {
	testBuildAccessJSONAgainstRawFixture(t,
		"f_abi_entry", filepath.Join("testdb", "f_abi_entry_org.json"))
}

func TestBuildAccessJSONFOem(t *testing.T) {
	testBuildAccessJSONPropertiesAgainstRawFixture(t,
		"f_oem", filepath.Join("testdb", "f_oem_org.json"))
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
