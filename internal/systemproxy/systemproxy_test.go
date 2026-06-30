package systemproxy

import "testing"

func TestRegValueMissing(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"ERROR: The system was unable to find the specified registry key or value.", true},
		{"错误: 系统找不到指定的注册表值或项。", true},
		{"ERROR: Access is denied.", false},
	}
	for _, tc := range cases {
		if got := regValueMissing(tc.text); got != tc.want {
			t.Fatalf("regValueMissing(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}
