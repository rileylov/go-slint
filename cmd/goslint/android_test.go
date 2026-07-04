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
		"arm64-v8a", "", "", "", "", "android", "androiddebugkey", "android", "")
	if def.pkg != "/tmp/cool" || def.label != "cool" || def.appID != "dev.goslint.cool" || def.out != "cool.apk" {
		t.Errorf("defaults: %+v", def)
	}

	// Explicit flags win over the derived defaults.
	ov := resolveAndroidCfg("/tmp/cool", "Out.apk", "com.x.y", "My Label", "2.0", 5, 21, 33,
		"x86_64", "Manifest.xml", "/sdk", "/ndk", "/ks", "pw", "alias", "kp", "POST_NOTIFICATIONS")
	if ov.out != "Out.apk" || ov.appID != "com.x.y" || ov.label != "My Label" ||
		ov.abiList != "x86_64" || ov.manifestArg != "Manifest.xml" || ov.keystore != "/ks" {
		t.Errorf("overrides: %+v", ov)
	}

	// Empty package resolves to "." (current directory).
	if cur := resolveAndroidCfg("", "", "", "", "1.0", 1, 24, 34, "arm64-v8a", "", "", "", "", "android", "androiddebugkey", "android", ""); cur.pkg != "." {
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
	m := genManifest("dev.goslint.demo", "Demo", 1, "1.0", 24, 34, []string{"android.permission.POST_NOTIFICATIONS"})
	for _, want := range []string{
		`package="dev.goslint.demo"`,
		`android:label="Demo"`,
		`android:minSdkVersion="24"`,
		`android.app.lib_name`, `goslint`,
		"android.app.NativeActivity",
		`<uses-permission android:name="android.permission.POST_NOTIFICATIONS" />`,
	} {
		if !strings.Contains(m, want) {
			t.Errorf("manifest missing %q", want)
		}
	}
}

// TestNaturalLess pins the version-aware ordering that keeps latestDir from
// picking "9.0.0" over "35.0.1" (a plain string sort does exactly that).
func TestNaturalLess(t *testing.T) {
	less := []struct{ a, b string }{
		{"android-9", "android-10"},
		{"9.0.0", "35.0.1"},
		{"28.0.3", "35.0.1"},
		{"1.2", "1.10"},
		{"ndk/25.2.9519653", "ndk/29.0.14206865"},
		{"abc", "abd"},
	}
	for _, c := range less {
		if !naturalLess(c.a, c.b) {
			t.Errorf("naturalLess(%q, %q) = false, want true", c.a, c.b)
		}
		if naturalLess(c.b, c.a) {
			t.Errorf("naturalLess(%q, %q) = true, want false", c.b, c.a)
		}
	}
}

// TestLatestDirNaturalOrder checks the real-world shapes: old build-tools or an
// old platform installed alongside the current one must not win.
func TestLatestDirNaturalOrder(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"build-tools/9.0.0", "build-tools/28.0.3", "build-tools/35.0.1"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(d)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	got, err := latestDir(filepath.Join(root, "build-tools"), "*")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got) != "35.0.1" {
		t.Errorf("latestDir picked %s, want 35.0.1", filepath.Base(got))
	}

	for _, d := range []string{"platforms/android-9", "platforms/android-34"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(d)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	got, err = latestDir(filepath.Join(root, "platforms"), "android-*")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got) != "android-34" {
		t.Errorf("latestDir picked %s, want android-34", filepath.Base(got))
	}
}

// TestToolPathFor checks the Windows extension resolution: apksigner is a .bat
// (CreateProcess only auto-appends .exe, so the extension must be explicit),
// aapt2 is an .exe, and non-Windows keeps bare names.
func TestToolPathFor(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"apksigner.bat", "aapt2.exe", "zipalign"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if got := toolPathFor(dir, "apksigner", "windows"); filepath.Base(got) != "apksigner.bat" {
		t.Errorf("windows apksigner -> %s, want apksigner.bat", got)
	}
	if got := toolPathFor(dir, "aapt2", "windows"); filepath.Base(got) != "aapt2.exe" {
		t.Errorf("windows aapt2 -> %s, want aapt2.exe", got)
	}
	if got := toolPathFor(dir, "zipalign", "linux"); filepath.Base(got) != "zipalign" {
		t.Errorf("linux zipalign -> %s, want zipalign", got)
	}
	// missing tool: return the plain path so exec fails with something concrete
	if got := toolPathFor(dir, "nope", "windows"); filepath.Base(got) != "nope" {
		t.Errorf("missing tool -> %s, want nope", got)
	}
}

// TestJDKBinDirs checks the keytool search order: JAVA_HOME first, then Android
// Studio's bundled JDK, newest vendor JDK before older ones.
func TestJDKBinDirs(t *testing.T) {
	dirs := jdkBinDirs("windows", `C:\jdk`, `C:\PF`, "")
	if len(dirs) < 2 || dirs[0] != filepath.Join(`C:\jdk`, "bin") {
		t.Errorf("JAVA_HOME must be first, got %v", dirs)
	}
	if dirs[1] != filepath.Join(`C:\PF`, "Android", "Android Studio", "jbr", "bin") {
		t.Errorf("Android Studio jbr must follow JAVA_HOME, got %v", dirs)
	}
	if dirs := jdkBinDirs("linux", "", "", "/home/u"); len(dirs) < 2 || dirs[0] != "/opt/android-studio/jbr/bin" {
		t.Errorf("linux candidates wrong: %v", dirs)
	}
}

// TestFindSDKWindowsDefault: %LOCALAPPDATA%\Android\Sdk is where Android Studio
// puts the SDK on Windows — it must resolve without -sdk or ANDROID_HOME.
func TestFindSDKWindowsDefault(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // keep any real ~/android-sdk out of the way
	t.Setenv("ANDROID_HOME", "")
	t.Setenv("ANDROID_SDK_ROOT", "")
	lad := t.TempDir()
	sdk := filepath.Join(lad, "Android", "Sdk")
	if err := os.MkdirAll(filepath.Join(sdk, "build-tools", "35.0.1"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOCALAPPDATA", lad)
	if got := findSDK(""); got != sdk {
		t.Errorf("findSDK() = %q, want the LOCALAPPDATA default %q", got, sdk)
	}
}

// TestJavaEnv pins the apksigner JVM handoff: with JAVA_HOME set or java on PATH
// nothing is added; with only a keytool findable (e.g. Android Studio's bundled
// JDK), JAVA_HOME is derived from it so apksigner's launcher script finds java.
func TestJavaEnv(t *testing.T) {
	// Already configured -> no overrides.
	t.Setenv("JAVA_HOME", "/some/jdk")
	if got := javaEnv(); got != nil {
		t.Errorf("with JAVA_HOME set, javaEnv() = %v, want nil", got)
	}

	// No JAVA_HOME, no java on PATH, keytool findable -> derive JAVA_HOME.
	t.Setenv("JAVA_HOME", "")
	t.Setenv("HOME", t.TempDir()) // keep real JDK install dirs out of the search
	jdk := filepath.Join(t.TempDir(), "jdk")
	bin := filepath.Join(jdk, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	kt := filepath.Join(bin, exeName(runtime.GOOS, "keytool"))
	if err := os.WriteFile(kt, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin) // keytool resolvable, java NOT
	got := javaEnv()
	if len(got) != 1 || got[0] != "JAVA_HOME="+jdk {
		t.Errorf("javaEnv() = %v, want [JAVA_HOME=%s]", got, jdk)
	}
}

// TestNormalizePermissions: short names get the android.permission. prefix,
// qualified names pass through, whitespace and empties are dropped.
func TestNormalizePermissions(t *testing.T) {
	got := normalizePermissions(" POST_NOTIFICATIONS, BLUETOOTH_SCAN ,com.example.CUSTOM,, ")
	want := []string{
		"android.permission.POST_NOTIFICATIONS",
		"android.permission.BLUETOOTH_SCAN",
		"com.example.CUSTOM",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	if p := normalizePermissions(""); p != nil {
		t.Errorf("empty input -> %v, want nil", p)
	}
}
