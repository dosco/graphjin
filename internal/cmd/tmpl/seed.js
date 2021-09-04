// Example script to seed database

let users = [];

for (let i = 0; i < 10; i++) {
  let data = {
    id: i,
    full_name: fake.name(),
    email: fake.email(),
    created_at: "now",
  };

  let q = `
  mutation {
		users(insert: $data) {
			id
		}
	}`;

  let res = graphql(q, {
    data: data,
  });

  users = users.concat(res.users);
}
