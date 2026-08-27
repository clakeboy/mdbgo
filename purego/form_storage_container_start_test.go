package purego

import (
	"bytes"
	"testing"
)

// TestAccessObjectContainerStartPrefersFragmentBoundary 验证主容器定位不会误选较早记录中的内嵌 CFB 签名。
func TestAccessObjectContainerStartPrefersFragmentBoundary(t *testing.T) {
	compoundMagic := []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}
	objects := []*AccessObjectData{
		{ObjectID: 0, Data: append([]byte("embedded-prefix"), compoundMagic...)},
		{ObjectID: 1, Data: append(append([]byte(nil), compoundMagic...), []byte("main-container")...)},
	}

	index, offset := accessObjectContainerStart(objects, compoundMagic)
	if index != 1 || offset != 0 {
		t.Fatalf("主容器起点=(%d, %d)，期望=(1, 0)", index, offset)
	}
}

// TestAccessObjectContainerStartFallsBackToEmbeddedSignature 验证旧格式仍可从分片内部的签名开始重组。
func TestAccessObjectContainerStartFallsBackToEmbeddedSignature(t *testing.T) {
	compoundMagic := []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}
	prefix := []byte("legacy-prefix")
	objects := []*AccessObjectData{
		{ObjectID: 0, Data: append(append([]byte(nil), prefix...), compoundMagic...)},
	}

	index, offset := accessObjectContainerStart(objects, compoundMagic)
	if index != 0 || offset != len(prefix) {
		t.Fatalf("兼容起点=(%d, %d)，期望=(0, %d)", index, offset, len(prefix))
	}
	if !bytes.Equal(objects[index].Data[offset:offset+len(compoundMagic)], compoundMagic) {
		t.Fatal("兼容起点没有指向 CFB 签名")
	}
}
