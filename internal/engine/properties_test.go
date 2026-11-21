package engine

import "testing"

func TestReadWriteProperties(t *testing.T) {
	dir := t.TempDir()
	props := DefaultJavaProperties()
	props["motd"] = "SekaiLink Test"
	props["max-players"] = "42"

	if err := WriteProperties(dir, props); err != nil {
		t.Fatalf("WriteProperties failed: %v", err)
	}
	loaded, err := ReadProperties(dir)
	if err != nil {
		t.Fatalf("ReadProperties failed: %v", err)
	}
	if loaded["motd"] != "SekaiLink Test" || loaded["max-players"] != "42" {
		t.Fatalf("unexpected loaded properties: %+v", loaded)
	}
}
