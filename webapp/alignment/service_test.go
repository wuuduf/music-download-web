package alignment

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
		"WebAlignmentEnabled": true,
		"WebAlignmentPython":  "/definitely/missing/python",
		"WebAlignmentScript":  "/definitely/missing/worker.py",
	})
	capability := service.Capabilities()
	if !capability.Enabled || capability.Ready || capability.Reason == "" {
		t.Fatalf("unexpected capability: %+v", capability)
	}
}
