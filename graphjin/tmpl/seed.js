// Example script to seed database

var users = [];

for (i = 0; i < 10; i++) {
	var pwd = fake.password()
	var data = {
		first_name: fake.first_name(),
		last_name: 	fake.last_name(),
		email: 			fake.email(),
		password: 	pwd,
		password_confirmation: pwd
	}

	var res = graphql(" \
	mutation { \
		user(insert: $data) { \
			id \
		} \
	}", { data: data })

	users.push(res.user)
}