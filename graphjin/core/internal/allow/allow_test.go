package allow

import (
	"testing"
)

func TestGQLName1(t *testing.T) {
	var q = `
	query {
		products(
			distinct: [price]
			where: { id: { and: { greater_or_equals: 20, lt: 28 } } }
		) { id name } }`

	name := QueryName(q)

	if name != "" {
		t.Fatal("Name should be empty, not ", name)
	}
}

func TestGQLName2(t *testing.T) {
	var q = `
	query hakuna_matata
	
	{
		products(
			distinct: [price]
			where: { id: { and: { greater_or_equals: 20, lt: 28 } } }
		) {
			id
			name
		}
	}`

	name := QueryName(q)

	if name != "hakuna_matata" {
		t.Fatal("Name should be 'hakuna_matata', not ", name)
	}
}

func TestGQLName3(t *testing.T) {
	var q = `
	mutation means{ users { id } }`

	// var v2 = `   { products( limit: 30, order_by: { price: desc }, distinct: [ price ] where: { id: { and: { greater_or_equals: 20, lt: 28 } } }) { id name price user { id email } } } `

	name := QueryName(q)

	if name != "means" {
		t.Fatal("Name should be 'means', not ", name)
	}
}

func TestGQLName4(t *testing.T) {
	var q = `
	query no_worries 
		users {
			id
		}
	}`

	name := QueryName(q)

	if name != "no_worries" {
		t.Fatal("Name should be 'no_worries', not ", name)
	}
}

func TestGQLName5(t *testing.T) {
	var q = `
	    {
		users {
			id
		}
	}`

	name := QueryName(q)

	if len(name) != 0 {
		t.Fatal("Name should be empty, not ", name)
	}
}

func TestParse1(t *testing.T) {
	var al = `
 # Hello world

	variables {
		"data": {
			"slug": "",
			"body": "",
			"post": {
				"connect": {
					"slug": ""
				}
			}
		}
	}
	
	mutation createComment {
		comment(insert: $data) {
			slug
			body
			createdAt: created_at
			totalVotes: cached_votes_total
			totalReplies: cached_replies_total
			vote: comment_vote(where: {user_id: {eq: $user_id}}) {
				created_at
				__typename
			}
			author: user {
				slug
				firstName: first_name
				lastName: last_name
				pictureURL: picture_url
				bio
				__typename
			}
			__typename
		}
	}
	
	# Query named createPost
	
	query createPost {
		post(insert: $data) {
			slug
			body
			published
			createdAt: created_at
			totalVotes: cached_votes_total
			totalComments: cached_comments_total
			vote: post_vote(where: {user_id: {eq: $user_id}}) {
				created_at
				__typename
			}
			author: user {
				slug
				firstName: first_name
				lastName: last_name
				pictureURL: picture_url
				bio
				__typename
			}
			__typename
		}
	}`

	_, err := parseQuery(al)
	if err != nil {
		t.Fatal(err)
	}
}

func TestParse2(t *testing.T) {
	var al = `
 /* Hello world */

	variables {
		"data": {
			"slug": "",
			"body": "",
			"post": {
				"connect": {
					"slug": ""
				}
			}
		}
	}
	
	mutation createComment {
		comment(insert: $data) {
			slug
			body
			createdAt: created_at
			totalVotes: cached_votes_total
			totalReplies: cached_replies_total
			vote: comment_vote(where: {user_id: {eq: $user_id}}) {
				created_at
				__typename
			}
			author: user {
				slug
				firstName: first_name
				lastName: last_name
				pictureURL: picture_url
				bio
				__typename
			}
			__typename
		}
	}
	
	/* 
	Query named createPost 
	*/
	
	variables {
		"data": {
			"thread": {
				"connect": {
					"slug": ""
				}
			},
			"slug": "",
			"published": false,
			"body": ""
		}
	}
	
	query createPost {
		post(insert: $data) {
			slug
			body
			published
			createdAt: created_at
			totalVotes: cached_votes_total
			totalComments: cached_comments_total
			vote: post_vote(where: {user_id: {eq: $user_id}}) {
				created_at
				__typename
			}
			author: user {
				slug
				firstName: first_name
				lastName: last_name
				pictureURL: picture_url
				bio
				__typename
			}
			__typename
		}
	}`

	_, err := parseQuery(al)
	if err != nil {
		t.Fatal(err)
	}
}
