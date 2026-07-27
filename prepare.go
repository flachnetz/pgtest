package pgtest

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofrs/flock"
)

type Config struct {
	Binary   string
	Snapshot string
	Workdir  string
}

var installCache sync.Map

// Install installs the default postgres version.
// This respects the PGTEST_VERSION environment variable
func Install(t testing.TB) (Config, error) {
	version := Version
	if v := os.Getenv("PGTEST_VERSION"); v != "" {
		version = v
	}

	cachedConfig, ok := installCache.Load(version)
	if ok {
		return cachedConfig.(Config), nil
	}

	config, err := doInstallVersion(t, version)
	if err != nil {
		return Config{}, fmt.Errorf("install postgres %q: %w", version, err)
	}

	installCache.Store(version, config)

	return config, nil
}

// InstallVersion installs a specific postgres version
func doInstallVersion(t testing.TB, version string) (Config, error) {
	var install func(_ logger, version string) (string, error)
	var fallbackInstall func(_ logger, version string) (string, error)

	// find the best way to install postgres
	useNix := hasNixShell()
	if useNix {
		install = installViaNixStore
		fallbackInstall = installPostgresFromGitHub
	} else {
		install = installPostgresFromGitHub
	}

	// install postgres
	path, err := install(t, version)
	if err != nil {
		t.Logf("failed to install postgres version %s: %s", version, err.Error())

		if fallbackInstall != nil {
			t.Logf("falling back to alternative installation method")
			path, err = fallbackInstall(t, version)
		}

		if err != nil {
			return Config{}, fmt.Errorf("install postgres version %s: %w", version, err)
		}
	}

	binary := filepath.Join(path, "/bin/postgres")
	initdb := filepath.Join(path, "/bin/initdb")

	// get the actual version from the binary
	output, err := exec.Command(binary, "--version").Output()
	if err != nil {
		return Config{}, fmt.Errorf("get version: %w", err)
	}

	actualVersion := regexp.
		MustCompile(`\b(\d\d[.]\d+)\b`).
		FindString(strings.TrimSpace(string(output)))

	if actualVersion == "" {
		return Config{}, fmt.Errorf("failed to get postgres version: %q", string(output))
	}

	if actualVersion != version {
		t.Logf("Actual version %q not the requested version %q", actualVersion, version)
		version = actualVersion
	}

	root := filepath.Join(Root, version)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return Config{}, fmt.Errorf("creating working directory: %w", err)
	}

	snapshot := filepath.Join(Root, version, "initdb")

	if err := execute(
		t,
		snapshot,
		initdb, "-U", "postgres", "-D", "pgdata", "--no-sync"); err != nil {
		return Config{}, fmt.Errorf("initialize pgdata snapshot: %w", err)
	}

	config := Config{
		Binary:   binary,
		Snapshot: snapshot,
		Workdir:  filepath.Join(Root, version),
	}

	return config, nil
}

func installPostgresFromGitHub(log logger, version string) (string, error) {
	system, err := deriveSystem(runtime.GOOS)
	if err != nil {
		return "", err
	}

	arch, err := deriveArchitecture(runtime.GOARCH)
	if err != nil {
		return "", err
	}

	url := postgresDownloadURL(version, system, arch, linuxDistro())

	path := filepath.Join(Root, version)
	if err := os.MkdirAll(path, 0o755); err != nil {
		return "", fmt.Errorf("creating working directory: %w", err)
	}

	if err := download(log,
		filepath.Join(path, "download"),
		url,
		"postgres.tar.gz"); err != nil {
		return "", fmt.Errorf("download postgres: %w", err)
	}

	if err := execute(
		log,
		filepath.Join(path, "unpacked"),
		"tar", "xzf", "../download/postgres.tar.gz"); err != nil {
		return "", fmt.Errorf("unpack postgres: %w", err)
	}

	return filepath.Join(path, "unpacked"), nil
}

func postgresDownloadURL(version, system, arch, distro string) string {
	// derive the release version tag: if version has two parts (e.g. "18.4"), append ".0"
	releaseVersion := version
	if strings.Count(version, ".") == 1 {
		releaseVersion = version + ".0"
	}

	var assetName string
	if system == "linux" {
		assetName = "postgres-linux-" + distro + "-" + arch + ".tar.gz"
	} else {
		assetName = "postgres-" + system + "-" + arch + ".tar.gz"
	}

	return "https://github.com/flachnetz/embedded-postgres-binaries/releases/download/v" + releaseVersion + "/" + assetName
}

// linuxDistro detects whether we're on alpine or debian-based linux.
func linuxDistro() string {
	data, err := os.ReadFile("/etc/os-release")
	if err == nil && strings.Contains(string(data), "alpine") {
		return "alpine"
	}
	return "debian"
}

func hasNixShell() bool {
	err := exec.Command("which", "nix-shell").Run()
	return err == nil
}

func installViaNixStore(_ logger, version string) (string, error) {
	if idx := strings.IndexByte(version, '.'); idx > 0 {
		version = version[:idx]
	}

	// get path to postgres binary
	pkg := fmt.Sprintf("postgresql_%s", version)
	output, err := exec.Command("nix-shell", "-p", pkg, "--run", "which postgres").Output()
	if err != nil {
		return "", fmt.Errorf("get postgres path: %w", err)
	}

	// cleanup path and remove binary
	path := strings.TrimSpace(string(output))
	path = strings.TrimSuffix(path, "/bin/postgres")
	return path, nil
}

func deriveArchitecture(arch string) (string, error) {
	switch arch {
	case "amd64":
		return "amd64", nil

	case "arm64":
		return "arm64", nil

	default:
		return "", fmt.Errorf("unsupported arch: %q", arch)
	}
}

func deriveSystem(system string) (string, error) {
	switch system {
	case "darwin", "linux":
		return system, nil

	default:
		return "", fmt.Errorf("unsupported system %q", system)
	}
}

func atomicOperation(target string, op func(tempTarget string) error) error {
	lock := flock.New(target + ".lock")
	defer func() { _ = lock.Close() }()

	if err := lock.Lock(); err != nil {
		return fmt.Errorf("get lock for download: %w", err)
	}

	defer lock.Unlock()

	// check if file already exists
	if _, err := os.Stat(target); err == nil {
		return nil
	}

	targetTemp := fmt.Sprintf("%s.%d", target, time.Now().UnixNano())
	defer os.RemoveAll(targetTemp)

	if err := os.MkdirAll(targetTemp, 0o755); err != nil {
		return fmt.Errorf("creating temporary directory: %w", err)
	}

	if err := op(targetTemp); err != nil {
		return err
	}

	// do an atomic rename to target file
	return os.Rename(targetTemp, target)
}

func execute(log logger, directory string, command ...string) error {
	return atomicOperation(directory, func(directory string) error {
		log.Log("Run shell command: ", strings.Join(command, " "))

		cmd := exec.Command(command[0], command[1:]...)
		cmd.Dir = directory

		cmdOutput, cmdErr := cmd.CombinedOutput()
		if cmdErr != nil {
			return fmt.Errorf("execute command %q in %q, output %q: %w", strings.Join(command, " "), directory, string(cmdOutput), cmdErr)
		}
		return nil
	})
}

func download(log logger, directory, url, name string) error {
	return atomicOperation(directory, func(target string) error {
		log.Log("Download: ", url)

		resp, err := http.DefaultClient.Get(url)
		if err != nil {
			return fmt.Errorf("request to %s: %w", url, err)
		}

		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("download failed with status %d: %q", resp.StatusCode, string(body))
		}

		// write the partial download to a temporary file
		fp, err := os.Create(filepath.Join(target, name))
		if err != nil {
			return fmt.Errorf("open temporary target file: %w", err)
		}

		defer fp.Close()

		if _, err := io.CopyBuffer(fp, resp.Body, make([]byte, 64*1024)); err != nil {
			return fmt.Errorf("download response into file: %w", err)
		}

		return nil
	})
}
