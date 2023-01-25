package jsn_test

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/jsn"
)

var (
	input1 = `
	{ 
	"data": {
	"test_1a": { "__twitter_id": "ABCD" },
	"users": [
		{
			"id": 1,
			"full_name": "'Sidney St[1]roman'",
			"email": "user0@demo.com",
			"__twitter_id": "2048666903444506956",
			"embed": {
				"id": 8,
				"full_name": "Caroll Orn Sr's",
				"email": "joannarau@hegmann.io",
				"__twitter_id": "ABC123"
				"more": [{
					"__twitter_id": "more123",
					"hello: "world
				}]
			}
		},
		{
			"id": 2,
			"full_name": "Jerry Dickinson",
			"email": "user1@demo.com",
			"__twitter_id": [{ "name": "hello" }, { "name": "world"}]
		},
		{
			"id": 3,
			"full_name": "Kenna Cassin",
			"email": "user2@demo.com",
			"__twitter_id": { "name": "\"hellos\"", "address": { "work": "1 infinity loop" } }
		},
		{
			"id": 4,
			"full_name": "Mr. Pat Parisian",
			"email": "__twitter_id",
			"__twitter_id": 1234567890
		},
		{
			"id": 5,
			"full_name": "Bette Ebert",
			"email": "janeenrath@goyette.com",
			"__twitter_id": 1.23E
		},
		{
			"id": 6,
			"full_name": "Everett Kiehn",
			"email": "michael@bartoletti.com",
			"__twitter_id": true
		},
		{
			"id": 7,
			"full_name": "Katrina Cronin",
			"email": "loretaklocko@framivolkman.org",
			"__twitter_id": false
		},
		{
			"id": 8,
			"full_name": "Caroll Orn Sr.",
			"email": "joannarau@hegmann.io",
			"__twitter_id": "2048666903444506956"
		},
		{
			"id": 9,
			"full_name": "Gwendolyn Ziemann",
			"email": "renaytoy@rutherford.co",
			"__twitter_id": ["hello", "world"]
		},
		{
			"id": 10,
			"full_name": "Mrs. Rosann Fritsch",
			"email": "holliemosciski@thiel.org",
			"__twitter_id": "2048666903444506956"
		},
		{
			"id": 11,
			"full_name": "Arden Koss",
			"email": "cristobalankunding@howewelch.org",
			"__twitter_id": "2048666903444506956",
			"something": null
		},
		{
			"id": 12,
			"full_name": "Brenton Bauch PhD",
			"email": "renee@miller.co",
			"__twitter_id": 1
		},
		{
			"id": 13,
			"full_name": "Daine Gleichner",
			"email": "andrea@gmail.com",
			"__twitter_id": "",
			"id__twitter_id": "NOOO",
			"work_email": "andrea@nienow.co"
		}
	]}
	}`

	input2 = `
	[{
		"id": 1,
		"full_name": "Sidney St[1]roman",
		"email": "user0@demo.com",
		"__twitter_id": "2048666903444506956",
		"something": null,
		"embed": {
			"id": 8,
			"full_name": "Caroll Orn Sr.",
			"email": "joannarau@hegmann.io",
			"__twitter_id": "ABC123"
		}
	},
	{
		"m": 1,
		"id": 2,
		"full_name": "Jerry Dickinson",
		"email": "user1@demo.com",
		"__twitter_id": [{ "name": "hello" }, { "name": "world"}]
	}]`

	input3 = `
	{ 
		"data": {
			"test_1a": { "__twitter_id": "ABCD" },
			"users": [{"id":1,"embed":{"id":8}},{"id":2},{"id":3},{"id":4},{"id":5},{"id":6},{"id":7},{"id":8},{"id":9},{"id":10},{"id":11},{"id":12},{"id":13}]
		}
	}`

	input4 = `
	{ "users" : [{
		"id": 1,
		"full_name": "Sidney St[1]roman",
		"email": "user0@demo.com",
		"__twitter_id": "2048666903444506956",
		"embed": {
			"id": 8,
			"full_name": null,
			"email": "joannarau@hegmann.io",
			"__twitter_id": "ABC123"
		}
	},
	{
		"m": 1,
		"id": 2,
		"full_name": "Jerry Dickinson",
		"email": "user1@demo.com",
		"__twitter_id": [{ "name": "hello" }, { "name": "world"}]
	}] }`

	input5 = `
	{"data":{"title":"In September 2018, Slovak police stated that Kuciak was murdered because of his investigative work, and that the murder had been ordered.[9][10] They arrested eight suspects,[11] charging three of them with first-degree murder.[11]","topics":["cpp"]},"a":["1111"]},"thread_slug":"in-september-2018-slovak-police-stated-that-kuciak-7929",}`

	input6 = `
	{"users" : [{"id" : 1, "email" : "vicram@gmail.com", "slug" : "vikram-rangnekar", "threads" : [], "threads_cursor" : null}, {"id" : 3, "email" : "marareilly@lang.name", "slug" : "raymundo-corwin", "threads" : [{"id" : 9, "title" : "Et alias et aut porro praesentium nam in voluptatem reiciendis quisquam perspiciatis inventore eos quia et et enim qui amet."}, {"id" : 25, "title" : "Ipsam quam nemo culpa tempore amet optio sit sed eligendi autem consequatur quaerat rem velit quibusdam quibusdam optio a voluptatem."}], "threads_cursor" : 25}], "users_cursor" : 3}`

	input7, _ = os.ReadFile("test7.json")

	input8, _ = os.ReadFile("test8.json")
)

func TestGet(t *testing.T) {
	values := jsn.Get([]byte(input1), [][]byte{
		[]byte("test_1a"),
		[]byte("__twitter_id"),
		[]byte("work_email"),
	})

	expected := []jsn.Field{
		{[]byte("test_1a"), []byte(`{ "__twitter_id": "ABCD" }`)},
		{[]byte("__twitter_id"), []byte(`"ABCD"`)},
		{[]byte("__twitter_id"), []byte(`"2048666903444506956"`)},
		{[]byte("__twitter_id"), []byte(`"ABC123"`)},
		{[]byte("__twitter_id"), []byte(`"more123"`)},
		{[]byte("__twitter_id"), []byte(`[{ "name": "hello" }, { "name": "world"}]`)},
		{[]byte("__twitter_id"), []byte(`{ "name": "\"hellos\"", "address": { "work": "1 infinity loop" } }`)},
		{[]byte("__twitter_id"), []byte(`1234567890`)},
		{[]byte("__twitter_id"), []byte(`1.23E`)},
		{[]byte("__twitter_id"), []byte(`true`)},
		{[]byte("__twitter_id"), []byte(`false`)},
		{[]byte("__twitter_id"), []byte(`"2048666903444506956"`)},
		{[]byte("__twitter_id"), []byte(`["hello", "world"]`)},
		{[]byte("__twitter_id"), []byte(`"2048666903444506956"`)},
		{[]byte("__twitter_id"), []byte(`"2048666903444506956"`)},
		{[]byte("__twitter_id"), []byte(`1`)},
		{[]byte("__twitter_id"), []byte(`""`)},
		{[]byte("work_email"), []byte(`"andrea@nienow.co"`)},
	}

	if len(values) != len(expected) {
		t.Fatal("len(values) != len(expected)")
	}

	for i := range expected {
		if !bytes.Equal(values[i].Key, expected[i].Key) {
			t.Error(string(values[i].Key), " != ", string(expected[i].Key))
		}

		if !bytes.Equal(values[i].Value, expected[i].Value) {
			t.Error(string(values[i].Value), " != ", string(expected[i].Value))
		}
	}
}

func TestGet1(t *testing.T) {
	values := jsn.Get([]byte(input5), [][]byte{
		[]byte("thread_slug"),
	})

	expected := []jsn.Field{
		{[]byte("thread_slug"), []byte(`"in-september-2018-slovak-police-stated-that-kuciak-7929"`)},
	}

	if len(values) != len(expected) {
		t.Fatal("len(values) != len(expected)")
	}

	for i := range expected {
		if !bytes.Equal(values[i].Key, expected[i].Key) {
			t.Error(string(values[i].Key), " != ", string(expected[i].Key))
		}

		if !bytes.Equal(values[i].Value, expected[i].Value) {
			t.Error(string(values[i].Value), " != ", string(expected[i].Value))
		}
	}
}

func TestGet2(t *testing.T) {
	values := jsn.Get([]byte(input6), [][]byte{
		[]byte("users_cursor"), []byte("threads_cursor"),
	})

	expected := []jsn.Field{
		{[]byte("threads_cursor"), []byte(`null`)},
		{[]byte("threads_cursor"), []byte(`25`)},
		{[]byte("users_cursor"), []byte(`3`)},
	}

	if len(values) != len(expected) {
		t.Fatal("len(values) != len(expected)")
	}

	for i := range expected {
		if !bytes.Equal(values[i].Key, expected[i].Key) {
			t.Error(string(values[i].Key), " != ", string(expected[i].Key))
		}

		if !bytes.Equal(values[i].Value, expected[i].Value) {
			t.Error(string(values[i].Value), " != ", string(expected[i].Value))
		}
	}
}

func TestGet3(t *testing.T) {
	values := jsn.Get(input7, [][]byte{[]byte("data")})
	v := values[0].Value

	if !bytes.Equal(v[len(v)-11:], []byte(`Rangnekar"}`)) {
		t.Fatal("corrupt ending")
	}
}

func TestGet4(t *testing.T) {
	exp := `"# \n\n@@@java\npackage main\n\nimport (\n        \"net/http\"\n        \"strings\"\n\n        \"github.com/gin-gonic/gin\"\n)\n\nfunc main() {\n        r := gin.Default()\n        r.LoadHTMLGlob(\"templates/*\")\n\n        r.GET(\"/\", handleIndex)\n        r.GET(\"/to/:name\", handleIndex)\n        r.Run()\n}\n\n// Hello is page data for the template\ntype Hello struct {\n        Name string\n}\n\nfunc handleIndex(c *gin.Context) {\n        name := c.Param(\"name\")\n        if name != \"\" {\n                name = strings.TrimPrefix(c.Param(\"name\"), \"/\")\n        }\n        c.HTML(http.StatusOK, \"hellofly.tmpl\", gin.H{\"Name\": name})\n}\n@@@\n\n\\"`

	exp = strings.ReplaceAll(exp, "@", "`")

	values := jsn.Get(input8, [][]byte{[]byte("body")})

	if string(values[0].Key) != "body" {
		t.Fatal("unexpected key")
	}

	if string(values[0].Value) != exp {
		fmt.Println(string(values[0].Value))
		t.Fatal("unexpected value")
	}
}

func TestValue(t *testing.T) {
	v1 := []byte("12345")
	if !bytes.Equal(jsn.Value(v1), v1) {
		t.Fatal("Number value invalid")
	}

	v2 := []byte(`"12345"`)
	if !bytes.Equal(jsn.Value(v2), []byte(`12345`)) {
		t.Fatal("String value invalid")
	}

	v3 := []byte(`{ "hello": "world" }`)
	if jsn.Value(v3) != nil {
		t.Fatal("Object value is not nil", jsn.Value(v3))
	}

	v4 := []byte(`[ "hello", "world" ]`)
	if jsn.Value(v4) != nil {
		t.Fatal("List value is not nil")
	}
}

func TestFilter1(t *testing.T) {
	var b bytes.Buffer
	err := jsn.Filter(&b, []byte(input2), []string{"id", "full_name", "embed"})
	if err != nil {
		t.Error(err)
		return
	}

	expected := `[{"id": 1,"full_name": "Sidney St[1]roman","embed": {"id": 8,"full_name": "Caroll Orn Sr.","email": "joannarau@hegmann.io","__twitter_id": "ABC123"}},{"id": 2,"full_name": "Jerry Dickinson"}]`

	if b.String() != expected {
		t.Error("Does not match expected json")
	}
}

func TestFilter2(t *testing.T) {
	value := `[{"id":1,"customer_id":"cus_2TbMGf3cl0","object":"charge","amount":100,"amount_refunded":0,"date":"01/01/2019","application":null,"billing_details":{"address":"1 Infinity Drive","zipcode":"94024"}},   {"id":2,"customer_id":"cus_2TbMGf3cl0","object":"charge","amount":150,"amount_refunded":0,"date":"02/18/2019","billing_details":{"address":"1 Infinity Drive","zipcode":"94024"}},{"id":3,"customer_id":"cus_2TbMGf3cl0","object":"charge","amount":150,"amount_refunded":50,"date":"03/21/2019","billing_details":{"address":"1 Infinity Drive","zipcode":"94024"}}]`

	var b bytes.Buffer
	err := jsn.Filter(&b, []byte(value), []string{"id"})
	if err != nil {
		t.Error(err)
		return
	}

	expected := `[{"id":1},{"id":2},{"id":3}]`

	if b.String() != expected {
		t.Error("Does not match expected json")
	}
}

func TestStrip(t *testing.T) {
	path1 := [][]byte{[]byte("data"), []byte("users")}
	value1 := jsn.Strip([]byte(input3), path1)

	expected := []byte(`[{"id":1,"embed":{"id":8}},{"id":2},{"id":3},{"id":4},{"id":5},{"id":6},{"id":7},{"id":8},{"id":9},{"id":10},{"id":11},{"id":12},{"id":13}]`)

	if !bytes.Equal(value1, expected) {
		t.Log(value1)
		t.Error("[Valid path] Does not match expected json")
	}

	path2 := [][]byte{[]byte("boo"), []byte("hoo")}
	value2 := jsn.Strip([]byte(input3), path2)

	if !bytes.Equal(value2, []byte(input3)) {
		t.Log(value2)
		t.Error("[Invalid path] Does not match expected json")
	}
}

func TestValidateTrue(t *testing.T) {
	json := []byte(`  [{"id":1,"embed":{"id":8}},{"id":2},{"id":3},{"id":4},{"id":5},{"id":6},{"id":7},{"id":8},{"id":9},{"id":10},{"id":11},{"id":12},{"id":13}]`)

	err := jsn.Validate(string(json))
	if err != nil {
		t.Error(err)
		return
	}
}

func TestValidateFalse(t *testing.T) {
	json := []byte(`   [{ "hello": 123"<html>}]`)

	err := jsn.Validate(string(json))
	if err == nil {
		t.Error("JSON validation failed to detect invalid json")
	}
}

func TestReplace(t *testing.T) {
	var buf bytes.Buffer

	from := []jsn.Field{
		{[]byte("__twitter_id"), []byte(`[{ "name": "hello" }, { "name": "world"}]`)},
		{[]byte("__twitter_id"), []byte(`"ABC123"`)},
	}

	to := []jsn.Field{
		{[]byte("__twitter_id"), []byte(`"1234567890"`)},
		{[]byte("some_list"), []byte(`[{"id":1,"embed":{"id":8}},{"id":2},{"id":3},{"id":4},{"id":5},{"id":6},{"id":7},{"id":8},{"id":9},{"id":10},{"id":11},{"id":12},{"id":13}]`)},
	}

	expected := `{ "users" : [{
		"id": 1,
		"full_name": "Sidney St[1]roman",
		"email": "user0@demo.com",
		"__twitter_id": "2048666903444506956",
		"embed": {
			"id": 8,
			"full_name": null,
			"email": "joannarau@hegmann.io",
			"some_list":[{"id":1,"embed":{"id":8}},{"id":2},{"id":3},{"id":4},{"id":5},{"id":6},{"id":7},{"id":8},{"id":9},{"id":10},{"id":11},{"id":12},{"id":13}]
		}
	},
	{
		"m": 1,
		"id": 2,
		"full_name": "Jerry Dickinson",
		"email": "user1@demo.com",
		"__twitter_id":"1234567890"
	}] }`

	err := jsn.Replace(&buf, []byte(input4), from, to)
	if err != nil {
		t.Fatal(err)
	}

	if buf.String() != expected {
		t.Log(buf.String())
		t.Error("Does not match expected json")
	}
}

func TestReplace1(t *testing.T) {
	var buf bytes.Buffer

	from := []jsn.Field{
		{[]byte("comments_cursor"), []byte(`"1"`)},
	}

	to := []jsn.Field{
		{[]byte("comments_cursor"), []byte(`"I1HlYnNdWOcMWSWOShaz2ysN5MDqphbFL9sNKJs="`)},
	}

	input := `{"comments": [{"id": 1}], "comments_cursor": "1"}`

	expected := `{"comments": [{"id": 1}], "comments_cursor":"I1HlYnNdWOcMWSWOShaz2ysN5MDqphbFL9sNKJs="}`

	err := jsn.Replace(&buf, []byte(input), from, to)
	if err != nil {
		t.Fatal(err)
	}

	output := buf.String()

	if output != expected {
		t.Log(output)
		t.Error("Does not match expected json")
	}
}

/*
func TestReplace2(t *testing.T) {
	var buf bytes.Buffer

	from := jsn.Get(json, [][]byte{
		[]byte("votes_cursor"),
		[]byte("bookmarks_cursor"),
		[]byte("papers_cursor")})

	var to []jsn.Field

	for _, v := range from {
		to = append(to, jsn.Field{v.Key, []byte(`"` + string(v.Key) + `_value"`)})
	}

	err := jsn.Replace(&buf, json, from, to)
	if err != nil {
		t.Fatal(err)
	}

	output := buf.String()

	if output != expected {
		t.Log(output)
		t.Error("Does not match expected json")
	}
}
*/

func TestReplaceEmpty(t *testing.T) {
	var buf bytes.Buffer

	json := `{ "users" : [{"id":1,"full_name":"Sidney St[1]roman","email":"user0@demo.com","__users_twitter_id":"2048666903444506956"}, {"id":2,"full_name":"Jerry Dickinson","email":"user1@demo.com","__users_twitter_id":"2048666903444506956"}, {"id":3,"full_name":"Kenna Cassin","email":"user2@demo.com","__users_twitter_id":"2048666903444506956"}, {"id":4,"full_name":"Mr. Pat Parisian","email":"rodney@kautzer.biz","__users_twitter_id":"2048666903444506956"}, {"id":5,"full_name":"Bette Ebert","email":"janeenrath@goyette.com","__users_twitter_id":"2048666903444506956"}, {"id":6,"full_name":"Everett Kiehn","email":"michael@bartoletti.com","__users_twitter_id":"2048666903444506956"}, {"id":7,"full_name":"Katrina Cronin","email":"loretaklocko@framivolkman.org","__users_twitter_id":"2048666903444506956"}, {"id":8,"full_name":"Caroll Orn Sr.","email":"joannarau@hegmann.io","__users_twitter_id":"2048666903444506956"}, {"id":9,"full_name":"Gwendolyn Ziemann","email":"renaytoy@rutherford.co","__users_twitter_id":"2048666903444506956"}, {"id":10,"full_name":"Mrs. Rosann Fritsch","email":"holliemosciski@thiel.org","__users_twitter_id":"2048666903444506956"}, {"id":11,"full_name":"Arden Koss","email":"cristobalankunding@howewelch.org","__users_twitter_id":"2048666903444506956"}, {"id":12,"full_name":"Brenton Bauch PhD","email":"renee@miller.co","__users_twitter_id":"2048666903444506956"}, {"id":13,"full_name":"Daine Gleichner","email":"andrea@nienow.co","__users_twitter_id":"2048666903444506956"}] }`

	err := jsn.Replace(&buf, []byte(json), []jsn.Field{}, []jsn.Field{})
	if err != nil {
		t.Fatal(err)
	}

	if buf.String() != json {
		t.Log(buf.String())
		t.Error("Does not match expected json")
	}
}

func TestKeys1(t *testing.T) {
	json := `[{"id":1,"posts": [{"title":"PT1-1","description":"PD1-1"}, {"title":"PT1-2","description":"PD1-2"}], "full_name":"FN1","email":"E1","books": [{"name":"BN1-1","description":"BD1-1"},{"name":"BN1-2","description":"BD1-2"},{"name":"BN1-2","description":"BD1-2"}]},{"id":1,"posts": [{"title":"PT1-1","description":"PD1-1"}, {"title":"PT1-2","description":"PD1-2"}], "full_name":"FN1","email":"E1","books": [{"name":"BN1-1","description":"BD1-1"},{"name":"BN1-2","description":"BD1-2"},{"name":"BN1-2","description":"BD1-2"}]},{"id":1,"posts": [{"title":"PT1-1","description":"PD1-1"}, {"title":"PT1-2","description":"PD1-2"}], "full_name":"FN1","email":"E1","books": [{"name":"BN1-1","description":"BD1-1"},{"name":"BN1-2","description":"BD1-2"},{"name":"BN1-2","description":"BD1-2"}]}]`

	fields := jsn.Keys([]byte(json))

	exp := []string{
		"id", "posts", "title", "description", "full_name", "email", "books", "name", "description",
	}

	if len(exp) != len(fields) {
		t.Errorf("Expected %d fields %d", len(exp), len(fields))
	}

	for i := range exp {
		if string(fields[i]) != exp[i] {
			t.Errorf("Expected field '%s' got '%s'", string(exp[i]), fields[i])
		}
	}
}

func TestKeys2(t *testing.T) {
	json := `{"id":1,"posts": [{"title":"PT1-1","description":"PD1-1"}, {"title":"PT1-2","description":"PD1-2"}], "full_name":"FN1","email":"E1","books": [{"name":"BN1-1","description":"BD1-1"},{"name":"BN1-2","description":"BD1-2"},{"name":"BN1-2","description":"BD1-2"}]}`

	fields := jsn.Keys([]byte(json))

	exp := []string{
		"id", "posts", "title", "description", "full_name", "email", "books", "name", "description",
	}

	if len(exp) != len(fields) {
		t.Errorf("Expected %d fields %d", len(exp), len(fields))
	}

	for i := range exp {
		if string(fields[i]) != exp[i] {
			t.Errorf("Expected field '%s' got '%s'", string(exp[i]), fields[i])
		}
	}
}

func TestKeys3(t *testing.T) {
	json := `{
		"insert": {
			"created_at": "now",
			"test_1a": { "type1": "a", "type2": "b" },
			"name": "Hello",
			"updated_at": "now",
			"description": "World"
		},
		"user": 123
	}`

	fields := jsn.Keys([]byte(json))

	exp := []string{
		"insert", "created_at", "test_1a", "type1", "type2", "name", "updated_at", "description",
		"user",
	}

	if len(exp) != len(fields) {
		t.Errorf("Expected %d fields %d", len(exp), len(fields))
	}

	for i := range exp {
		if string(fields[i]) != exp[i] {
			t.Errorf("Expected field '%s' got '%s'", string(exp[i]), fields[i])
		}
	}
}

func TestClear(t *testing.T) {
	var buf bytes.Buffer

	json := `{
		"insert": {
			"created_at": "now",
			"test_1a": { "type1": "a", "type2": [{ "a": 2 }] },
			"name": "Hello",
			"updated_at": "now",
			"description": "World"
		},
		"user": 123,
		"tags": [1, 2, "what"]
	}`

	expected := `{"insert":{"created_at":"","test_1a":{"type1":"","type2":[{"a":0.0}]},"name":"","updated_at":"","description":""},"user":0.0,"tags":[]}`

	err := jsn.Clear(&buf, []byte(json))
	if err != nil {
		t.Fatal(err)
	}

	if buf.String() != expected {
		t.Log(buf.String())
		t.Error("Does not match expected json")
	}
}

func BenchmarkGet(b *testing.B) {
	b.ReportAllocs()

	for n := 0; n < b.N; n++ {
		jsn.Get([]byte(input1), [][]byte{[]byte("__twitter_id")})
	}
}

func BenchmarkFilter(b *testing.B) {
	var buf bytes.Buffer

	keys := []string{"id", "full_name", "embed", "email", "__twitter_id"}
	b.ResetTimer()
	b.ReportAllocs()

	for n := 0; n < b.N; n++ {
		err := jsn.Filter(&buf, []byte(input2), keys)
		if err != nil {
			b.Fatal(err)
		}
		buf.Reset()
	}
}

func BenchmarkStrip(b *testing.B) {
	path := [][]byte{[]byte("data"), []byte("users")}
	b.ReportAllocs()

	for n := 0; n < b.N; n++ {
		jsn.Strip([]byte(input3), path)
	}
}

func BenchmarkReplace(b *testing.B) {
	var buf bytes.Buffer

	from := []jsn.Field{
		{[]byte("__twitter_id"), []byte(`[{ "name": "hello" }, { "name": "world"}]`)},
		{[]byte("__twitter_id"), []byte(`"ABC123"`)},
	}

	to := []jsn.Field{
		{[]byte("__twitter_id"), []byte(`"1234567890"`)},
		{[]byte("some_list"), []byte(`[{"id":1,"embed":{"id":8}},{"id":2},{"id":3},{"id":4},{"id":5},{"id":6},{"id":7},{"id":8},{"id":9},{"id":10},{"id":11},{"id":12},{"id":13}]`)},
	}

	b.ResetTimer()
	b.ReportAllocs()

	for n := 0; n < b.N; n++ {
		err := jsn.Replace(&buf, []byte(input4), from, to)
		if err != nil {
			b.Fatal(err)
		}
		buf.Reset()
	}
}

func BenchmarkTest(b *testing.B) {
	b.ResetTimer()
	b.ReportAllocs()

	var test string
	hello := "hello"

	for n := 0; n < b.N; n++ {
		var buf bytes.Buffer
		buf.WriteString(hello)
		buf.WriteString("world")
		test = buf.String()
		buf.Reset()
	}

	fmt.Println(test)
}
