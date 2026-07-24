package mdbgo

import (
	"bytes"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
)

func TestCompactJet4ExpandedFields(t *testing.T) {
	expanded := []byte{
		0x62, 0x00, 0x00, 0x00, // compact tag
		0x96, 0x00, // Access property ID (Width)
		0x03, 0x00, 0x00, 0x00, // Long
		0x04, 0x00, 0x00, 0x00, // element size
		0x02, 0x00, 0x00, 0x00, // stored byte length
		0x28, 0x32, // 12840 twips
		0xDC, 0x00, 0x00, 0x00,
		0x9C, 0x00,
		0x0C, 0x00, 0x00, 0x00,
		0x04, 0x00, 0x00, 0x00,
		0x06, 0x00, 0x00, 0x00,
		'v', 0, '_', 0, 'x', 0,
		0xE3, 0x00, 0x00, 0x00,
		0xBC, 0x00,
		0x0B, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0x03, 0x00, 0x00, 0x00,
		0x01, 0x02, 0x03,
	}
	got, end, ok := compactJet4ExpandedFields(expanded, 0)
	if !ok {
		t.Fatal("compactJet4ExpandedFields did not recognize expanded fields")
	}
	if end != len(expanded) {
		t.Fatalf("end=%d want=%d", end, len(expanded))
	}
	want := []byte{
		0x62, 0x28, 0x32,
		0xDC, 0x06, 'v', 0, '_', 0, 'x', 0,
		0xE3, 0x03, 0x01, 0x02, 0x03,
	}
	if string(got) != string(want) {
		t.Fatalf("compact fields=% x want=% x", got, want)
	}
}

func TestAccess2003FormContentMatchesAccess2000(t *testing.T) {
	const (
		access2000Path = "testdb/mdbs/mpci_2000.mdb"
		access2003Path = "testdb/mdbs/mpci_2003.mdb"
	)
	for _, path := range []string{access2000Path, access2003Path} {
		if _, err := os.Stat(path); err != nil {
			t.Skipf("fixture not found: %s, err=%v", path, err)
		}
	}

	openForm := func(t *testing.T, path, formName string) *FormContent {
		t.Helper()
		db, err := Open(path)
		if err != nil {
			t.Fatalf("Open(%s) failed: %v", path, err)
		}
		t.Cleanup(func() { _ = db.Close() })
		content, err := db.ReadFormContent(formName)
		if err != nil {
			t.Fatalf("ReadFormContent(%s, %s) failed: %v", path, formName, err)
		}
		return content
	}

	want := openForm(t, access2000Path, "f_doc_query")
	got := openForm(t, access2003Path, "f_doc_query")
	if got.Width != want.Width || got.Height != want.Height ||
		got.DefaultView != want.DefaultView || got.RecordSource != want.RecordSource ||
		got.Caption != want.Caption {
		t.Fatalf("2003 form=(width=%d height=%d view=%d source=%q caption=%q), want 2000=(%d %d %d %q %q)",
			got.Width, got.Height, got.DefaultView, got.RecordSource, got.Caption,
			want.Width, want.Height, want.DefaultView, want.RecordSource, want.Caption)
	}

	wantControls := make(map[string]FormControlContent, len(want.Controls))
	for _, control := range want.Controls {
		wantControls[strings.ToLower(control.Name)] = control
	}
	for _, control := range got.Controls {
		expected, ok := wantControls[strings.ToLower(control.Name)]
		if !ok {
			t.Fatalf("2003 contains unexpected control %q", control.Name)
		}
		if control.Left != expected.Left || control.Top != expected.Top ||
			control.Width != expected.Width || control.Height != expected.Height ||
			control.HasGeometry != expected.HasGeometry {
			t.Errorf("control %q geometry=(%d,%d,%d,%d,%v), want=(%d,%d,%d,%d,%v)",
				control.Name, control.Left, control.Top, control.Width, control.Height, control.HasGeometry,
				expected.Left, expected.Top, expected.Width, expected.Height, expected.HasGeometry)
		}
		if control.Type == "SubForm" && control.SourceObject != expected.SourceObject {
			t.Errorf("SubForm %q SourceObject=%q want=%q",
				control.Name, control.SourceObject, expected.SourceObject)
		}
	}
	exportJSON := func(t *testing.T, path, formName string) []byte {
		t.Helper()
		db, err := Open(path)
		if err != nil {
			t.Fatalf("Open(%s) failed: %v", path, err)
		}
		t.Cleanup(func() { _ = db.Close() })
		entries, err := db.ReadAccessObjectEntries()
		if err != nil {
			t.Fatalf("ReadAccessObjectEntries(%s) failed: %v", path, err)
		}
		exported, err := newAccessRawJSONBuilder(entries).buildForm(formName)
		if err != nil {
			t.Fatalf("buildForm(%s, %s) failed: %v", path, formName, err)
		}
		data, err := marshalAccessRawJSONIndent(exported)
		if err != nil {
			t.Fatalf("marshal %s from %s failed: %v", formName, path, err)
		}
		return data
	}
	for _, formName := range []string{"f_doc_query", "f_oem_hbl"} {
		wantJSON := exportJSON(t, access2000Path, formName)
		gotJSON := exportJSON(t, access2003Path, formName)
		if !bytes.Equal(gotJSON, wantJSON) {
			t.Fatalf("Access 2003 %s JSON differs from Access 2000", formName)
		}
	}

	openContents := func(t *testing.T, path string) []FormContent {
		t.Helper()
		db, err := Open(path)
		if err != nil {
			t.Fatalf("Open(%s) failed: %v", path, err)
		}
		t.Cleanup(func() { _ = db.Close() })
		contents, err := db.ExportFormContents()
		if err != nil {
			t.Fatalf("ExportFormContents(%s) failed: %v", path, err)
		}
		return contents
	}
	wantContents := openContents(t, access2000Path)
	gotContents := openContents(t, access2003Path)
	if len(gotContents) != len(wantContents) {
		t.Fatalf("2003 forms=%d want 2000 forms=%d", len(gotContents), len(wantContents))
	}
	if len(gotContents) != 66 {
		t.Fatalf("fixture forms=%d want=66", len(gotContents))
	}

	mismatchesByType := make(map[string]int)
	mismatchesByForm := make(map[string]int)
	samples := make([]string, 0, 20)
	totalControls := 0
	for formIndex := range wantContents {
		wantForm := &wantContents[formIndex]
		gotForm := &gotContents[formIndex]
		if !strings.EqualFold(gotForm.FormName, wantForm.FormName) {
			t.Fatalf("form %d name=%q want=%q", formIndex, gotForm.FormName, wantForm.FormName)
		}
		if gotForm.Width != wantForm.Width || gotForm.Height != wantForm.Height {
			mismatchesByType["Form"]++
			mismatchesByForm[gotForm.FormName]++
			if len(samples) < cap(samples) {
				samples = append(samples, fmt.Sprintf(
					"%s Form size=(%d,%d) want=(%d,%d)",
					gotForm.FormName, gotForm.Width, gotForm.Height, wantForm.Width, wantForm.Height))
			}
		}
		if len(gotForm.Controls) != len(wantForm.Controls) {
			t.Fatalf("form %q controls=%d want=%d",
				gotForm.FormName, len(gotForm.Controls), len(wantForm.Controls))
		}
		totalControls += len(gotForm.Controls)
		wantControlsByKey := make(map[string]*FormControlContent, len(wantForm.Controls))
		for controlIndex := range wantForm.Controls {
			control := &wantForm.Controls[controlIndex]
			key := strings.ToLower(control.Type) + "\x00" + strings.ToLower(control.Name)
			if _, exists := wantControlsByKey[key]; exists {
				t.Fatalf("form %q has duplicate 2000 control key %s/%q",
					wantForm.FormName, control.Type, control.Name)
			}
			wantControlsByKey[key] = control
		}
		for controlIndex := range gotForm.Controls {
			gotControl := &gotForm.Controls[controlIndex]
			key := strings.ToLower(gotControl.Type) + "\x00" + strings.ToLower(gotControl.Name)
			wantControl := wantControlsByKey[key]
			if wantControl == nil {
				t.Fatalf("form %q 2003 control %d=%s/%q is missing from 2000",
					gotForm.FormName, controlIndex, gotControl.Type, gotControl.Name)
			}
			if gotControl.Left == wantControl.Left && gotControl.Top == wantControl.Top &&
				gotControl.Width == wantControl.Width && gotControl.Height == wantControl.Height &&
				gotControl.HasGeometry == wantControl.HasGeometry {
				continue
			}
			mismatchesByType[gotControl.Type]++
			mismatchesByForm[gotForm.FormName]++
			if len(samples) < cap(samples) {
				samples = append(samples, fmt.Sprintf(
					"%s/%s %s geometry=(%d,%d,%d,%d,%v) want=(%d,%d,%d,%d,%v)",
					gotForm.FormName, gotControl.Name, gotControl.Type,
					gotControl.Left, gotControl.Top, gotControl.Width, gotControl.Height, gotControl.HasGeometry,
					wantControl.Left, wantControl.Top, wantControl.Width, wantControl.Height, wantControl.HasGeometry))
			}
		}
	}
	if len(mismatchesByType) > 0 {
		types := make([]string, 0, len(mismatchesByType))
		for controlType := range mismatchesByType {
			types = append(types, controlType)
		}
		sort.Strings(types)
		counts := make([]string, 0, len(types))
		for _, controlType := range types {
			counts = append(counts, fmt.Sprintf("%s=%d", controlType, mismatchesByType[controlType]))
		}
		forms := make([]string, 0, len(mismatchesByForm))
		for formName := range mismatchesByForm {
			forms = append(forms, formName)
		}
		sort.Strings(forms)
		formCounts := make([]string, 0, len(forms))
		for _, formName := range forms {
			formCounts = append(formCounts, fmt.Sprintf("%s=%d", formName, mismatchesByForm[formName]))
		}
		t.Fatalf("2003 geometry mismatches: %s; forms: %s; samples: %s",
			strings.Join(counts, ", "), strings.Join(formCounts, ", "), strings.Join(samples, "; "))
	}
	if totalControls != 1713 {
		t.Fatalf("fixture controls=%d want=1713", totalControls)
	}
}
