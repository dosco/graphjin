// Example script to seed database

let user_count = 3;
let customer_count = 100;
let product_count = 50;
let purchase_count = 100;
let notifications_count = 100;
let comments_count = 100;

// ---- add users

let users = [];
let pwd = "12345";

for (let i = 0; i < 3; i++) {
  let data = {
    full_name: fake.name(),
    avatar: fake.avatar_url(),
    phone: fake.phone(),
    email: "user" + i + "@demo.com",
    password: pwd,
    password_confirmation: pwd,
    created_at: "now",
    updated_at: "now",
  };

  let q = `
	mutation {
		users(insert: $data) {
			id
		}
	}`;

  let res = graphql(q, { data: data });

  users = users.concat(res.users);
}

// more fake users with random email id's
for (let i = 0; i < user_count; i++) {
  let data = {
    full_name: fake.name(),
    avatar: fake.avatar_url(),
    phone: fake.phone(),
    email: "user_" + i + "_" + fake.email(),
    password: pwd,
    password_confirmation: pwd,
    created_at: "now",
    updated_at: "now",
  };

  let q = `
	mutation {
		users(insert: $data) {
			id
		}
	}`;

  let res = graphql(q, { data: data });

  users = users.concat(res.users);
}

// ---- add customers

let customers = [];

// we also need customers
for (let i = 0; i < customer_count; i++) {
  let u = users[Math.floor(Math.random() * user_count)];
  let data = {
    stripe_id: "ch_" + Math.floor(Math.random() * 100),
    full_name: fake.name(),
    phone: fake.phone(),
    email: fake.email(),
    password: pwd,
    password_confirmation: pwd,
  };

  let q = `mutation {
		customers(insert: $data) {
			id
		}
	}`;

  let res = graphql(q, { data: data }, { user_id: u.id });

  customers = customers.concat(res.customers);
}

// ---- define some sections

let categories = [
  {
    id: 1,
    name: "Beers",
    description: "Liquid Bread",
    created_at: "now",
    updated_at: "now",
  },
  {
    id: 2,
    name: "Alcohol",
    description: "Bad for you!",
    created_at: "now",
    updated_at: "now",
  },
];

// ---- add those sections using bulk insert

let q = `mutation {
  categories(insert: $categories) {
    id
  } 
}
`;
let res = graphql(q, { categories: categories }, { user_id: 1 });

// ---- add products

let products = [];

for (let i = 0; i < product_count; i++) {
  let desc = [fake.beer_style(), fake.beer_hop(), fake.beer_malt()].join(", ");
  let u = users[Math.floor(Math.random() * user_count)];

  // categories needs connecting since they are
  // a related to items in a diffent table
  // while tags can just be anything.

  let tag_list = fake.hipster_sentence(5);
  let tags = tag_list.substring(0, tag_list.length - 1).split(" ");

  let data = {
    name: fake.beer_name(),
    description: desc,
    price: Math.random() * 19.0,
    tags: tags,
    categories: {
      connect: { id: [1, 2] },
    },
  };

  let q = `mutation {
  	products(insert: $data) {
  		id
  	}
  }`;

  let res = graphql(q, { data: data }, { user_id: u.id });

  products = products.concat(res.products);
}

// ---- add purchases (joining customers with products)

let purchases = [];

for (let i = 0; i < purchase_count; i++) {
  let u = users[Math.floor(Math.random() * user_count)];
  let p = products[Math.floor(Math.random() * product_count)];

  let data = {
    quantity: Math.floor(Math.random() * 10),
    customers: {
      connect: { id: u.id },
    },
    products: {
      connect: { id: p.id },
    },
  };

  let q = `mutation {
  	purchases(insert: $data) {
  		id
  	}
  }`;

  let res = graphql(q, { data: data }, { user_id: u.id });

  purchases = purchases.concat(res.purchases);
}

// ---- add notifications

let notifications = [];

for (let i = 0; i < notifications_count; i++) {
  let u = users[Math.floor(Math.random() * user_count)];
  let p = products[Math.floor(Math.random() * product_count)];
  let keys = ["liked", "joined"];
  let k = keys[Math.floor(Math.random() * keys.length)];

  let subject_id = 0;
  let subject_type = "";

  if (k == "liked") {
    subject_type = "products";
    subject_id = Math.floor(Math.random() * product_count);
  } else {
    subject_type = "users";
    subject_id = Math.floor(Math.random() * user_count);
  }

  let data = {
    key: k,
    subject_type: subject_type,
    subject_id: subject_id,
    user: {
      connect: { id: u.id },
    },
  };

  let q = `mutation {
  	notifications(insert: $data) {
  		id
  	}
  }`;

  let res = graphql(q, { data: data }, { user_id: u.id });

  notifications = notifications.concat(res.notifications);
}

// ---- add comments

let comments = [];

for (let i = 0; i < comments_count; i++) {
  let u = users[Math.floor(Math.random() * user_count)];
  let p = products[Math.floor(Math.random() * product_count)];

  let data = {
    body: fake.sentence(10),
    created_at: "now",
    updated_at: "now",
    user: {
      connect: { id: u.id },
    },
    product: {
      connect: { id: p.id },
    },
  };

  if (comments.length !== 0) {
    let c = comments[Math.floor(Math.random() * comments.length)];
    data["comments"] = {
      find: "children",
      connect: { id: c.id },
    };
  }

  let q = `  mutation {
  	comments(insert: $data) {
  		id
  	} 
  }`;

  let res = graphql(q, { data: data }, { user_id: u.id });

  comments = comments.concat(res.comments);
}
