package main

import "os"
import "path/filepath"
import "strings"
import "testing"

// TestEnsureAndroidEntry checks that the on-demand Android entry point is written
// when missing (so `goslint android build` works on a desktop-only scaffold) and
// never clobbers an entry the user already has.
func TestEnsureAndroidEntry(t *testing.T) {
	dir := t.TempDir()
	entry := filepath.Join(dir, "android_main.go")

	// missing -> created from the template, gated on the custom (non-GOOS) tag
	if err := ensureAndroidEntry(dir); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(entry)
	if err != nil {
		t.Fatalf("entry not created: %v", err)
	}
	for _, want := range []string{"//go:build goslint_android", "//export goslint_android_main", "func main()"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("generated entry missing %q", want)
		}
	}
	// the name must NOT end in _<goos>, or the GOOS filename rule re-introduces the
	// android cross-build view we're avoiding.
	if strings.HasSuffix(entry, "_android.go") {
		t.Errorf("entry %q ends in _android.go — that implies GOOS=android by filename", entry)
	}

	// already present -> left untouched (don't overwrite it)
	if err := os.WriteFile(entry, []byte("//go:build goslint_android\n//export goslint_android_main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureAndroidEntry(dir); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(entry); !strings.HasPrefix(string(b), "//go:build goslint_android\n//export") {
		t.Errorf("ensureAndroidEntry clobbered an existing entry: %q", b)
	}

	// an entry under a DIFFERENT filename is still detected (no duplicate created)
	dir2 := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir2, "myentry.go"), []byte("//export goslint_android_main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureAndroidEntry(dir2); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(dir2, "android_main.go")) {
		t.Error("created android_main.go even though an entry already exported the symbol")
	}
}

func TestSelectABIs(t *testing.T) {
	got, err := selectABIs("arm64-v8a,x86_64")
	if err != nil || len(got) != 2 {
		t.Fatalf("both ABIs: %v err=%v", got, err)
	}
	if one, err := selectABIs("arm64-v8a"); err != nil || len(one) != 1 || one[0].triple != "aarch64-linux-android" {
		t.Fatalf("single ABI: %v err=%v", one, err)
	}
	if _, err := selectABIs("mips"); err == nil {
		t.Fatal("expected error for unknown ABI")
	}
}

func TestSanitizeID(t *testing.T) {
	for in, want := range map[string]string{
		"My App!": "myapp",
		"interop": "interop",
		"":        "app",
		"123-x":   "123x",
	} {
		if got := sanitizeID(in); got != want {
			t.Errorf("sanitizeID(%q)=%q want %q", in, got, want)
		}
	}
}

func TestGenManifest(t *testing.T) {
	m := genManifest("dev.goslint.demo", "Demo", 1, "1.0", 24, 34)
	for _, want := range []string{
		`package="dev.goslint.demo"`,
		`android:label="Demo"`,
		`android:minSdkVersion="24"`,
		`android.app.lib_name`, `goslint`,
		"android.app.NativeActivity",
	} {
		if !strings.Contains(m, want) {
			t.Errorf("manifest missing %q", want)
		}
	}
}
