// Example script to seed database

let users = [];

for (let i = 0; i < 10; i++) {
  let q = `
  mutation {
		users(insert: {
      id: $id,
      email: $email
      full_name: $fullName,
      created_at: "now",
      products: {
        id: $productId,
        name: $productName
      }
    }) {
			id
		}
	}`;

  let res = graphql(q, {
    id: i,
    email: fake.email(),
    fullName: fake.name(),
    productId: i,
    productName: fake.name(),
  });

  users = users.concat(res.users);
}
