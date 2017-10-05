package main

import (
	"io/ioutil"
	"path/filepath"
	"testing"
)

func TestRenameExt(t *testing.T) {
	if s := renameExt("foo.amber", ".amber", ".html"); s != "foo.html" {
		t.Error(s)
	}
	if s := renameExt("foo.amber", "", ".html"); s != "foo.html" {
		t.Error(s)
	}
	if s := renameExt("foo.amber", ".md", ".html"); s != "foo.amber" {
		t.Error(s)
	}
	if s := renameExt("foo", ".amber", ".html"); s != "foo" {
		t.Error(s)
	}
	if s := renameExt("foo", "", ".html"); s != "foo.html" {
		t.Error(s)
	}
}

func TestVars(t *testing.T) {
	tests := map[string]Vars{
		`
foo: bar
title: Hello, world!
---
Some content in markdown
`: Vars{
			"foo":       "bar",
			"title":     "Hello, world!",
			"url":       "test.html",
			"file":      "test.md",
			"output":    filepath.Join(PUBDIR, "test.html"),
			"__content": "Some content in markdown\n",
		},
		`
url: "example.com/foo.html"
---
Hello
`: Vars{
			"url":       "example.com/foo.html",
			"__content": "Hello\n",
		},
	}

	for script, vars := range tests {
		ioutil.WriteFile("test.md", []byte(script), 0644)
		if v, s, err := getVars("test.md", Vars{"baz": "123"}); err != nil {
			t.Error(err)
		} else if s != vars["__content"] {
			t.Error(s, vars["__content"])
		} else {
			for key, value := range vars {
				if key != "__content" && v[key] != value {
					t.Error(key, v[key], value)
				}
			}
		}
	}
}
