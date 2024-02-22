package pgtest

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/theckman/go-flock"
)

func Install() (Config, error) {
	root := filepath.Join(Root, Version)

	if err := os.MkdirAll(root, 0o755); err != nil {
		return Config{}, errors.WithMessage(err, "creating working directory")
	}

	var install func() (string, error)

	// find a way to install postgres
	if hasNixShell() {
		install = installViaNixStore
	} else {
		install = installPostgresViaMaven
	}

	// install postgres
	path, err := install()
	if err != nil {
		return Config{}, errors.WithMessage(err, "install postgres with nix")
	}

	binary := filepath.Join(path, "/bin/postgres")
	initdb := filepath.Join(path, "/bin/initdb")
	snapshot := filepath.Join(Root, Version, "initdb")

	if err := execute(
		snapshot,
		initdb, "-U", "postgres", "-D", "pgdata", "--no-sync"); err != nil {
		return Config{}, errors.WithMessage(err, "initialize pgdata snapshot")
	}

	config := Config{
		Binary:   binary,
		Snapshot: snapshot,
		Workdir:  filepath.Join(Root, Version),
	}

	return config, nil
}

func installPostgresViaMaven() (string, error) {
	system, err := deriveSystem(runtime.GOOS)
	if err != nil {
		return "", err
	}

	arch, err := deriveArchitecture(runtime.GOARCH)
	if err != nil {
		return "", err
	}

	if err := download(
		filepath.Join(Root, Version, "download"),
		"https://repo1.maven.org/maven2/io/zonky/test/postgres/embedded-postgres-binaries-"+system+"-"+arch+"/"+Version+"/embedded-postgres-binaries-"+system+"-"+arch+"-"+Version+".jar",
		"postgres.jar"); err != nil {
		return "", errors.WithMessage(err, "download postgres")
	}

	if err := extractTarGzFromJar(
		filepath.Join(Root, Version, "download", "postgres.jar"),
		filepath.Join(Root, Version, "unjar", "postgres.tar.xz")); err != nil {
		return "", errors.WithMessage(err, "extract tar from jar")
	}

	if err := execute(
		filepath.Join(Root, Version, "unpacked"),
		"tar", "xvf", "../unjar/postgres.tar.xz"); err != nil {
		return "", errors.WithMessage(err, "unpack postgres")
	}

	return filepath.Join(Root, Version, "unpacked"), nil
}

func hasNixShell() bool {
	_, err := exec.Command("which", "nix-shell").Output()
	return err == nil
}

func installViaNixStore() (string, error) {
	version := Version

	path := filepath.Join(Root, Version, "postgres")
	if err := os.MkdirAll(path, 0o755); err != nil {
		return "", errors.WithMessage(err, "create postgres directory")
	}

	postgresPath := filepath.Join(path, "pg")
	if _, err := os.Stat(postgresPath); err != nil {
		if idx := strings.IndexByte(version, '.'); idx > 0 {
			version = version[:idx]
		}

		// build an expression that should give us a postgres instance
		expr := fmt.Sprintf("(import <nixpkgs> {}).postgresql_%s", version)

		// create a derivation for postgres
		derivation, err := exec.
			Command("nix-instantiate", "--expr", expr).
			Output()

		if err != nil {
			return "", errors.WithMessagef(err, "derivation for expr %q", expr)
		}

		// instantiate the postgres derivation
		err = exec.
			Command("nix-store", "--realize", strings.TrimSpace(string(derivation)), "--add-root", postgresPath).
			Run()

		if err != nil {
			return "", errors.WithMessagef(err, "derivation for expr %q", expr)
		}
	}

	return postgresPath, nil
}

func deriveArchitecture(arch string) (string, error) {
	switch arch {
	case "amd64":
		return "amd64", nil

	case "arm64":
		return "arm64v8", nil

	default:
		return "", errors.Errorf("unsupported arch: %q", arch)
	}
}

func deriveSystem(system string) (string, error) {
	switch system {
	case "darwin", "linux":
		return system, nil

	default:
		return "", errors.Errorf("unsupported system %q", system)
	}
}

func atomicOperation(target string, op func(tempTarget string) error) error {
	lock := flock.New(target + ".lock")
	if err := lock.Lock(); err != nil {
		return errors.WithMessage(err, "get lock for download")
	}

	defer lock.Unlock()

	// check if file already exists
	if _, err := os.Stat(target); err == nil {
		return nil
	}

	targetTemp := fmt.Sprintf("%s.%d", target, time.Now().UnixNano())
	defer os.RemoveAll(targetTemp)

	if err := os.MkdirAll(targetTemp, 0o755); err != nil {
		return errors.WithMessage(err, "creating temporary directory")
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

		return errors.WithMessage(cmd.Run(), "execute command")
	})
}

func download(directory, url, name string) error {
	return atomicOperation(directory, func(target string) error {
		fmt.Println("Download: ", url)

		resp, err := http.DefaultClient.Get(url)
		if err != nil {
			return errors.WithMessage(err, "request to "+url)
		}

		defer resp.Body.Close()

		// write the partial download to a temporary file
		fp, err := os.Create(filepath.Join(target, name))
		if err != nil {
			return errors.WithMessage(err, "open temporary target file")
		}

		defer fp.Close()

		if _, err := io.CopyBuffer(fp, resp.Body, make([]byte, 64*1024)); err != nil {
			return errors.WithMessage(err, "download response into file")
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
			return errors.WithMessagef(err, "open postgres.jar file")
		}

		defer jar.Close()

		for _, file := range jar.File {
			// just pick the biggest file
			if file.UncompressedSize64 > 4*1024*1024 {
				r, err := file.Open()
				if err != nil {
					return errors.WithMessage(err, "unpack jar entry")
				}

				//goland:noinspection ALL
				defer r.Close()

				if err := writeTo(filepath.Join(tempTarget, filepath.Base(tar)), r); err != nil {
					return errors.WithMessage(err, "unpack jar entry")
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
		return errors.WithMessagef(err, "open file at %s", target)
	}

	defer fp.Close()

	_, err = io.Copy(fp, reader)
	if err != nil {
		return errors.WithMessagef(err, "copy to file %s", target)
	}

	return nil
}
