package pgtest

import (
	"archive/zip"
	"fmt"
	"github.com/pkg/errors"
	"github.com/theckman/go-flock"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func PreparePostgresInstallation(path string, version string, linux bool) error {
	root := filepath.Join(path, version)

	if err := os.MkdirAll(root, 0755); err != nil {
		return errors.WithMessage(err, "creating working directory")
	}

	var system string
	if linux {
		system = "linux"
	} else {
		system = "darwin"
	}

	if err := download(
		filepath.Join(root, "download"),
		"https://repo1.maven.org/maven2/io/zonky/test/postgres/embedded-postgres-binaries-"+system+"-amd64/"+version+"/embedded-postgres-binaries-"+system+"-amd64-"+version+".jar",
		"postgres.jar"); err != nil {

		return errors.WithMessage(err, "download postgres")
	}

	jar, err := zip.OpenReader(filepath.Join(root, "download", "postgres.jar"))
	if err != nil {
		return errors.WithMessagef(err, "open postgres.jar file")
	}

	defer jar.Close()

	for _, file := range jar.File {
		if file.Name == "postgres-"+system+"-x86_64.txz" {
			r, err := file.Open()
			if err != nil {
				return errors.WithMessage(err, "unpack postgres.tar.gz")
			}

			defer r.Close()

			if err := writeTo(filepath.Join(root, "download", "postgres.tar.gz"), r); err != nil {
				return errors.WithMessage(err, "unpack postgres.tar.gz")
			}
		}
	}

	if err := execute(
		filepath.Join(root, "unpacked"),
		"tar", "xvf", "../download/postgres.tar.gz"); err != nil {

		return errors.WithMessage(err, "unpack postgres")
	}

	if err := execute(
		filepath.Join(root, "initdb"),
		"../unpacked/bin/initdb", "-U", "postgres", "-D", "pgdata", "--no-sync"); err != nil {

		return errors.WithMessage(err, "initialize pgdata snapshot")
	}

	return nil
}

func atomicOperation(target string, op func(string) error) error {
	lock := flock.NewFlock(target + ".lock")
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

	if err := os.MkdirAll(targetTemp, 0755); err != nil {
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
