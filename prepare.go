package pgtest

import (
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

func PreparePostgresInstallation(root string, linux bool) error {
	if err := os.MkdirAll(root, 0755); err != nil {
		return errors.WithMessage(err, "creating working directory")
	}

	if linux {
		if err := download(
			filepath.Join(root, "download"),
			"https://get.enterprisedb.com/postgresql/postgresql-10.1-3-linux-x64-binaries.tar.gz",
			"postgres.tar.gz"); err != nil {

			return errors.WithMessage(err, "download postgres")
		}

		if err := execute(
			filepath.Join(root, "unpacked"),
			"tar", "xvf", "../download/postgres.tar.gz"); err != nil {

			return errors.WithMessage(err, "unpack postgres")
		}
	} else {
		if err := download(
			filepath.Join(root, "download"),
			"https://get.enterprisedb.com/postgresql/postgresql-10.1-3-osx-binaries.zip",
			"postgres.zip"); err != nil {

			return errors.WithMessage(err, "download postgres")
		}

		if err := execute(
			filepath.Join(root, "unpacked"),
			"unzip", "../download/postgres.zip"); err != nil {

			return errors.WithMessage(err, "unpack postgres")
		}
	}

	if err := execute(
		filepath.Join(root, "initdb"),
		"../unpacked/pgsql/bin/initdb", "-U", "postgres", "-D", "pgdata", "--no-sync"); err != nil {

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
