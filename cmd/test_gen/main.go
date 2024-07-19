//go:generate go run main.go -go -out ../../internal/examples/semantics/generated_test.go ../../internal/examples/semantics
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path"
	"regexp"
	"strings"
)

const coqHeader string = `(* autogenerated by goose/cmd/test_gen *)
From Perennial.goose_lang Require Import prelude.
From Perennial.goose_lang Require Import ffi.disk_prelude.
From Perennial.goose_lang.interpreter Require Import test_config.

(* test functions *)
From Goose.github_com.tchajed.goose.internal.examples Require Import semantics.

`

const goHeader string = `// Code generated by goose/cmd/test_gen DO NOT EDIT.
package semantics

import (
	"testing"

	"github.com/goose-lang/goose/machine/disk"
	"github.com/stretchr/testify/suite"
)

type GoTestSuite struct {
	suite.Suite
}

`

const goFooter string = `func TestSuite(t *testing.T) {
	suite.Run(t, new(GoTestSuite))
}
`

func main() {

	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "Usage: test_gen [options] <path to go package>")

		flag.PrintDefaults()
	}

	var coqTest bool
	flag.BoolVar(&coqTest, "coq", false,
		"generate a .v test file from tests functions in input package")

	var goTest bool
	flag.BoolVar(&goTest, "go", false,
		"generate a .go test suite file from test functions in input package")

	var outFile string
	flag.StringVar(&outFile, "out", "-",
		"file to output to (use '-' for stdout)")

	flag.Parse()

	var t string
	if coqTest && !goTest {
		t = "coq"
	} else if !coqTest && goTest {
		t = "go"
	} else {
		fmt.Fprintln(os.Stderr, "must invoke either -coq or -go flag (but not both)")
		os.Exit(1)
	}

	// out file set up
	out := os.Stdout

	if outFile != "-" {
		var err error
		out, err = os.Create(outFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			fmt.Fprintln(os.Stderr, "could not write output")
			os.Exit(1)
		}
		defer out.Close()
	}

	// read files from input directory
	if flag.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "Path to semantics package not provided")
		flag.Usage()
		os.Exit(1)
	}
	srcDir := flag.Arg(0)
	files, err := os.ReadDir(srcDir)
	if err != nil {
		panic(err)
	}

	if t == "coq" {
		fmt.Fprint(out, coqHeader)

		for _, file := range files {

			// skip emacs back-up files, generated test file, and gold file
			if strings.HasSuffix(file.Name(), "~") ||
				strings.HasSuffix(file.Name(), ".gold.v") ||
				strings.HasSuffix(file.Name(), "_test.go") {
				continue
			}

			f, err := os.Open(path.Join(srcDir, file.Name()))
			if err != nil {
				panic(err)
			}

			fmt.Fprintf(out, "(* %s *)\n", file.Name())
			scanner := bufio.NewScanner(f)
			for scanner.Scan() {
				line := scanner.Text()

				re := regexp.MustCompile(`(?:^func\s)(?P<fail>(failing_)?)(?P<name>test[[:alnum:]]+)(?:\(.*)`)
				m := re.FindStringSubmatch(line)

				if len(m) != 0 {
					if len(m[2]) != 0 {
						fmt.Fprintf(out, "Fail Example %s_ok : %s%s #() ~~> #true := t.\n", m[3], m[2], m[3])
					} else {
						fmt.Fprintf(out, "Example %s_ok : %s #() ~~> #true := t.\n", m[3], m[3])
					}
				}
			}
			fmt.Fprint(out, "\n")
		}

	} else if t == "go" {
		fmt.Fprint(out, goHeader)
		re := regexp.MustCompile(`(?:^func\s)(?P<fail>(failing_)?)(?:test)(?P<name>[[:alnum:]]+)(?:\(.*)`)

		for _, file := range files {

			// skip emacs back-up files
			if strings.HasSuffix(file.Name(), "~") {
				continue
			}

			f, err := os.Open(path.Join(srcDir, file.Name()))
			if err != nil {
				panic(err)
			}

			scanner := bufio.NewScanner(f)
			for scanner.Scan() {
				line := scanner.Text()

				m := re.FindStringSubmatch(line)
				if len(m) != 0 {
					fmt.Fprintf(out, "func (suite *GoTestSuite) Test%s() {\n", m[3])
					fmt.Fprintf(out, "\td := disk.NewMemDisk(30)\n")
					fmt.Fprintf(out, "\tdisk.Init(d)\n")
					fmt.Fprintf(out, "\tsuite.Equal(true, %stest%s())\n", m[2], m[3])
					fmt.Fprintf(out, "}\n\n")
				}
			}
		}

		fmt.Fprint(out, goFooter)

	} else {
		fmt.Fprintln(os.Stderr, "could not write output")
		os.Exit(1)
	}
}
