var user_count = 10
    customer_count = 100
    product_count = 50
    purchase_count = 100

var users = []
    customers = []
    products = []

for (i = 0; i < user_count; i++) {
  var pwd = fake.password()
  var data = {
    full_name: fake.name(),
    avatar: fake.avatar_url(200),
    phone: fake.phone(),
    email: fake.email(),
    password: pwd,
    password_confirmation: pwd,
    created_at: "now",
    updated_at: "now"
  }

  var res = graphql(" \
    mutation { \
      user(insert: $data) { \
        id \
      } \
    }", { data: data })

  users.push(res.user)
}

for (i = 0; i < product_count; i++) {
  var n = Math.floor(Math.random() * users.length)
  var user = users[n]

  var desc = [
    fake.beer_style(),
    fake.beer_hop(),
    fake.beer_yeast(),
    fake.beer_ibu(),
    fake.beer_alcohol(),
    fake.beer_blg(),
  ].join(", ")
 
  var data = {
    name: fake.beer_name(),
    description: desc,
    price: fake.price()
    //user_id: user.id,
    //created_at: "now",
    //updated_at: "now"
  }

  var res = graphql(" \
    mutation { \
      product(insert: $data) { \
        id \
      } \
    }", { data: data }, {
      user_id: 5
    })
  products.push(res.product)
}

for (i = 0; i < customer_count; i++) {
  var pwd = fake.password()

  var data = {
    stripe_id: "CUS-" + fake.uuid(), 
    full_name: fake.name(),
    phone: fake.phone(),
    email: fake.email(),
    password: pwd,
    password_confirmation: pwd,
    created_at: "now",
    updated_at: "now"
  }

  var res = graphql(" \
    mutation { \
      customer(insert: $data) { \
        id \
      } \
    }", { data: data })
  customers.push(res.customer)
}

for (i = 0; i < purchase_count; i++) {
  var sale_type = fake.rand_string(["rented", "bought"])

  if (sale_type === "rented") {
    var due_date = fake.date()
    var returned = fake.date()
  }

  var data = {
    customer_id: customers[Math.floor(Math.random() * customer_count)].id,
    product_id: products[Math.floor(Math.random() * product_count)].id,
    sale_type: sale_type,
    quantity: Math.floor(Math.random() * 10),
    due_date: due_date,
    returned: returned,
    created_at: "now",
    updated_at: "now"
  }

  var res = graphql(" \
    mutation { \
      purchase(insert: $data) { \
        id \
      } \
    }", { data: data })

  console.log(res)
}