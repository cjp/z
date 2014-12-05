package main

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/russross/blackfriday"
)

const (
	ZSDIR  = ".zs"
	PUBDIR = ".pub"
)

func split2(s, delim string) (string, string) {
	parts := strings.SplitN(s, delim, 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	} else {
		return parts[0], ""
	}
}

func md(s string) (map[string]string, string) {
	v := map[string]string{}
	// FIXME: if no header?
	header, body := split2(s, "\n\n")
	for _, line := range strings.Split(header, "\n") {
		key, value := split2(line, ":")
		v[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(value)
	}
	return v, body
}

type EvalFn func(args []string, vars map[string]string) (string, error)

func render(s string, vars map[string]string, eval EvalFn) (string, error) {
	b := []byte(s)
	delim_open := []byte("{{")
	delim_close := []byte("}}")

	out := bytes.NewBuffer(nil)
	for {
		if from := bytes.Index(b, delim_open); from == -1 {
			out.Write(b)
			return out.String(), nil
		} else {
			to := bytes.Index(b, delim_close)
			if to == -1 {
				return "", fmt.Errorf("Close delim not found")
			} else {
				out.Write(b[:from])
				cmd := b[from+len(delim_open) : to]
				b = b[to+len(delim_close):]
				m := strings.Fields(string(cmd))
				if len(m) == 1 {
					if v, ok := vars[m[0]]; ok {
						out.Write([]byte(v))
						continue
					}
				}
				if res, err := eval(m, vars); err == nil {
					out.Write([]byte(res))
				} else {
					log.Println(err) // silent
				}
			}
		}
	}
}

func eval(cmd []string, vars map[string]string) (string, error) {
	var outbuf, errbuf bytes.Buffer
	c := exec.Command(path.Join(ZSDIR, cmd[0]), cmd[1:]...)
	env := []string{"ZS=" + os.Args[0]}
	for k, v := range vars {
		env = append(env, "ZS_"+strings.ToUpper(k)+"="+v)
	}
	c.Env = append(c.Env, env...)
	c.Stdout = &outbuf
	c.Stderr = &errbuf
	if err := c.Run(); err != nil {
		log.Println(err)
		c := exec.Command(path.Join(cmd[0]), cmd[1:]...)
		c.Env = append(c.Env, env...)
		c.Stdout = &outbuf
		c.Stderr = &errbuf
		if err := c.Run(); err != nil {
			return "", err
		}
	}
	if errbuf.Len() > 0 {
		log.Println(errbuf.String())
	}
	return outbuf.String(), nil
}

func buildMarkdown(path string) error {
	b, err := ioutil.ReadFile(path)
	if err != nil {
		return err
	}
	v, body := md(string(b))
	defaultVars(v, path)
	content, err := render(body, v, eval)
	if err != nil {
		return err
	}
	v["content"] = string(blackfriday.MarkdownBasic([]byte(content)))
	b, err = ioutil.ReadFile(filepath.Join(ZSDIR, v["layout"]))
	if err != nil {
		return err
	}
	content, err = render(string(b), v, eval)
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(v["output"], []byte(content), 0666)
	if err != nil {
		return err
	}
	return nil
}

func defaultVars(vars map[string]string, path string) {
	if _, ok := vars["file"]; !ok {
		vars["file"] = path
	}
	if _, ok := vars["url"]; !ok {
		vars["url"] = path[:len(path)-len(filepath.Ext(path))] + ".html"
		if strings.HasPrefix(vars["url"], "./") {
			vars["url"] = vars["url"][2:]
		}
	}
	if _, ok := vars["outdir"]; !ok {
		vars["outdir"] = PUBDIR
	}
	if _, ok := vars["output"]; !ok {
		vars["output"] = filepath.Join(PUBDIR, vars["url"])
	}
	if _, ok := vars["layout"]; !ok {
		vars["layout"] = "index.html"
	}
}

func copyFile(path string) error {
	if in, err := os.Open(path); err != nil {
		return err
	} else {
		defer in.Close()
		if stat, err := in.Stat(); err != nil {
			return err
		} else {
			// Directory?
			if stat.Mode().IsDir() {
				os.Mkdir(filepath.Join(PUBDIR, path), 0755)
				return nil
			}
			if !stat.Mode().IsRegular() {
				return nil
			}
		}
		if out, err := os.Create(filepath.Join(PUBDIR, path)); err != nil {
			return err
		} else {
			defer out.Close()
			_, err = io.Copy(out, in)
			return err
		}
	}
}

func buildAll(once bool) {
	lastModified := time.Unix(0, 0)
	for {
		os.Mkdir(PUBDIR, 0755)
		err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
			// ignore hidden files and directories
			if filepath.Base(path)[0] == '.' || strings.HasPrefix(path, ".") {
				return nil
			}

			if info.ModTime().After(lastModified) {
				ext := filepath.Ext(path)
				if ext == ".md" || ext == "mkd" {
					log.Println("mkd: ", path)
					return buildMarkdown(path)
				} else {
					log.Println("raw: ", path)
					return copyFile(path)
				}
			}
			return nil
		})
		if err != nil {
			log.Println("ERROR:", err)
		}
		lastModified = time.Now()
		if once {
			break
		}
		time.Sleep(1 * time.Second)
	}
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
		buildAll(true)
	case "watch":
		buildAll(false) // pass duration
	case "var":
		if len(args) == 0 {
			// print error
			return
		}
		if b, err := ioutil.ReadFile(args[0]); err == nil {
			vars, _ := md(string(b))
			defaultVars(vars, args[0])
			if len(args) > 1 {
				for _, a := range args[1:] {
					fmt.Println(vars[a])
				}
			} else {
				for k, v := range vars {
					fmt.Println(k + ":" + v)
				}
			}
		} else {
			// print error
		}
	default:
		cmd := exec.Command(path.Join(ZSDIR, cmd), args...)
		cmd.Env = append(cmd.Env, "ZS="+os.Args[0])
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			log.Println(err)
		}
	}
}