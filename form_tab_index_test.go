package mdbgo

import "testing"

// TestNormalizeExpandedTabIndexesByPage 验证逐页修复不受页数、其他容器或合法手工顺序影响。
func TestNormalizeExpandedTabIndexesByPage(t *testing.T) {
	cases := []struct {
		name     string
		controls []FormControlInfo
		values   map[string]int
		want     map[string]int
		missing  string
	}{
		{
			name:     "three pages preserve manual order and repair only damaged page",
			controls: []FormControlInfo{{Type: "TabControl"}, {Type: "TabPage"}, {Type: "TextBox", Name: "manual_a"}, {Type: "TextBox", Name: "manual_b"}, {Type: "TabPage"}, {Type: "TextBox", Name: "broken_a"}, {Type: "TextBox", Name: "broken_b"}, {Type: "TabPage"}, {Type: "TextBox", Name: "last"}},
			values:   map[string]int{"manual_a": 1, "manual_b": 0, "broken_a": 0, "broken_b": 0, "last": 0},
			want:     map[string]int{"manual_a": 1, "manual_b": 0, "broken_a": 0, "broken_b": 1, "last": 0},
		},
		{
			name:     "multiple tab containers",
			controls: []FormControlInfo{{Type: "TabControl"}, {Type: "TabPage"}, {Type: "TextBox", Name: "a"}, {Type: "TextBox", Name: "b"}, {Type: "TabControl"}, {Type: "TabPage"}, {Type: "TextBox", Name: "c"}, {Type: "TextBox", Name: "d"}},
			values:   map[string]int{"a": 0, "b": 0, "c": 1, "d": 0},
			want:     map[string]int{"a": 0, "b": 1, "c": 1, "d": 0},
		},
		{
			name:     "sparse manual order retains relative order",
			controls: []FormControlInfo{{Type: "TabControl"}, {Type: "TabPage"}, {Type: "TextBox", Name: "a"}, {Type: "TextBox", Name: "b"}, {Type: "TextBox", Name: "c"}},
			values:   map[string]int{"a": 4, "b": 0, "c": 2},
			want:     map[string]int{"a": 2, "b": 0, "c": 1},
		},
		{
			name:     "missing numeric record leaves its page untouched",
			controls: []FormControlInfo{{Type: "TabControl"}, {Type: "TabPage"}, {Type: "TextBox", Name: "a"}, {Type: "TextBox", Name: "missing"}, {Type: "TextBox", Name: "b"}, {Type: "TabPage"}, {Type: "TextBox", Name: "c"}, {Type: "TextBox", Name: "d"}},
			values:   map[string]int{"a": 4, "b": 2, "c": 0, "d": 0},
			want:     map[string]int{"a": 4, "b": 2, "c": 0, "d": 1},
		},
		{
			name:     "unknown focus ownership leaves its page untouched",
			controls: []FormControlInfo{{Type: "TabControl"}, {Type: "TabPage"}, {Type: "TextBox", Name: "a"}, {Type: "OptionButton", Name: "option"}, {Type: "TextBox", Name: "b"}},
			values:   map[string]int{"a": 4, "b": 2},
			want:     map[string]int{"a": 4, "b": 2},
		},
		{
			name:     "section ends page",
			controls: []FormControlInfo{{Type: "TabControl"}, {Type: "TabPage"}, {Type: "TextBox", Name: "a"}, {Type: "TextBox", Name: "b"}, {Type: "FormFooter", TypeCode: 0x189A}, {Type: "TextBox", Name: "footer"}},
			values:   map[string]int{"a": 0, "b": 0, "footer": 9},
			want:     map[string]int{"a": 0, "b": 1, "footer": 9},
		},
		{
			name:     "unlocated page prevents guessing membership",
			controls: []FormControlInfo{{Type: "TabControl"}, {Type: "TabPage", Name: "missing_page"}, {Type: "TextBox", Name: "a"}, {Type: "TextBox", Name: "b"}},
			values:   map[string]int{"a": 0, "b": 0},
			want:     map[string]int{"a": 0, "b": 0}, missing: "missing_page",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			textBoxes := map[string]jet4FormNumericProperties{}
			for name, value := range tc.values {
				textBoxes[name] = jet4FormNumericProperties{TabIndex: value, HasTabIndex: true}
			}
			offsets := make([]int, len(tc.controls))
			for i := range tc.controls {
				tc.controls[i].Index = uint32(i)
				offsets[i] = 10 * (i + 1)
				if tc.missing != "" && tc.controls[i].Name == tc.missing {
					offsets[i] = -1
				}
			}
			normalizeJet4TabIndexes(tc.controls, offsets, textBoxes, nil, nil, nil, nil, nil, nil, nil, true)
			for name, want := range tc.want {
				if got := textBoxes[name].TabIndex; got != want {
					t.Errorf("%s TabIndex=%d want=%d", name, got, want)
				}
			}
		})
	}
}

// TestNormalizeExpandedTabIndexesCountsImplicitControls 验证默认序号控件参与页内集合判定。
func TestNormalizeExpandedTabIndexesCountsImplicitControls(t *testing.T) {
	controls := []FormControlInfo{{Type: "TabControl", Name: "tabs"}, {Type: "TabPage"}, {Type: "SubForm", Name: "sub"}, {Type: "Button", Name: "button"}, {Type: "CheckBox", Name: "check"}, {Type: "TextBox", Name: "count"}, {Type: "TextBox", Name: "root"}}
	offsets := []int{10, 20, 30, 40, 50, 60, 70}
	textBoxes := map[string]jet4FormNumericProperties{"count": {Geometry: formControlGeometry{Top: 200}, HasGeometry: true}, "root": {TabIndex: 8, HasTabIndex: true, Geometry: formControlGeometry{Top: 0}, HasGeometry: true}}
	buttons := map[string]jet4ButtonNumericProperties{"button": {}}
	checkBoxes := map[string]jet4CheckBoxNumericProperties{"check": {}}
	subForms := map[string]jet4SubFormNumericProperties{"sub": {}}
	tabs := map[string]jet4TabControlNumericProperties{"tabs": {Geometry: formControlGeometry{Top: 100}, HasGeometry: true}}
	normalizeJet4TabIndexes(controls, offsets, textBoxes, nil, buttons, checkBoxes, nil, nil, subForms, tabs, true)
	if got := textBoxes["count"].TabIndex; got != 3 {
		t.Errorf("count TabIndex=%d want=3", got)
	}
	if got := textBoxes["root"].TabIndex; got != 8 {
		t.Errorf("root TabIndex=%d want=8", got)
	}
	if buttons["button"].TabIndex != 1 || checkBoxes["check"].TabIndex != 2 {
		t.Fatal("省略序号的 Button 或 CheckBox 未占号")
	}
}
