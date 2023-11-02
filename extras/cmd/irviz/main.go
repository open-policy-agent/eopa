package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/open-policy-agent/opa/ir"
)

func main() {
	var filename string
	var plain bool
	fs := flag.NewFlagSet("irviz", flag.ExitOnError)
	fs.StringVar(&filename, "f", "", "IR JSON filename to read in and dump a Graphviz DOT diagram for. (default: stdin)")
	fs.BoolVar(&plain, "p", false, "plain text output of golang representation")
	fs.Parse(os.Args[1:])

	// Get input Rego file from stdin or a file on disk.
	var fileBytes bytes.Buffer
	if filename == "" {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			fileBytes.Write(scanner.Bytes())
			fileBytes.WriteByte('\n')
		}
	} else {
		b, err := os.ReadFile(filename)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fileBytes.Write(b)
	}

	var policy ir.Policy
	if err := json.Unmarshal(fileBytes.Bytes(), &policy); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if plain {
		out := bytes.Buffer{}
		if err := ir.Pretty(&out, &policy); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}

		scanner := bufio.NewScanner(&out)
		for scanner.Scan() {
			os.Stdout.Write(bytes.ReplaceAll(scanner.Bytes(), []byte("|"), []byte(" ")))
			os.Stdout.Write([]byte("\n"))
		}
		return
	}

	f, err := PolicyToCFGDAGForest(&policy)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(f.AsDOT())
}
