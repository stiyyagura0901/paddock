package main

import "testing"

func TestParsePR(t *testing.T) {
	for _, tc := range []struct {
		ref, repo string
		n         int
	}{
		{"https://github.com/octo/hello.js/pull/12", "octo/hello.js", 12},
		{"https://github.com/octo/hello/pull/12/files", "octo/hello", 12},
		{"octo/hello#7", "octo/hello", 7},
		{"42", "", 42},
	} {
		repo, n, err := parsePR(tc.ref)
		if err != nil || repo != tc.repo || n != tc.n {
			t.Errorf("parsePR(%q) = %q, %d, %v; want %q, %d", tc.ref, repo, n, err, tc.repo, tc.n)
		}
	}
	for _, ref := range []string{
		"", "0", "-3", "abc", "octo/hello", "octo/hello#", "octo#3", "a/b/c#3",
		"https://github.com/octo/hello/issues/12",
		"https://github.com/octo/hello/pull/x",
		"https://gitlab.com/octo/hello/pull/12",
		"https://github.com.evil.example/octo/hello/pull/12",
	} {
		if _, _, err := parsePR(ref); err == nil {
			t.Errorf("parsePR(%q) accepted an invalid reference", ref)
		}
	}
}
