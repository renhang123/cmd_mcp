package config

import "testing"

func TestBuildRejectsAbuseConfiguration(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*File)
	}{
		{name: "raw shell", mutate: func(f *File) { f.Commands[0].Executable = "/bin/bash" }},
		{name: "command chain", mutate: func(f *File) { f.Commands[0].ArgvTemplate = []ArgvTemplatePartConfig{{Literal: "ok;whoami"}} }},
		{name: "command substitution", mutate: func(f *File) { f.Commands[0].ArgvTemplate = []ArgvTemplatePartConfig{{Literal: "$(whoami)"}} }},
		{name: "pipe", mutate: func(f *File) { f.Commands[0].ArgvTemplate = []ArgvTemplatePartConfig{{Literal: "ok|cat"}} }},
		{name: "redirect", mutate: func(f *File) { f.Commands[0].ArgvTemplate = []ArgvTemplatePartConfig{{Literal: "ok>/tmp/x"}} }},
		{name: "sensitive arbitrary file read", mutate: func(f *File) { f.Commands[0].Executable = "/bin/cat" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			file := validFile()
			tc.mutate(&file)
			if _, err := Build(file); err == nil {
				t.Fatal("expected abuse configuration to be rejected")
			}
		})
	}
}
