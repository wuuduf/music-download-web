package stems

import "testing"

type fakeConfig map[string]any

func (f fakeConfig) GetString(key string) string {
	value, _ := f[key].(string)
	return value
}
func (f fakeConfig) GetInt(key string) int {
	value, _ := f[key].(int)
	return value
}
func (f fakeConfig) GetBool(key string) bool {
	value, _ := f[key].(bool)
	return value
}

func TestCapabilitiesDisabledByDefault(t *testing.T) {
	service := New(nil)
	capability := service.Capabilities()
	if capability.Enabled || capability.Ready || capability.Reason == "" {
		t.Fatalf("unexpected capability: %+v", capability)
	}
}

func TestCapabilitiesReportsMissingRuntime(t *testing.T) {
	service := New(fakeConfig{
		"WebStemEnabled": true,
		"WebStemPython":  "/definitely/missing/python",
		"WebStemScript":  "/definitely/missing/worker.py",
	})
	capability := service.Capabilities()
	if !capability.Enabled || capability.Ready || capability.Reason == "" {
		t.Fatalf("unexpected capability: %+v", capability)
	}
}

func TestSafeAssetPathRejectsTraversal(t *testing.T) {
	if _, err := safeAssetPath(t.TempDir(), "../secret.wav"); err == nil {
		t.Fatal("expected traversal to be rejected")
	}
}

func TestSafeAssetPathAcceptsChild(t *testing.T) {
	root := t.TempDir()
	path, err := safeAssetPath(root, "vocals.wav")
	if err != nil || path == "" {
		t.Fatalf("unexpected result: path=%q err=%v", path, err)
	}
}
