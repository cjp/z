package main

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/eknkc/amber"
	"github.com/russross/blackfriday"
	"github.com/yosssi/gcss"
	"gopkg.in/yaml.v2"
)

const (
	ZSDIR  = ".zs"
	PUBDIR = ".pub"
)

type Vars map[string]string

// renameExt renames extension (if any) from oldext to newext
// If oldext is an empty string - extension is extracted automatically.
// If path has no extension - new extension is appended
func renameExt(path, oldext, newext string) string {
	if oldext == "" {
		oldext = filepath.Ext(path)
	}
	if oldext == "" || strings.HasSuffix(path, oldext) {
		return strings.TrimSuffix(path, oldext) + newext
	} else {
		return path
	}
}

// globals returns list of global OS environment variables that start
// with ZS_ prefix as Vars, so the values can be used inside templates
func globals() Vars {
	vars := Vars{}
	for _, e := range os.Environ() {
		pair := strings.Split(e, "=")
		if strings.HasPrefix(pair[0], "ZS_") {
			vars[strings.ToLower(pair[0][3:])] = pair[1]
		}
	}
	return vars
}

// getVars returns list of variables defined in a text file and actual file
// content following the variables declaration. Header is separated from
// content by an empty line. Header can be either YAML or JSON.
// If no empty newline is found - file is treated as content-only.
func getVars(path string, globals Vars) (Vars, string, error) {
	b, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	s := string(b)

	// Pick some default values for content-dependent variables
	v := Vars{}
	title := strings.Replace(strings.Replace(path, "_", " ", -1), "-", " ", -1)
	v["title"] = strings.ToTitle(title)
	v["description"] = ""
	v["file"] = path
	v["url"] = path[:len(path)-len(filepath.Ext(path))] + ".html"
	v["output"] = filepath.Join(PUBDIR, v["url"])

	// Override default values with globals
	for name, value := range globals {
		v[name] = value
	}

	// Add layout if none is specified
	if _, ok := v["layout"]; !ok {
		if _, err := os.Stat(filepath.Join(ZSDIR, "layout.amber")); err == nil {
			v["layout"] = "layout.amber"
		} else {
			v["layout"] = "layout.html"
		}
	}

	delim := "\n---\n"
	if sep := strings.Index(s, delim); sep == -1 {
		return v, s, nil
	} else {
		header := s[:sep]
		body := s[sep+len(delim):]

		vars := Vars{}
		if err := yaml.Unmarshal([]byte(header), &vars); err != nil {
			fmt.Println("ERROR: failed to parse header", err)
			return nil, "", err
		} else {
			// Override default values + globals with the ones defines in the file
			for key, value := range vars {
				v[key] = value
			}
		}
		if strings.HasPrefix(v["url"], "./") {
			v["url"] = v["url"][2:]
		}
		return v, body, nil
	}
}

// Renders markdown with the given layout into html expanding all the macros
func buildMarkdown(path string, w io.Writer, vars Vars) error {
	v, body, err := getVars(path, vars)
	if err != nil {
		return err
	}
	v["content"] = string(blackfriday.MarkdownCommon([]byte(body)))
	if w == nil {
		out, err := os.Create(filepath.Join(PUBDIR, renameExt(path, "", ".html")))
		if err != nil {
			return err
		}
		defer out.Close()
		w = out
	}
        return buildAmber(filepath.Join(ZSDIR, v["layout"]), w, v)
}

// Renders .amber file into .html
func buildAmber(path string, w io.Writer, vars Vars) error {
	v, body, err := getVars(path, vars)
	if err != nil {
		return err
	}
	a := amber.New()
	if err := a.Parse(body); err != nil {
		fmt.Println(body)
		return err
	}

	t, err := a.Compile()
	if err != nil {
		return err
	}

	htmlBuf := &bytes.Buffer{}
	if err := t.Execute(htmlBuf, v); err != nil {
		return err
	}

	if w == nil {
		f, err := os.Create(filepath.Join(PUBDIR, renameExt(path, ".amber", ".html")))
		if err != nil {
			return err
		}
		defer f.Close()
		w = f
	}
	_, err = io.WriteString(w, string(htmlBuf.Bytes()))
	return err
}

// Compiles .gcss into .css
func buildGCSS(path string, w io.Writer) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	if w == nil {
		s := strings.TrimSuffix(path, ".gcss") + ".css"
		css, err := os.Create(filepath.Join(PUBDIR, s))
		if err != nil {
			return err
		}
		defer css.Close()
		w = css
	}
	_, err = gcss.Compile(w, f)
	return err
}

// Copies file as is from path to writer
func buildRaw(path string, w io.Writer) error {
	in, err := os.Open(path)
	if err != nil {
		return err
	}
	defer in.Close()
	if w == nil {
		if out, err := os.Create(filepath.Join(PUBDIR, path)); err != nil {
			return err
		} else {
			defer out.Close()
			w = out
		}
	}
	_, err = io.Copy(w, in)
	return err
}

func build(path string, w io.Writer, vars Vars) error {
	ext := filepath.Ext(path)
	if ext == ".md" || ext == ".mkd" {
		return buildMarkdown(path, w, vars)
	} else if ext == ".amber" {
		return buildAmber(path, w, vars)
	} else if ext == ".gcss" {
		return buildGCSS(path, w)
	} else {
		return buildRaw(path, w)
	}
}

func buildAll(watch bool) {
	lastModified := time.Unix(0, 0)
	modified := false

	vars := globals()
	for {
		os.Mkdir(PUBDIR, 0755)
		filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
			// ignore hidden files and directories
			if filepath.Base(path)[0] == '.' || strings.HasPrefix(path, ".") {
				return nil
			}
			// inform user about fs walk errors, but continue iteration
			if err != nil {
				fmt.Println("error:", err)
				return nil
			}

			if info.IsDir() {
				os.Mkdir(filepath.Join(PUBDIR, path), 0755)
				return nil
			} else if info.ModTime().After(lastModified) {
				if !modified {
					// First file in this build cycle is about to be modified
					// TODO: future prehook action
					modified = true
				}
				log.Println("build:", path)
				return build(path, nil, vars)
			}
			return nil
		})
		if modified {
			// At least one file in this build cycle has been modified
                        // TODO: future posthook action
			modified = false
		}
		if !watch {
			break
		}
		lastModified = time.Now()
		time.Sleep(1 * time.Second)
	}
}

func init() {
	// prepend .zs to $PATH, so plugins will be found before OS commands
	p := os.Getenv("PATH")
	p = ZSDIR + ":" + p
	os.Setenv("PATH", p)
}

func main() {
	if len(os.Args) == 1 {
		fmt.Println(os.Args[0], "<command> [args]")
		return
	}
	cmd := os.Args[1]
	args := os.Args[2:]
	switch cmd {
	case "build":
		if len(args) == 0 {
			buildAll(false)
		} else if len(args) == 1 {
			if err := build(args[0], os.Stdout, globals()); err != nil {
				fmt.Println("ERROR: " + err.Error())
			}
		} else {
			fmt.Println("ERROR: too many arguments")
		}
	case "watch":
		buildAll(true)
	case "var":
		if len(args) == 0 {
			fmt.Println("var: filename expected")
		} else {
			s := ""
			if vars, _, err := getVars(args[0], Vars{}); err != nil {
				fmt.Println("var: " + err.Error())
			} else {
				if len(args) > 1 {
					for _, a := range args[1:] {
						s = s + vars[a] + "\n"
					}
				} else {
					for k, v := range vars {
						s = s + k + ":" + v + "\n"
					}
				}
			}
			fmt.Println(strings.TrimSpace(s))
		}
	}
}
