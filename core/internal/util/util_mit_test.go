/*
The MIT License (MIT)

Copyright (c) 2015 Ian Coleman
Copyright (c) 2018 Ma_124, <github.com/Ma124>

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, Subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or Substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
*/

package util

import "testing"

func TestToSnake(t *testing.T) {
	cases := [][]string{
		{"testCase", "test_case"},
		{"TestCase", "test_case"},
		{"Test Case", "test_case"},
		{" Test Case", "test_case"},
		{"Test Case ", "test_case"},
		{" Test Case ", "test_case"},
		{"test", "test"},
		{"test_case", "test_case"},
		{"Test", "test"},
		{"", ""},
		{"ManyManyWords", "many_many_words"},
		{"manyManyWords", "many_many_words"},
		{"AnyKind of_string", "any_kind_of_string"},
		{"numbers2and55with000", "numbers_2_and_55_with_000"},
		{"JSONData", "json_data"},
		{"userID", "user_id"},
		{"AAAbbb", "aa_abbb"},
		{"1A2", "1_a_2"},
		{"A1B", "a_1_b"},
		{"A1A2A3", "a_1_a_2_a_3"},
		{"A1 A2 A3", "a_1_a_2_a_3"},
		{"AB1AB2AB3", "ab_1_ab_2_ab_3"},
		{"AB1 AB2 AB3", "ab_1_ab_2_ab_3"},
		{"some string", "some_string"},
		{" some string", "some_string"},
	}
	for _, i := range cases {
		in := i[0]
		out := i[1]
		result := ToSnake(in)
		if result != out {
			t.Errorf("%q (%q != %q)", in, result, out)
		}
	}
}

func TestToCamel(t *testing.T) {
	cases := [][]string{
		{"foo-bar", "fooBar"},
		{"TestCase", "testCase"},
		{"", ""},
		{"AnyKind of_string", "anyKindOfString"},
		{"AnyKind.of-string", "anyKindOfString"},
		{"ID", "id"},
		{"some string", "someString"},
		{" some string", "someString"},
	}
	for _, i := range cases {
		in := i[0]
		out := i[1]
		result := ToCamel(in)
		if result != out {
			t.Errorf("%q (%q != %q)", in, result, out)
		}
	}
}
