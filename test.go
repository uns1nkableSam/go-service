// +build ignore

// This is a fairly crummy test runner that tries to hackily dispatch arguments
// to the right test, set coverpkg properly and collect coverage for all tests
// into a single report.
//
// Pass arguments to test binaries after a '--':
//
//   go run test.go -- -v -service.fuzz=true
//
// All arguments that start with '-service.fuzz' are passed to the servicetest
// package only.
//

package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/shabbyrobe/gocovmerge"
	"golang.org/x/tools/cover"
)

type testPkg struct {
	pkg  string
	args []string
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	var (
		coverProfile  string
		coverPackages []string
	)

	flag.StringVar(&coverProfile, "cover", "", "coverage profile file")
	flag.Parse()

	coverPkgArg := "-coverpkg=github.com/shabbyrobe/go-service"
	pkgs := []testPkg{
		{pkg: "github.com/shabbyrobe/go-service"},

		{pkg: "github.com/shabbyrobe/go-service/servicetest", args: []string{coverPkgArg}},
		{pkg: "github.com/shabbyrobe/go-service/serviceutil", args: []string{coverPkgArg}},
	}

	var cleanFiles []string
	defer func() {
		for _, f := range cleanFiles {
			os.Remove(f)
		}
	}()

	for _, pkg := range pkgs {
		coverPackages = append(coverPackages, pkg.pkg)
	}

	// set aside fuzzer arguments. has the undesirable side effect of forcing
	// all passed-through flags to use '=' to separate flag and value.
	var args []string
	var argsFuzz []string

	for _, arg := range flag.Args() {
		if strings.HasPrefix(arg, "-service.fuzz") {
			argsFuzz = append(argsFuzz, arg)
		} else {
			args = append(args, arg)
		}
	}

	var profiles []*cover.Profile

	for _, pkg := range pkgs {
		pargs := []string{"test"}

		var tmpProfile string
		if coverProfile != "" {
			tmpProfile = filepath.Join(os.TempDir(), "profile-"+randID())
			cleanFiles = append(cleanFiles, tmpProfile)
			pargs = append(pargs, "-coverprofile", tmpProfile)
		}

		pargs = append(pargs, pkg.pkg)

		pargs = append(pargs, pkg.args...)
		pargs = append(pargs, args...)

		if pkg.pkg == "github.com/shabbyrobe/go-service/servicetest" {
			pargs = append(pargs, argsFuzz...)
		}

		if err := cmd("go", pargs); err != nil {
			return err
		}

		if coverProfile != "" {
			cprofiles, err := cover.ParseProfiles(tmpProfile)
			if err != nil {
				return err
			}
			for _, p := range cprofiles {
				profiles = gocovmerge.AddProfile(profiles, p)
			}
		}
	}

	if coverProfile != "" {
		f, err := os.OpenFile(coverProfile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
		if err != nil {
			return err
		}
		defer f.Close()

		if err := gocovmerge.DumpProfiles(profiles, f); err != nil {
			return err
		}
	}

	return nil
}

func cmd(cmd string, args []string) error {
	c := exec.Command("go", args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return err
	}
	return nil
}

func randID() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), rand.Uint64())
}
