package pgtest

import (
	"archive/zip"
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
		fallbackInstall = installPostgresViaMaven
	} else {
		install = installPostgresViaMaven
	}

	if os.Getenv("PGTEST_FORCE_MAVEN") == "true" {
		t.Logf("forcing maven installation for postgres version %q", version)
		install = installPostgresViaMaven
		fallbackInstall = nil
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

func installPostgresViaMaven(log logger, version string) (string, error) {
	system, err := deriveSystem(runtime.GOOS)
	if err != nil {
		return "", err
	}

	arch, err := deriveArchitecture(runtime.GOARCH)
	if err != nil {
		return "", err
	}

	path := filepath.Join(Root, version)
	if err := os.MkdirAll(path, 0o755); err != nil {
		return "", fmt.Errorf("creating working directory: %w", err)
	}

	if err := download(log,
		filepath.Join(path, "download"),
		"https://repo1.maven.org/maven2/io/zonky/test/postgres/embedded-postgres-binaries-"+system+"-"+arch+"/"+version+"/embedded-postgres-binaries-"+system+"-"+arch+"-"+version+".jar",
		"postgres.jar"); err != nil {
		return "", fmt.Errorf("download postgres: %w", err)
	}

	if err := extractTarGzFromJar(
		filepath.Join(path, "download", "postgres.jar"),
		filepath.Join(path, "unjar", "postgres.tar.xz")); err != nil {
		return "", fmt.Errorf("extract tar from jar: %w", err)
	}

	if err := execute(
		filepath.Join(path, "unpacked"),
		"tar", "xf", "../unjar/postgres.tar.xz"); err != nil {
		return "", fmt.Errorf("unpack postgres: %w", err)
	}

	return filepath.Join(path, "unpacked"), nil
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
		return "arm64v8", nil

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

func execute(directory string, command ...string) error {
	return atomicOperation(directory, func(directory string) error {
		fmt.Println("Run shell command: ", strings.Join(command, " "))

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

func extractTarGzFromJar(jar, tar string) error {
	target := filepath.Dir(tar)

	return atomicOperation(target, func(tempTarget string) error {
		fmt.Println("Extract file from jar:", jar)

		jar, err := zip.OpenReader(jar)
		if err != nil {
			return fmt.Errorf("open postgres.jar file: %w", err)
		}

		defer jar.Close()

		for _, file := range jar.File {
			// just pick the biggest file
			if file.UncompressedSize64 > 4*1024*1024 {
				r, err := file.Open()
				if err != nil {
					return fmt.Errorf("unpack jar entry: %w", err)
				}

				//goland:noinspection ALL
				defer r.Close()

				if err := writeTo(filepath.Join(tempTarget, filepath.Base(tar)), r); err != nil {
					return fmt.Errorf("unpack jar entry: %w", err)
				}

				return nil
			}
		}

		return nil
	})
}

func writeTo(target string, reader io.Reader) error {
	fp, err := os.Create(target)
	if err != nil {
		return fmt.Errorf("open file at %s: %w", target, err)
	}

	defer fp.Close()

	_, err = io.Copy(fp, reader)
	if err != nil {
		return fmt.Errorf("copy to file %s: %w", target, err)
	}

	return nil
}
