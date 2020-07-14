// Example script to seed database

var users = [];
var pwd = "12345";

var user_count = 3;
var customer_count = 100;
var product_count = 50;
var purchase_count = 100;
var notifications_count = 100;

var users = [];

// fake admin users aka shop owners
for (i = 0; i < 3; i++) {
  var data = {
    full_name: fake.name(),
    avatar: fake.avatar_url(),
    phone: fake.phone(),
    email: "user" + i + "@demo.com",
    password: pwd,
    password_confirmation: pwd,
    created_at: "now",
    updated_at: "now",
  };

  var res = graphql(
    " \
	mutation { \
		user(insert: $data) { \
			id \
		} \
	}",
    { data: data },
    { user_id: -1 }
  );

  users.push(res.user);
}

// more fake users with random email id's
for (i = 0; i < user_count; i++) {
  var data = {
    full_name: fake.name(),
    avatar: fake.avatar_url(),
    phone: fake.phone(),
    email: fake.email(),
    password: pwd,
    password_confirmation: pwd,
    created_at: "now",
    updated_at: "now",
  };

  var res = graphql(
    " \
	mutation { \
		user(insert: $data) { \
			id \
		} \
	}",
    { data: data },
    { user_id: -1 }
  );

  users.push(res.user);
}

var customers = [];

// we also need customers
for (i = 0; i < customer_count; i++) {
  var u = users[Math.floor(Math.random() * user_count)];
  var data = {
    stripe_id: fake.uuid(),
    full_name: fake.name(),
    phone: fake.phone(),
    email: fake.email(),
    password: pwd,
    password_confirmation: pwd,
  };

  var res = graphql(
    " \
	mutation { \
		customer(insert: $data) { \
			id \
		} \
	}",
    { data: data },
    { user_id: u.id }
  );

  customers.push(res.customer);
}

var products = [];

// now the products
for (i = 0; i < product_count; i++) {
  var desc = [fake.beer_style(), fake.beer_hop(), fake.beer_malt()].join(", ");
  var u = users[Math.floor(Math.random() * user_count)];

  var data = {
    name: fake.beer_name(),
    description: desc,
    price: Math.random() * 19.0,
  };

  var res = graphql(
    " \
  mutation { \
  	product(insert: $data) { \
  		id \
  	} \
  }",
    { data: data },
    { user_id: u.id }
  );

  products.push(res.product);
}

var purchases = [];

// and then the purchases
for (i = 0; i < purchase_count; i++) {
  var u = users[Math.floor(Math.random() * user_count)];
  var p = products[Math.floor(Math.random() * product_count)];

  var data = {
    quantity: Math.floor(Math.random() * 10),
    customers: {
      connect: { id: u.id },
    },
    products: {
      connect: { id: p.id },
    },
  };

  var res = graphql(
    " \
  mutation { \
  	purchase(insert: $data) { \
  		id \
  	} \
  }",
    { data: data },
    { user_id: u.id }
  );

  purchases.push(res.purchase);
}

var notifications = [];

// and finally lets add some notifications
for (i = 0; i < notifications_count; i++) {
  var u = users[Math.floor(Math.random() * user_count)];
  var p = products[Math.floor(Math.random() * product_count)];
  var keys = ["liked", "joined"];
  var k = keys[Math.floor(Math.random() * keys.length)];

  var subject_id = 0;
  var subject_type = "";

  if (k == "liked") {
    subject_type = "products";
    subject_id = Math.floor(Math.random() * product_count);
  } else {
    subject_type = "users";
    subject_id = Math.floor(Math.random() * user_count);
  }

  var data = {
    key: k,
    subject_type: subject_type,
    subject_id: subject_id,
    user: {
      connect: { id: u.id },
    },
  };

  var res = graphql(
    " \
  mutation { \
  	notification(insert: $data) { \
  		id \
  	} \
  }",
    { data: data },
    { user_id: u.id }
  );

  notifications.push(res.notification);
}
