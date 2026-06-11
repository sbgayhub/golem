package plugin

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

func TestDiscoverPluginExecutablesRecursively(t *testing.T) {
	oldPluginDir := pluginDir
	pluginDir = t.TempDir()
	t.Cleanup(func() {
		pluginDir = oldPluginDir
	})

	writePluginCandidate(t, pluginDir, "root", true)
	writePluginCandidate(t, filepath.Join(pluginDir, "nested", "deep"), "nested", true)
	writePluginCandidate(t, filepath.Join(pluginDir, "nested"), "non_exec", false)
	writeFile(t, filepath.Join(pluginDir, "nested", executableFileName("ignored")), 0755)

	executables, err := discoverPluginExecutables()
	if err != nil {
		t.Fatalf("discover plugin executables: %v", err)
	}

	names := make([]string, 0, len(executables))
	for _, executable := range executables {
		names = append(names, executable.name)
	}
	slices.Sort(names)

	want := []string{"nested", "root"}
	if !slices.Equal(names, want) {
		t.Fatalf("unexpected plugin names: got %v, want %v", names, want)
	}
}

func TestDiscoverPluginExecutablesIgnoresMissingPluginDir(t *testing.T) {
	oldPluginDir := pluginDir
	pluginDir = filepath.Join(t.TempDir(), "missing")
	t.Cleanup(func() {
		pluginDir = oldPluginDir
	})

	executables, err := discoverPluginExecutables()
	if err != nil {
		t.Fatalf("discover plugin executables: %v", err)
	}
	if len(executables) != 0 {
		t.Fatalf("missing plugin dir should not discover executables: %v", executables)
	}
}

func TestPluginNameFromExecutable(t *testing.T) {
	tests := map[string]string{
		"golem_plugin_example":     "example",
		"golem_plugin_example.exe": "example",
		"golem_plugin_example.CMD": "example",
	}

	for fileName, want := range tests {
		if got := pluginNameFromExecutable(fileName); got != want {
			t.Fatalf("pluginNameFromExecutable(%q) = %q, want %q", fileName, got, want)
		}
	}
}

func writePluginCandidate(t *testing.T, dir, name string, executable bool) string {
	t.Helper()

	fileName := "golem_plugin_" + name
	if runtime.GOOS == "windows" && executable {
		fileName += ".exe"
	}

	mode := os.FileMode(0644)
	if executable {
		mode = 0755
	}
	return writeFile(t, filepath.Join(dir, fileName), mode)
}

func executableFileName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func writeFile(t *testing.T, path string, mode os.FileMode) string {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("create parent dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("test"), mode); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, mode); err != nil {
			t.Fatalf("chmod file: %v", err)
		}
	}
	return path
}
