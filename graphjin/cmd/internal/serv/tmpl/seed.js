// Example script to seed database

var users = [];

for (i = 0; i < 10; i++) {
	var data = {
		full_name: fake.name(),
		email:     fake.email()
	}

	var res = graphql(" \
	mutation { \
		user(insert: $data) { \
			id \
		} \
	}", { data: data })

	users.push(res.user)
}