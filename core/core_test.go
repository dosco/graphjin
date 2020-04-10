package core

/*

func simpleMutation(t *testing.T) {
	gql := `mutation {
		product(id: 15, insert: { name: "Test", price: 20.5 }) {
			id
			name
		}
	}`

	sql := `test`

	backgroundCtx := context.Background()
	ctx := &coreContext{Context: backgroundCtx}

	resSQL, err := compileGQLToPSQL(gql)
	if err != nil {
		t.Fatal(err)
	}

	fmt.Println(">", string(resSQL))

	if string(resSQL) != sql {
		t.Fatal(errNotExpected)
	}
}

func TestCompileGQL(t *testing.T) {
	t.Run("withComplexArgs", withComplexArgs)
	t.Run("simpleMutation", simpleMutation)
}

*/
