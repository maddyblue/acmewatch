// Copyright 2014 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Acmego watches acme for .go files being written.
// Each time a .go file is written, acmego checks whether the
// import block needs adjustment. If so, it makes the changes
// in the window body but does not write the file.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"9fans.net/go/acme"
)

func main() {
	flag.Parse()
	l, err := acme.Log()
	if err != nil {
		log.Fatal(err)
	}

	for {
		event, err := l.Read()
		if err != nil {
			log.Fatal(err)
		}
		if event.Name != "" && event.Op == "put" {
			var fn fmtFn
			switch filepath.Ext(event.Name) {
			case ".go":
				//fn = fmtGoImports
				fn = fmtCrlfmt
			case ".js", ".css", ".html", ".json", ".less", ".ts", ".tsx":
				fn = fmtJS
			case ".sql":
				fn = fmtSQL
			//case ".rs":
			//fn = fmtRust
			case ".opt":
				fn = fmtOpt
			}
			if fn != nil {
				reformat(event.ID, event.Name, fn)
			}
		}
	}
}

type fmtFn func(name string) (new []byte, err error)

func fmtSQL(name string) ([]byte, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	cmd := exec.Command("sqlfmt", "--print-width", "80")
	cmd.Stdin = f
	new, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(new), "fatal error") {
			return nil, fmt.Errorf("sqlfmt %s: %v\n%s", name, err, new)
		}
		return nil, fmt.Errorf("%s", new)
	}
	return new, nil
}

func fmtCrlfmt(name string) ([]byte, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	cmd := exec.Command("crlfmt", "-tab", "2")
	cmd.Stdin = f
	new, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(new), "fatal error") {
			return nil, fmt.Errorf("crlfmt %s: %v\n%s", name, err, new)
		}
		return nil, fmt.Errorf("%s", new)
	}
	return new, nil
}

func fmtRust(name string) ([]byte, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	cmd := exec.Command("rustfmt", "--config", "hard_tabs=true", "--edition", "2018")
	cmd.Stdin = f
	new, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(new), "fatal error") {
			return nil, fmt.Errorf("rustfmt %s: %v\n%s", name, err, new)
		}
		return nil, fmt.Errorf("%s", new)
	}
	return new, nil
}

func fmtOpt(name string) ([]byte, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	cmd := exec.Command("optfmt")
	cmd.Stdin = f
	new, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(new), "fatal error") {
			return nil, fmt.Errorf("rustfmt %s: %v\n%s", name, err, new)
		}
		return nil, fmt.Errorf("%s", new)
	}
	return new, nil
}

func fmtGoImports(name string) ([]byte, error) {
	new, err := exec.Command("goimports", name).CombinedOutput()
	if err != nil {
		if strings.Contains(string(new), "fatal error") {
			return nil, fmt.Errorf("goimports %s: %v\n%s", name, err, new)
		}
		return nil, fmt.Errorf("%s", new)
	}
	return new, nil
}

func fmtJS(name string) ([]byte, error) {
	new, err := exec.Command(
		"prettier",
		"--use-tabs",
		"--single-quote",
		"--no-color",
		"--trailing-comma", "es5",
		name).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s: %v\n%s", name, err, new)
	}
	return new, nil
}

func reformat(id int, name string, fn fmtFn) {
	w, err := acme.Open(id, nil)
	if err != nil {
		log.Print(err)
		return
	}
	defer w.CloseFiles()

	old, err := ioutil.ReadFile(name)
	if err != nil {
		//log.Print(err)
		return
	}
	new, err := fn(name)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}

	if new == nil || bytes.Equal(old, new) {
		return
	}

	f, err := ioutil.TempFile("", "acmego")
	if err != nil {
		log.Print(err)
		return
	}
	if _, err := f.Write(new); err != nil {
		log.Print(err)
		return
	}
	tmp := f.Name()
	f.Close()
	defer os.Remove(tmp)

	diff, _ := exec.Command("9", "diff", name, tmp).CombinedOutput()

	w.Write("ctl", []byte("mark"))
	w.Write("ctl", []byte("nomark"))
	diffLines := strings.Split(string(diff), "\n")
	for i := len(diffLines) - 1; i >= 0; i-- {
		line := diffLines[i]
		if line == "" {
			continue
		}
		if line[0] == '<' || line[0] == '-' || line[0] == '>' {
			continue
		}
		j := 0
		for j < len(line) && line[j] != 'a' && line[j] != 'c' && line[j] != 'd' {
			j++
		}
		if j >= len(line) {
			log.Printf("cannot parse diff line: %q", line)
			break
		}
		oldStart, oldEnd := parseSpan(line[:j])
		newStart, newEnd := parseSpan(line[j+1:])
		if oldStart == 0 || newStart == 0 {
			continue
		}
		switch line[j] {
		case 'a':
			err := w.Addr("%d+#0", oldStart)
			if err != nil {
				log.Print(err)
				break
			}
			w.Write("data", findLines(new, newStart, newEnd))
		case 'c':
			err := w.Addr("%d,%d", oldStart, oldEnd)
			if err != nil {
				log.Print(err)
				break
			}
			w.Write("data", findLines(new, newStart, newEnd))
		case 'd':
			err := w.Addr("%d,%d", oldStart, oldEnd)
			if err != nil {
				log.Print(err)
				break
			}
			w.Write("data", nil)
		}
	}
}

func parseSpan(text string) (start, end int) {
	i := strings.Index(text, ",")
	if i < 0 {
		n, err := strconv.Atoi(text)
		if err != nil {
			log.Printf("cannot parse span %q", text)
			return 0, 0
		}
		return n, n
	}
	start, err1 := strconv.Atoi(text[:i])
	end, err2 := strconv.Atoi(text[i+1:])
	if err1 != nil || err2 != nil {
		log.Printf("cannot parse span %q", text)
		return 0, 0
	}
	return start, end
}

func findLines(text []byte, start, end int) []byte {
	i := 0

	start--
	for ; i < len(text) && start > 0; i++ {
		if text[i] == '\n' {
			start--
			end--
		}
	}
	startByte := i
	for ; i < len(text) && end > 0; i++ {
		if text[i] == '\n' {
			end--
		}
	}
	endByte := i
	return text[startByte:endByte]
}
