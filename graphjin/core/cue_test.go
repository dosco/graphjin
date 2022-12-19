package core_test

import (
	"context"
	"encoding/json"
	"testing"

	core "github.com/dosco/graphjin/v2/core"
	"github.com/dosco/graphjin/v2/plugin/cue"
)

func TestCueValidationQuerySingleIntVarValue(t *testing.T) {
	gql := `query @validation(src:"id:2" type:"cue") {
		users(where: {id:$id}) {
		  id
		  full_name
		  email
		}
	  }`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db,
		core.OptionSetValidator("cue", cue.New()))
	if err != nil {
		panic(err)
	}

	_, err = gj.GraphQL(context.Background(), gql, json.RawMessage(`{"id":2}`), nil)
	if err != nil {
		t.Error(err)
		return
	}
}

func TestCueInvalidationQuerySingleIntVarValue(t *testing.T) {
	gql := `query @validation(src:"id:2" type:"cue") {
		users(where: {id:$id}) {
		  id
		  full_name
		  email
		}
	  }`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db,
		core.OptionSetValidator("cue", cue.New()))
	if err != nil {
		panic(err)
	}

	_, err = gj.GraphQL(context.Background(), gql, json.RawMessage(`{"id":3}`), nil)
	if err == nil {
		t.Error("expected validation error")
	}
}

func TestCueValidationQuerySingleIntVarType(t *testing.T) {
	gql := `query @validation(src:"id:int" type:"cue") {
		users(where: {id:$id}) {
		  id
		  full_name
		  email
		}
	  }`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db,
		core.OptionSetValidator("cue", cue.New()))
	if err != nil {
		panic(err)
	}

	_, err = gj.GraphQL(context.Background(), gql, json.RawMessage(`{"id":2}`), nil)
	if err != nil {
		t.Error(err)
		return
	}
}

func TestCueValidationQuerySingleIntVarOR(t *testing.T) {
	gql := `query @validation(src:"id: 3 | 2", type:"cue") {
		users(where: {id:$id}) {
		  id
		  full_name
		  email
		}
	  }`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db,
		core.OptionSetValidator("cue", cue.New()))
	if err != nil {
		panic(err)
	}

	_, err = gj.GraphQL(context.Background(), gql, json.RawMessage(`{"id":2}`), nil)
	if err != nil {
		t.Error(err)
		return
	}
}

func TestCueInvalidationQuerySingleIntVarOR(t *testing.T) {
	gql := `query @validation(src:"id: 3 | 2", type:"cue") {
		users(where: {id:$id}) {
		  id
		  full_name
		  email
		}
	  }`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db,
		core.OptionSetValidator("cue", cue.New()))
	if err != nil {
		panic(err)
	}

	_, err = gj.GraphQL(context.Background(), gql, json.RawMessage(`{"id":4}`), nil)
	if err == nil {
		t.Error(err)
	}
}

// TODO: validation source can be read from a local file like scripts

// func TestCueValidationQuerySingleStringVarOR(t *testing.T) {
// 	// TODO: couldn't find a way to pass string inside cue through plain graphql query ( " )
// 	// (only way is using varibales and escape \")
// 	gql := `query @validation(src:$validation type:"cue") {
// 		users(where: {email:$mail}) {
// 		  id
// 		  full_name
// 		  email
// 		}
// 	  }`

// 	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
// 	gj, err := core.NewGraphJin(conf, db)
// 	if err != nil {
// 		panic(err)
// 	}

// 	vars := json.RawMessage(`{"mail":"mail@example.com","validation":"mail: \"mail@example.com\" | \"mail@example.org\" "}`)
// 	_, err = gj.GraphQL(context.Background(), gql, vars, nil)
// 	if err != nil {
// 		t.Error(err)
// 		return
// 	}
// }

// func TestCueInvalidationQuerySingleStringVarOR(t *testing.T) {
// 	gql := `query @validation(cue:$validation) {
// 		users(where: {email:$mail}) {
// 		  id
// 		  full_name
// 		  email
// 		}
// 	  }`

// 	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
// 	gj, err := core.NewGraphJin(conf, db)
// 	if err != nil {
// 		panic(err)
// 	}

// 	_, err = gj.GraphQL(context.Background(), gql, json.RawMessage(`{"mail":"mail@example.net","validation":"mail: \"mail@example.com\" | \"mail@example.org\" "}`), nil)
// 	if err == nil {
// 		t.Error(err)
// 	}
// }

func TestCueInvalidationQuerySingleIntVarType(t *testing.T) {
	gql := `query @validation(src:"email:int" type: "cue") {
		users(where: {email:$email}) {
		  id
		  full_name
		  email
		}
	  }`

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		panic(err)
	}

	_, err = gj.GraphQL(context.Background(), gql, json.RawMessage(`{"email":"mail@example.com"}`), nil)
	if err == nil {
		t.Error("expected validation error")
	}
}

// func TestCueValidationMutationMapVarStringsLen(t *testing.T) {
// 	if dbType == "mysql" {
// 		t.SkipNow()
// 		return
// 	}
// 	gql := `mutation @validation(cue:$validation) {
// 		users(insert:$inp) {
// 		  id
// 		  full_name
// 		  email
// 		}
// 	  }`

// 	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
// 	gj, err := core.NewGraphJin(conf, db)
// 	if err != nil {
// 		panic(err)
// 	}

// 	_, err = gj.GraphQL(context.Background(), gql, json.RawMessage(`{
// 		"inp":{
// 			"id":105, "email":"mail1@example.com", "full_name":"Full Name", "created_at":"now", "updated_at":"now"
// 		},
// 		"validation":"import (\"strings\"), inp: {id?: int, full_name: string & strings.MinRunes(3) & strings.MaxRunes(22), created_at:\"now\", updated_at:\"now\", email: string}"
// 	}`), nil)
// 	if err != nil {
// 		t.Error(err)
// 		return
// 	}
// }

// func TestCueInvalidationMutationMapVarStringsLen(t *testing.T) {
// 	if dbType == "mysql" {
// 		t.SkipNow()
// 		return
// 	}
// 	gql := `mutation @validation(cue:$validation) {
// 		users(insert:$inp) {
// 		  id
// 		  full_name
// 		  email
// 		}
// 	  }`

// 	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
// 	gj, err := core.NewGraphJin(conf, db)
// 	if err != nil {
// 		panic(err)
// 	}

// 	_, err = gj.GraphQL(context.Background(), gql, json.RawMessage(`{
// 		"inp":{
// 			"id":106, "email":"mail2@example.com", "full_name":"Fu", "created_at":"now", "updated_at":"now"
// 		},
// 		"validation":"import (\"strings\"), inp: {id?: int, full_name: string & strings.MinRunes(3) & strings.MaxRunes(22), created_at:\"now\", updated_at:\"now\", email: string}"
// 	}`), nil)
// 	if err == nil {
// 		t.Error(err)
// 	}
// }

// func TestCueValidationMutationMapVarIntMaxMin(t *testing.T) {
// 	if dbType == "mysql" {
// 		t.SkipNow()
// 		return
// 	}
// 	gql := `mutation @validation(cue:$validation) {
// 		users(insert:$inp) {
// 		  id
// 		  full_name
// 		  email
// 		}
// 	  }`

// 	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
// 	gj, err := core.NewGraphJin(conf, db)
// 	if err != nil {
// 		panic(err)
// 	}

// 	_, err = gj.GraphQL(context.Background(), gql, json.RawMessage(`{
// 		"inp":{
// 			"id":101, "email":"mail3@example.com", "full_name":"Full Name", "created_at":"now", "updated_at":"now"
// 		},
// 		"validation":" inp: {id?: int & >100 & <102, full_name: string , created_at:\"now\", updated_at:\"now\", email: string}"
// 	}`), nil)
// 	if err != nil {
// 		t.Error(err)
// 		return
// 	}
// }

// func TestCueInvalidationMutationMapVarIntMaxMin(t *testing.T) {
// 	if dbType == "mysql" {
// 		t.SkipNow()
// 		return
// 	}
// 	gql := `mutation @validation(cue:$validation) {
// 		users(insert:$inp) {
// 		  id
// 		  full_name
// 		  email
// 		}
// 	  }`

// 	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
// 	gj, err := core.NewGraphJin(conf, db)
// 	if err != nil {
// 		panic(err)
// 	}

// 	_, err = gj.GraphQL(context.Background(), gql, json.RawMessage(`{
// 		"inp":{
// 			"id":107, "email":"mail4@example.com", "full_name":"Fu", "created_at":"now", "updated_at":"now"
// 		},
// 		"validation":"inp: {id?: int & >100 & <102, full_name: string , created_at:\"now\", updated_at:\"now\", email: string}"
// 	}`), nil)
// 	if err == nil {
// 		t.Error(err)
// 	}
// }

// func TestCueValidationMutationMapVarOptionalKey(t *testing.T) {
// 	if dbType == "mysql" {
// 		t.SkipNow()
// 		return
// 	}
// 	gql := `mutation @validation(cue:$validation) {
// 		users(insert:$inp) {
// 		  id
// 		  full_name
// 		  email
// 		}
// 	  }`

// 	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
// 	gj, err := core.NewGraphJin(conf, db)
// 	if err != nil {
// 		panic(err)
// 	}

// 	_, err = gj.GraphQL(context.Background(), gql, json.RawMessage(`{
// 		"inp":{
// 			"id":111, "email":"mail7@example.com", "full_name":"Fu", "created_at":"now", "updated_at":"now"
// 		},
// 		"validation":"inp: {id?: int, phone?: string, full_name: string , created_at:\"now\", updated_at:\"now\", email: string}"
// 	}`), nil)
// 	if err != nil {
// 		t.Error(err)
// 		return
// 	}
// }

// func TestCueValidationMutationMapVarRegex(t *testing.T) {
// 	if dbType == "mysql" {
// 		t.SkipNow()
// 		return
// 	}
// 	gql := `mutation @validation(cue:$validation) {
// 		users(insert:$inp) {
// 		  id
// 		  full_name
// 		  email
// 		}
// 	  }`

// 	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
// 	gj, err := core.NewGraphJin(conf, db)
// 	if err != nil {
// 		panic(err)
// 	}

// 	_, err = gj.GraphQL(context.Background(), gql, json.RawMessage(`{
// 		"inp":{
// 			"id":108, "email":"mail5@example.com", "full_name":"Full Name", "created_at":"now", "updated_at":"now"
// 		},
// 		"validation":"inp: {id?: int & >100 & <110, full_name: string , created_at:\"now\", updated_at:\"now\", email: =~\"^[a-zA-Z0-9.!#$+%&'*/=?^_{|}\\\\-`+"`"+`~]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\\\\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$\"}"
// 	}`), nil) // regex from : https://cuelang.org/play/?id=iFcZKx72Bwm#cue@export@cue
// 	if err != nil {
// 		t.Error(err)
// 		return
// 	}
// }

// func TestCueInvalidationMutationMapVarRegex(t *testing.T) {
// 	if dbType == "mysql" {
// 		t.SkipNow()
// 		return
// 	}
// 	gql := `mutation @validation(cue:$validation) {
// 		users(insert:$inp) {
// 		  id
// 		  full_name
// 		  email
// 		}
// 	  }`

// 	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true})
// 	gj, err := core.NewGraphJin(conf, db)
// 	if err != nil {
// 		panic(err)
// 	}

// 	_, err = gj.GraphQL(context.Background(), gql, json.RawMessage(`{
// 		"inp":{
// 			"id":109, "email":"mail6@ex`+"`"+`ample.com", "full_name":"Full Name", "created_at":"now", "updated_at":"now"
// 		},
// 		"validation":"inp: {id?: int & >110 & <102, full_name: string , created_at:\"now\", updated_at:\"now\", email: =~\"^[a-zA-Z0-9.!#$+%&'*/=?^_{|}\\\\-`+"`"+`~]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\\\\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$\"}"
// 	}`), nil)
// 	if err == nil {
// 		t.Error(err)
// 	}
// }
