//go:build gofuzz
// +build gofuzz

package jsn_test

import (
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/jsn"
)

var ret int

func TestFuzzCrashers(t *testing.T) {
	crashers := []string{
		"00\"0000\"0{",
		"6\",\n\t\t\t\"something\": " +
			"null\n\t\t},\n\t\t{\n\t\t\t\"id" +
			"\": 12,\n\t\t\t\"full_name" +
			"\": \"Brenton Bauch Ph" +
			"D\",\n\t\t\t\"email\": \"ren" +
			"ee@miller.co\",\n\t\t\t\"_" +
			"_twitter_id\": 1\n\t\t}," +
			"\n\t\t{\n\t\t\t\"id\": 13,\n\t\t" +
			"\t\"full_name\": \"Daine" +
			" Gleichner\",\n\t\t\t\"ema" +
			"il\": \"andrea@gmail.c" +
			"om\",\n\t\t\t\"__twitter_i" +
			"d\": \"\",\n\t\t\t\"id__twit" +
			"ter_id\": \"NOOO\",\n\t\t\t" +
			"\"work_email\": \"andre" +
			"a@nienow.co\"\n\t\t}\n\t]}" +
			"\n\t}",
		"0000\"0000\"0{",
		"0000\"\"{",
		"0000\"000\"{",
		"0\"\"{",
		"\"0\"{",
		"000\"0\"{",
		"0\"0000\"0{",
		"000\"\"{",
		"0\"00\"{",
		"000\"0000\"0{",
		"000\"00\"{",
		"\"\"{",
		"0\"0000\"{",
		"\"000\"00{",
		"0000\"00\"{",
		"00\"0\"{",
		"0\"0\"{",
		"000\"0000\"{",
		"00\"0000\"{",
		"0000\"0000\"{",
		"\"000\"{",
		"00\"00\"{",
		"00\"0000\"00{",
		"0\"0000\"00{",
		"00\"\"{",
		"0000\"0\"{",
		"000\"000\"{",
		"\"00000000\"{",
		`0000"00"00000000"000000000"00"000000000000000"00000"00000": "00"0"__twitter_id": [{ "name": "hello" }, { "name": "world"}]`,
		`0000"000000000000000000000000000000000000"00000000"000000000"00"000000000000000"00000"00000": "00000000000000"00000"__twitter_id": [{ "name": "hello" }, { "name": "world"}]`,
		`00"__twitter_id":[{ "name": "hello" }, { "name": "world"}]`,
		"\"\xb0\xef\xbd\xe3\xbd\xef\x99\xe3\xbd\xef\xbd\xef\xbd\xef\xbd\xe5\x99\xe3\xbd" +
			"\xef\x99\xe3\"",
		"\"\xef\xe3\xef\xe3\xe3\xe3\xef\xe3\xe3\xef\xe3\xef\xe3\xe3\xe3\xef\xe3\xef\xe3" +
			"\xe3\xef\xef\xef\xe5\xe3\xef\xe3\xc6\xef\xef\xef\xe5\xe3\xef\xe3\xc6\xef\xef\"",
	}

	for _, f := range crashers {
		ret = jsn.Fuzz([]byte(f))
	}
}
