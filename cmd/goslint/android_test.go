package main

import "strings"
import "testing"

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
