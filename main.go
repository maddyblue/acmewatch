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
	"time"

	"9fans.net/go/acme"
	"github.com/adrg/xdg"
	toml "github.com/pelletier/go-toml"
)

func main() {
	flag.Parse()
	l, err := acme.Log()
	if err != nil {
		log.Fatal(err)
	}

	configPath, err := xdg.ConfigFile("acmewatch.toml")
	if err != nil {
		log.Fatal(err)
	}
	var lastMod time.Time
	var config Config

	readEvent := func(id int, name string) error {
		info, err := os.Stat(configPath)
		if err != nil {
			return err
		}
		mod := info.ModTime()
		if mod.After(lastMod) {
			f, err := os.Open(configPath)
			if err != nil {
				return err
			}
			defer f.Close()
			if err := toml.NewDecoder(f).Decode(&config); err != nil {
				return err
			}
			for _, fm := range config.Formatter {
				for i, m := range fm.Match {
					if strings.HasPrefix(m, ".") && !strings.Contains(m, "*") {
						fm.Match[i] = "*" + m
					}
				}
			}
			lastMod = mod
			fmt.Printf("read %s at %s\n", configPath, lastMod)
		}

		for _, fm := range config.Formatter {
			for _, m := range fm.Match {
				matchName := name
				if strings.HasPrefix(m, "*.") {
					matchName = filepath.Base(matchName)
				}
				matched, err := filepath.Match(m, matchName)
				if err != nil {
					return err
				}
				if !matched {
					continue
				}

				stdin := true
				args := fm.Args
				for i, arg := range args {
					if arg == "$name" {
						newArgs := make([]string, len(args))
						copy(newArgs, args)
						newArgs[i] = name
						args = newArgs
						stdin = false
					}
				}
				cmd := exec.Command(fm.Cmd, args...)
				cmd.Dir = filepath.Dir(name)
				if stdin {
					f, err := os.Open(name)
					if err != nil {
						return err
					}
					defer f.Close()
					cmd.Stdin = f
				}
				out, err := cmd.CombinedOutput()
				if err != nil {
					return fmt.Errorf("%s: %s", err, string(out))
				}
				reformat(id, name, out)
				return nil
			}
		}

		return nil
	}

	for {
		event, err := l.Read()
		if err != nil {
			log.Fatal(err)
		}
		if event.Name == "" || event.Op != "put" {
			continue
		}
		if err := readEvent(event.ID, event.Name); err != nil {
			fmt.Printf("%s: %s\n", event.Name, err)
		}
	}
}

type Config struct {
	Formatter []struct {
		Match []string
		Cmd   string
		Args  []string
	}
}

func reformat(id int, name string, new []byte) {
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
