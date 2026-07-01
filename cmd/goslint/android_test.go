package main

import "os"
import "path/filepath"
import "runtime"
import "strings"
import "testing"

// TestFindSDK checks the build-tools validation: a dir that looks like an SDK
// resolves, and one without build-tools is not accepted as that path.
func TestFindSDK(t *testing.T) {
	sdk := t.TempDir()
	if err := os.MkdirAll(filepath.Join(sdk, "build-tools", "35.0.1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := findSDK(sdk); got != sdk { // -sdk is checked first, so this is deterministic
		t.Errorf("findSDK(valid sdk) = %q, want %q", got, sdk)
	}
	if bare := t.TempDir(); findSDK(bare) == bare {
		t.Errorf("findSDK accepted %q, which has no build-tools", bare)
	}
}

// TestPkgManagerSDKs checks the package-manager fallback paths so a Homebrew/apt
// install resolves without ANDROID_HOME.
func TestPkgManagerSDKs(t *testing.T) {
	var want string
	switch runtime.GOOS {
	case "darwin":
		want = "/opt/homebrew/share/android-commandlinetools"
	case "linux":
		want = "/usr/lib/android-sdk"
	default:
		return // no package-manager fallbacks on other platforms
	}
	got := pkgManagerSDKs()
	for _, p := range got {
		if p == want {
			return
		}
	}
	t.Errorf("pkgManagerSDKs() = %v, want it to include %q", got, want)
}

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

// TestResolveAndroidCfg checks the defaults `goslint android build`/`dev` derive from
// the package (name/application-id/label/output), and that explicit values win.
func TestResolveAndroidCfg(t *testing.T) {
	// Defaults derive from the package base name (absolute path -> deterministic base).
	def := resolveAndroidCfg("/tmp/cool", "", "", "", "1.0", 1, 24, 34,
		"arm64-v8a", "", "", "", "", "android", "androiddebugkey", "android")
	if def.pkg != "/tmp/cool" || def.label != "cool" || def.appID != "dev.goslint.cool" || def.out != "cool.apk" {
		t.Errorf("defaults: %+v", def)
	}

	// Explicit flags win over the derived defaults.
	ov := resolveAndroidCfg("/tmp/cool", "Out.apk", "com.x.y", "My Label", "2.0", 5, 21, 33,
		"x86_64", "Manifest.xml", "/sdk", "/ndk", "/ks", "pw", "alias", "kp")
	if ov.out != "Out.apk" || ov.appID != "com.x.y" || ov.label != "My Label" ||
		ov.abiList != "x86_64" || ov.manifestArg != "Manifest.xml" || ov.keystore != "/ks" {
		t.Errorf("overrides: %+v", ov)
	}

	// Empty package resolves to "." (current directory).
	if cur := resolveAndroidCfg("", "", "", "", "1.0", 1, 24, 34, "arm64-v8a", "", "", "", "", "android", "androiddebugkey", "android"); cur.pkg != "." {
		t.Errorf("empty pkg -> %q, want %q", cur.pkg, ".")
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
