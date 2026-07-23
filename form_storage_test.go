package mdbgo

import (
	"bytes"
	"testing"
)

func TestReadAccessObjectDataAllMatchesDirectRead(t *testing.T) {
	dbPath := requireDBFile(t)
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	objects, err := db.readAccessObjectDataAll()
	if err != nil {
		t.Fatalf("readAccessObjectDataAll failed: %v", err)
	}
	if len(objects) == 0 {
		t.Fatal("readAccessObjectDataAll returned no rows")
	}

	indexes := []int{0, len(objects) / 2, len(objects) - 1}
	for _, index := range indexes {
		got := objects[index]
		want, err := db.ReadAccessObjectDataByID(got.ObjectID)
		if err != nil {
			t.Fatalf("ReadAccessObjectDataByID(%d) failed: %v", got.ObjectID, err)
		}
		if !bytes.Equal(got.Data, want.Data) {
			t.Fatalf("bulk data for id=%d differs: got=%d bytes want=%d bytes", got.ObjectID, len(got.Data), len(want.Data))
		}
	}
}
