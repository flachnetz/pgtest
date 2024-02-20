package pgtest

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/theckman/go-flock"
)

func PreparePostgresInstallation(path string, version string, linux bool, arch string) error {
	root := filepath.Join(path, version)

	if err := os.MkdirAll(root, 0o755); err != nil {
		return errors.WithMessage(err, "creating working directory")
	}

	var system string
	if linux {
		system = "linux"
	} else {
		system = "darwin"
	}

	if arch == "arm64" {
		arch = "arm64v8"
	}

	if err := download(
		filepath.Join(root, "download"),
		"https://repo1.maven.org/maven2/io/zonky/test/postgres/embedded-postgres-binaries-"+system+"-"+arch+"/"+version+"/embedded-postgres-binaries-"+system+"-"+arch+"-"+version+".jar",
		"postgres.jar"); err != nil {
		return errors.WithMessage(err, "download postgres")
	}

	if err := extractTarGzFromJar(
		filepath.Join(root, "download", "postgres.jar"),
		filepath.Join(root, "unjar", "postgres.tar.xz")); err != nil {
		return errors.WithMessage(err, "extract tar from jar")
	}

	if err := execute(
		filepath.Join(root, "unpacked"),
		"tar", "xvf", "../unjar/postgres.tar.xz"); err != nil {
		return errors.WithMessage(err, "unpack postgres")
	}

	if err := execute(
		filepath.Join(root, "initdb"),
		"../unpacked/bin/initdb", "-U", "postgres", "-D", "pgdata", "--no-sync"); err != nil {
		return errors.WithMessage(err, "initialize pgdata snapshot")
	}

	return nil
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
	target := path.Dir(tar)

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

				if err := writeTo(filepath.Join(tempTarget, path.Base(tar)), r); err != nil {
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
