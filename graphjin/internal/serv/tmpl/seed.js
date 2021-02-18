// Example script to seed database

var users = [];

for (i = 0; i < 10; i++) {
  var data = {
    id: i,
    full_name: fake.name(),
    email: fake.email(),
    created_at: "now",
  };

  var res = graphql(" \
	mutation { \
		users(insert: $data) { \
			id \
		} \
	}", {
    data: data,
  });

  users = users.concat(res.users);
}
