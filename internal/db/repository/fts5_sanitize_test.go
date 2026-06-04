package repository

import "testing"

func TestSanitizeFTS5Query(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "empty",
			in:   "",
			want: "",
		},
		{
			name: "whitespace only",
			in:   "   \t\n",
			want: "",
		},
		{
			name: "punctuation only is dropped",
			in:   "= : * \" ( ) - + ^ ~ , .",
			want: "",
		},
		{
			// Reproduces the production failure: `name = "Foo"` → fts5 syntax error
			name: "equals expression with quoted value",
			in:   `name = "Foo"`,
			want: `"name" "foo"`,
		},
		{
			name: "snake_case identifier preserved",
			in:   "find_call_chain",
			want: `"find_call_chain"`,
		},
		{
			name: "boolean keyword lowercased to avoid operator semantics",
			in:   "AND OR NOT",
			want: `"and" "or" "not"`,
		},
		{
			name: "mixed punctuation and unicode",
			in:   `请搜索 user.Login()`,
			want: `"请搜索" "user" "login"`,
		},
		{
			name: "embedded double quote escaped",
			// FTS5 phrase escaping: "" inside "..."; our tokenizer strips "
			// before phrase wrapping, so this still produces clean phrases.
			in:   `say "hello"`,
			want: `"say" "hello"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeFTS5Query(tc.in)
			if got != tc.want {
				t.Errorf("sanitizeFTS5Query(%q) = %q; want %q", tc.in, got, tc.want)
			}
		})
	}
}