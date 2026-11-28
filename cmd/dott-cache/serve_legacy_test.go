package main

import "testing"

func TestLegacyAllowed(t *testing.T) {
	s := &serveSite{
		cfg:   Config{WriteBlogs: []string{"/"}, LegacyBlogs: []string{"/en/"}},
		blogs: map[int]string{1: "/", 2: "/en/", 3: "/es/"},
	}
	cases := []struct {
		p               string
		allowed, legacy bool
	}{
		{"/", true, false},
		{"/ban-do-an-do/", true, false},
		{"/en/", true, true},
		{"/en/abkhazia-maps/", true, true},
		{"/es/algo/", false, false},
		{"/english-page/", true, false}, // không phải tiền tố /en/
	}
	for _, c := range cases {
		if got := s.allowed(c.p); got != c.allowed {
			t.Errorf("allowed(%s) = %v, muốn %v", c.p, got, c.allowed)
		}
		if got := s.isLegacy(c.p); got != c.legacy {
			t.Errorf("isLegacy(%s) = %v, muốn %v", c.p, got, c.legacy)
		}
	}

	// Chưa biết map blog (khởi động chưa hỏi được site): không coi là kiểu cũ, và không cho ghi ngoài write_blogs.
	s2 := &serveSite{cfg: s.cfg, blogs: map[int]string{}}
	if s2.isLegacy("/en/x/") || s2.allowed("/en/x/") {
		t.Errorf("không có map blog: /en/x/ phải bị chặn")
	}
}
