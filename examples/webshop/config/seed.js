// Example script to seed database

var users = [];
var pwd = "12345";

var user_count = 3;
var customer_count = 100;
var product_count = 50;
var purchase_count = 100;
var notifications_count = 100;
var comments_count = 100;


for (i = 0; i < 3; i++) {
  var data = {
    full_name: fake.name(),
    avatar: fake.avatar_url(),
    phone: fake.phone(),
    email: "user" + i + "@demo.com",
    password: pwd,
    password_confirmation: pwd
  };

  var res = graphql(
      " \
      mutation { \
          users(insert: $data) { \
              id \
          } \
      }",
      { data: data },
      { user_id: -1 }
  );

  users.push(res.users);
}

// more fake users with random email id's
for (i = 0; i < user_count; i++) {
  var data = {
    full_name: fake.name(),
    avatar: fake.avatar_url(),
    phone: fake.phone(),
    email: "user_" + i + "_" + fake.email(),
    password: pwd,
    password_confirmation: pwd
  };

  var res = graphql(
      " \
      mutation { \
          users(insert: $data) { \
              id \
          } \
      }",
      { data: data },
      { user_id: -1 }
  );

  users.push(res.users);
}


// ---- add customers

var customers = [];

// we also need customers
for (i = 0; i < customer_count; i++) {
  var u = users[Math.floor(Math.random() * user_count)];
  var data = {
    stripe_id: "ch_" + Math.floor(Math.random() * 100),
    full_name: fake.name(),
    phone: fake.phone(),
    email: fake.email(),
    password: pwd,
    password_confirmation: pwd,
  };

  var res = graphql(
      " \
      mutation { \
          customers(insert: $data) { \
              id \
          } \
      }",
      { data: data },
      { user_id: u.id }
  );

  customers.push(res.customer);
}

// ---- define some sections

var categories = [
  {
    id: 1,
    name: "Beers",
    description: "Liquid Bread"
  },
  {
    id: 2,
    name: "Alcohol",
    description: "Bad for you!"
  },
];

// ---- add those sections using bulk insert

var res = graphql(
    " \
  mutation { \
    categories(insert: $categories) { \
      id \
    } \
  }",
    { categories: categories, user_id: 1 }
);

// ---- add products

var products = [];

for (i = 0; i < product_count; i++) {
  var desc = [fake.beer_style(), fake.beer_hop(), fake.beer_malt()].join(", ");
  var u = users[Math.floor(Math.random() * user_count)];

  // categories needs connecting since they are
  // a related to items in a different table
  // while tags can just be anything.

  var tag_list = fake.hipster_sentence(5);
  var tags = tag_list.substring(0, tag_list.length - 1).split(" ");

  var data = {
    "name": fake.beer_name(),
    "description": desc,
    "price": fake.price(),
    "category_ids":[1,2,3],
    "user_id": u.id
  };



  var res = graphql(
      " \
    mutation { \
        products(insert: $data) { \
            id \
        } \
    }",
      { data: data }

  );

  products.push(res.products);
}

// ---- add purchases (joining customers with products)

var purchases = [];

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
        purchases(insert: $data) { \
            id \
        } \
    }",
      { data: data },
      { user_id: u.id }
  );

  purchases.push(res.purchases);
}

// ---- add notifications

var notifications = [];

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
        notifications(insert: $data) { \
            id \
        } \
    }",
      { data: data },
      { user_id: u.id }
  );

  notifications.push(res.notifications);
}

// ---- add comments

var comments = [];

for (i = 0; i < comments_count; i++) {
  var userId = Math.floor(Math.random() * user_count) + 1; // no id of 0
  var productId = Math.floor(Math.random() * product_count) + 1;

  var data = {
    body: fake.sentence(10),
    product_id: productId,
    user_id: userId
  };

  if (comments.length !== 0) {
    var c = comments[Math.floor(Math.random() * comments.length)];
    data["comment"] = {
      find: "children",
      connect: { id: c.id },
    };
  }


  var res = graphql(
      " \
    mutation { \
        comments(insert: $data) { \
            id \
        } \
    }",
      { data: data }
  );

}


