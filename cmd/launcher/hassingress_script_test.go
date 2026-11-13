package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// requireScriptTools skips when the host lacks a tool the script needs, except
// in CI (CI is set), where a skip would hide the test, so it fails instead.
func requireScriptTools(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"bash", "wget", "unzip", "sha256sum"} {
		if _, err := exec.LookPath(tool); err != nil {
			if os.Getenv("CI") != "" {
				t.Fatalf("%s not found in CI: %v", tool, err)
			}
			t.Skipf("%s not found: %v", tool, err)
		}
	}
}

// makeZip builds a zip holding files (name → content), in name order.
func makeZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, n := range names {
		w, err := zw.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(w, files[n]); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sha256Hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// serveZip serves body at /ingress.zip and 404 everywhere else.
func serveZip(t *testing.T, body []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ingress.zip" {
			http.NotFound(w, r)
			return
		}
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// runScript runs the real install script with bash on the host and returns its
// stderr and error. no_proxy keeps a host proxy away from the local server.
func runScript(t *testing.T, configDir, tmpDir, url string, size int64, sha string) (string, error) {
	t.Helper()
	return runScriptEnv(t, configDir, tmpDir, url, size, sha)
}

// runScriptEnv is runScript with extra environment entries (a later PATH wins).
func runScriptEnv(t *testing.T, configDir, tmpDir, url string, size int64, sha string, extraEnv ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("bash", "-c", buildHassIngressScript(configDir, tmpDir, url, size, sha))
	cmd.Env = append(os.Environ(), "no_proxy=127.0.0.1,localhost", "NO_PROXY=127.0.0.1,localhost")
	cmd.Env = append(cmd.Env, extraEnv...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stderr.String(), err
}

func stagingDirs(t *testing.T, configDir string) []string {
	t.Helper()
	m, err := filepath.Glob(filepath.Join(configDir, ".snap-hass-ingress.*"))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// checkNoResidue: the run left no staging dir of its own and an empty tmpDir.
func checkNoResidue(t *testing.T, configDir, tmpDir string, stagingBefore []string) {
	t.Helper()
	if got := stagingDirs(t, configDir); strings.Join(got, ",") != strings.Join(stagingBefore, ",") {
		t.Errorf("staging dirs = %q, want %q", got, stagingBefore)
	}
	left, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Errorf("tmpDir not empty: %v", left)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

const testManifest = `{"domain": "ingress", "version": "9.9.9"}`

// writeOld creates configDir/custom_components/ingress/old.txt.
func writeOld(t *testing.T, configDir string) {
	t.Helper()
	dir := filepath.Join(configDir, "custom_components", "ingress")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "old.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// s1, s2, s7, s8: the script installs, replaces, quotes paths, and ignores a
// leftover staging dir from an interrupted run.
func TestHassIngressScriptInstalls(t *testing.T) {
	requireScriptTools(t)
	body := makeZip(t, map[string]string{"manifest.json": testManifest, "marker.txt": "new"})
	srv := serveZip(t, body)
	cases := []struct {
		name     string
		subdir   string // configDir below t.TempDir()
		old      bool   // an existing custom_components/ingress with old.txt
		leftover bool   // a .snap-hass-ingress.* from an interrupted run
	}{
		{"s1 fresh", "", false, false},
		{"s2 replace", "", true, false},
		{"s7 space and quote in the path", "my config's dir", true, false}, // Review Focus 1
		{"s8 leftover staging dir", "", true, true},                        // Review Focus 2
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			configDir := filepath.Join(t.TempDir(), tc.subdir)
			if err := os.MkdirAll(configDir, 0o755); err != nil {
				t.Fatal(err)
			}
			tmpDir := t.TempDir()
			if tc.old {
				writeOld(t, configDir)
			}
			if tc.leftover {
				if err := os.Mkdir(filepath.Join(configDir, ".snap-hass-ingress.ABC123"), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			before := stagingDirs(t, configDir)
			stderr, err := runScript(t, configDir, tmpDir, srv.URL+"/ingress.zip", int64(len(body)), sha256Hex(body))
			if err != nil {
				t.Fatalf("script: %v\nstderr: %s", err, stderr)
			}
			dest := filepath.Join(configDir, "custom_components", "ingress")
			if got := readFile(t, filepath.Join(dest, "manifest.json")); got != testManifest {
				t.Errorf("manifest.json = %q, want %q", got, testManifest)
			}
			if got := readFile(t, filepath.Join(dest, "marker.txt")); got != "new" {
				t.Errorf("marker.txt = %q, want new", got)
			}
			if _, err := os.Stat(filepath.Join(dest, "old.txt")); !os.IsNotExist(err) {
				t.Errorf("old.txt still there (err %v)", err)
			}
			checkNoResidue(t, configDir, tmpDir, before)
		})
	}
}

// s3-s6: every failed check exits non-zero and leaves the existing copy as it was.
func TestHassIngressScriptFailsClosed(t *testing.T) {
	requireScriptTools(t)
	body := makeZip(t, map[string]string{"manifest.json": testManifest, "marker.txt": "new"})
	wrapped := makeZip(t, map[string]string{"custom_components/ingress/manifest.json": testManifest})
	srv := serveZip(t, body)
	wsrv := serveZip(t, wrapped)
	cases := []struct {
		name, url string
		size      int64
		sha       string
		wantErr   string
	}{
		{"s3 wrong sha", srv.URL + "/ingress.zip", int64(len(body)), sha256Hex([]byte("other bytes")), "sha256 mismatch"},
		{"s4 wrong size", srv.URL + "/ingress.zip", int64(len(body)) + 1, sha256Hex(body), "size mismatch"},
		{"s5 http 404", srv.URL + "/missing.zip", int64(len(body)), sha256Hex(body), "download failed"},
		{"s6 manifest not at the root", wsrv.URL + "/ingress.zip", int64(len(wrapped)), sha256Hex(wrapped), "manifest.json not at the zip root"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			configDir, tmpDir := t.TempDir(), t.TempDir()
			writeOld(t, configDir)
			before := stagingDirs(t, configDir)
			stderr, err := runScript(t, configDir, tmpDir, tc.url, tc.size, tc.sha)
			if err == nil {
				t.Fatalf("script succeeded, want failure; stderr: %s", stderr)
			}
			if !strings.Contains(stderr, tc.wantErr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr, tc.wantErr)
			}
			dest := filepath.Join(configDir, "custom_components", "ingress")
			if got := readFile(t, filepath.Join(dest, "old.txt")); got != "old" {
				t.Errorf("old.txt = %q, want old", got)
			}
			if _, err := os.Stat(filepath.Join(dest, "manifest.json")); !os.IsNotExist(err) {
				t.Errorf("manifest.json appeared in the existing copy (err %v)", err)
			}
			checkNoResidue(t, configDir, tmpDir, before)
		})
	}
}

// fakeMvPATH returns a PATH whose first entry holds an `mv` stand-in. The
// stand-in's body gets $real, the real mv, as a shell variable.
func fakeMvPATH(t *testing.T, body string) string {
	t.Helper()
	real, err := exec.LookPath("mv")
	if err != nil {
		t.Skipf("mv not found: %v", err)
	}
	bin := t.TempDir()
	script := "#!/bin/bash\nreal=" + shellQuote(real) + "\n" + body
	if err := os.WriteFile(filepath.Join(bin, "mv"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return "PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH")
}

// s9, s10: an interrupted or failed swap puts the old copy back (D3).
func TestHassIngressScriptSwapInterrupted(t *testing.T) {
	requireScriptTools(t)
	body := makeZip(t, map[string]string{"manifest.json": testManifest})
	srv := serveZip(t, body)
	cases := []struct {
		name, mv, wantErr string
	}{
		// TERM hits the script's bash right after the old copy is moved aside.
		{"s9 TERM between the two renames", `"$real" "$@" || exit $?
case "${@: -1}" in */old) kill -TERM $PPID ;; esac
`, ""},
		// The move of the new copy into place fails, so the rollback runs.
		{"s10 move into place fails", `case "$1" in */new) exit 1 ;; esac
exec "$real" "$@"
`, "previous copy restored"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			configDir, tmpDir := t.TempDir(), t.TempDir()
			writeOld(t, configDir)
			stderr, err := runScriptEnv(t, configDir, tmpDir, srv.URL+"/ingress.zip", int64(len(body)), sha256Hex(body), fakeMvPATH(t, tc.mv))
			if err == nil {
				t.Fatalf("script succeeded, want failure; stderr: %s", stderr)
			}
			if !strings.Contains(stderr, tc.wantErr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr, tc.wantErr)
			}
			dest := filepath.Join(configDir, "custom_components", "ingress")
			if got := readFile(t, filepath.Join(dest, "old.txt")); got != "old" {
				t.Errorf("old.txt = %q, want old", got)
			}
			if _, err := os.Stat(filepath.Join(dest, "manifest.json")); !os.IsNotExist(err) {
				t.Errorf("manifest.json of the new copy appeared in the dest (err %v)", err)
			}
		})
	}
}

// T5: run by hand when bumping the pin; CI never hits the network.
func TestHassIngressPinOnline(t *testing.T) {
	if os.Getenv("HASS_INGRESS_PIN_ONLINE") != "1" {
		t.Skip("set HASS_INGRESS_PIN_ONLINE=1 to download the pinned asset")
	}
	resp, err := http.Get(hassIngressPinURL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: %s", hassIngressPinURL, resp.Status)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != hassIngressPinSize {
		t.Errorf("size = %d, want %d", len(b), hassIngressPinSize)
	}
	if got := sha256Hex(b); got != hassIngressPinSHA256 {
		t.Errorf("sha256 = %s, want %s", got, hassIngressPinSHA256)
	}
}
